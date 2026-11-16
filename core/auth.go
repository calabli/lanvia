package core

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pairing code alphabet: the confusable 0/O and 1/I/L are removed.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
const codeLen = 10

// Clock skew allowed for signatures. Clients calibrate with /api/time first, so this is only a fallback.
const maxSkew = 5 * time.Minute

func newCode() string {
	var sb strings.Builder
	for i := 0; i < codeLen; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		sb.WriteByte(codeAlphabet[n.Int64()])
	}
	return sb.String()
}

func normalizeCode(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(s) {
		if strings.ContainsRune(codeAlphabet, r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func formatCode(c string) string {
	if len(c) <= 5 {
		return c
	}
	return c[:5] + "-" + c[5:]
}

// creds are derived from the pairing code; the pairing code itself is never sent over the network.
type creds struct {
	key   []byte // HMAC key
	space string // public space ID, put in the broadcast beacon to recognize the devices of the same group
	token string // Bearer token for automations such as shortcuts (a derived value, the pairing code cannot be recovered from it)
	enc   []byte // key for encrypting content between nodes (separate from the signing key)
}

func deriveCreds(code string) creds {
	k := sha256.Sum256([]byte("lanvia-key:" + code))
	s := sha256.Sum256([]byte("lanvia-space:" + code))
	return creds{
		key:   k[:],
		space: hex.EncodeToString(s[:8]),
		token: hex.EncodeToString(mac(k[:], "lanvia-api-token"))[:32],
		enc:   mac(k[:], "lanvia-content-encryption"),
	}
}

func mac(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func signature(key []byte, ts, method, path string) string {
	return hex.EncodeToString(mac(key, ts+"\n"+method+"\n"+path))
}

// Signature v2 (since v0.8): it adds a random nonce and covers the query parameters. The nonce of
// a write request may only be used once, so an intercepted request cannot be replayed; responses to
// v2 signed requests are always encrypted (unless the signature says _plain=1). The old version (v1) only signs the time, method and path and is still accepted.
func signature2(key []byte, ts, nonce, method, path, query string) string {
	return hex.EncodeToString(mac(key, "lanvia2\n"+ts+"\n"+nonce+"\n"+method+"\n"+path+"\n"+query))
}

// the parameters used for signing are not part of the signature itself; _q holds the encrypted query parameters and is replaced after decryption
var sigParams = map[string]bool{"_t": true, "_s": true, "_n": true, "_v": true, "_q": true}

func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	var keep []string
	for _, kv := range strings.Split(raw, "&") {
		k, _, _ := strings.Cut(kv, "=")
		if !sigParams[k] {
			keep = append(keep, kv)
		}
	}
	return strings.Join(keep, "&")
}

func signRequest(req *http.Request, c creds, nodeID string, v2 bool) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req.Header.Set("X-Lanvia-TS", ts)
	req.Header.Set("X-Lanvia-Node", nodeID)
	if !v2 {
		req.Header.Set("X-Lanvia-Sig", signature(c.key, ts, req.Method, req.URL.Path))
		return
	}
	nonce := randHex(8)
	req.Header.Set("X-Lanvia-Auth", "2")
	req.Header.Set("X-Lanvia-Nonce", nonce)
	req.Header.Set("X-Lanvia-Sig", signature2(c.key, ts, nonce, req.Method, req.URL.Path, canonicalQuery(req.URL.RawQuery)))
}

type authKind int

const (
	authFail authKind = iota
	authToken
	authV1
	authV2
)

// checkSigned verifies the signature or the token of a request. The query parameters of a v2 request may be encrypted (_q), they are decrypted and replace r.URL.RawQuery while verifying.
// It returns the nonce and the time of a v2 request, which are used to prevent replay.
func checkSigned(r *http.Request, c creds) (kind authKind, nonce string, ts int64) {
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(b)), []byte(c.token)) == 1 {
			return authToken, "", 0
		}
		return authFail, "", 0
	}
	h, q := r.Header, r.URL.Query()
	tss, sig, nonce, ver := h.Get("X-Lanvia-TS"), h.Get("X-Lanvia-Sig"), h.Get("X-Lanvia-Nonce"), h.Get("X-Lanvia-Auth")
	if tss == "" { // EventSource and download links cannot set request headers, so use the query
		tss, sig, nonce, ver = q.Get("_t"), q.Get("_s"), q.Get("_n"), q.Get("_v")
	}
	n, err := strconv.ParseInt(tss, 10, 64)
	if err != nil {
		return authFail, "", 0
	}
	if d := time.Since(time.UnixMilli(n)); d > maxSkew || d < -maxSkew {
		return authFail, "", 0
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	if ver != "2" {
		if q.Has("_q") {
			return authFail, "", 0
		}
		if hmac.Equal([]byte(sig), []byte(signature(c.key, tss, method, r.URL.Path))) {
			return authV1, "", 0
		}
		return authFail, "", 0
	}
	if len(nonce) < 8 || len(nonce) > 64 {
		return authFail, "", 0
	}
	if eq := q.Get("_q"); eq != "" {
		plain, err := openString(eq, c.enc)
		if err != nil {
			return authFail, "", 0
		}
		r.URL.RawQuery = plain
	}
	if hmac.Equal([]byte(sig), []byte(signature2(c.key, tss, nonce, method, r.URL.Path, canonicalQuery(r.URL.RawQuery)))) {
		return authV2, nonce, n
	}
	return authFail, "", 0
}

// openString decrypts a short string encrypted by the web page (a base64url encoded encrypted stream).
func openString(s string, key []byte) (string, error) {
	if len(s) > 64<<10 {
		return "", errors.New("too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	plain, err := io.ReadAll(newOpenReader(bytes.NewReader(b), key))
	return string(plain), err
}

// nonceCache remembers the nonces used by v2 write requests (within the signature lifetime) and rejects a request that arrives a second time.
type nonceCache struct {
	mu   sync.Mutex
	seen map[string]int64 // nonce -> expiry time
	next int64            // time of the next cleanup of the expired entries
}

func (n *nonceCache) use(nonce string, ts int64) bool {
	now := time.Now().UnixMilli()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen == nil {
		n.seen = map[string]int64{}
	}
	if now > n.next {
		for k, exp := range n.seen {
			if exp < now {
				delete(n.seen, k)
			}
		}
		n.next = now + 60_000
	}
	if _, dup := n.seen[nonce]; dup {
		return false
	}
	n.seen[nonce] = max(ts, now) + maxSkew.Milliseconds() + 1000
	return true
}

// isLocalAdmin reports whether a request comes from the local browser (the dashboard).
// The source IP alone is not enough: a malicious page can reach localhost through DNS rebinding or a
// cross-site form, so the Host must be localhost too and the Origin of a write request must match the Host.
func isLocalAdmin(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	if h != "localhost" && h != "127.0.0.1" && h != "::1" {
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	o := r.Header.Get("Origin")
	return o == "" || o == "http://"+r.Host
}

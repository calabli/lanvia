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

// 配对码字母表：去掉了容易混淆的 0/O、1/I/L。
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
const codeLen = 10

// 签名允许的时钟偏差。客户端会先用 /api/time 校准，所以这里只需兜底。
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

// creds 由配对码派生；配对码本身永远不会在网络上传输。
type creds struct {
	key   []byte // HMAC 密钥
	space string // 公开的空间 ID，写在广播信标里用于识别同组设备
	token string // 给快捷指令等自动化用的 Bearer token（派生值，无法反推配对码）
	enc   []byte // 节点之间内容加密的密钥（与签名密钥分开）
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

// 签名 v2（v0.8 起）：多了随机 nonce，并且覆盖查询参数。写操作的 nonce 只能用一次，
// 所以截获的请求不能再发一遍；带 v2 签名的请求，响应一律加密（签名里明确写了 _plain=1 的除外）。
// 旧版本（v1）只签时间、方法和路径，仍然接受，以便和旧版本设备互通。
func signature2(key []byte, ts, nonce, method, path, query string) string {
	return hex.EncodeToString(mac(key, "lanvia2\n"+ts+"\n"+nonce+"\n"+method+"\n"+path+"\n"+query))
}

// 签名用的参数不参与签名本身；_q 是加密后的查询参数，解密后替换掉
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

// checkSigned 校验请求的签名或 token。v2 请求的查询参数可能是加密的（_q），校验时会解密并替换 r.URL.RawQuery。
// 返回 v2 请求的 nonce 和时间，用于防重放。
func checkSigned(r *http.Request, c creds) (kind authKind, nonce string, ts int64) {
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(b)), []byte(c.token)) == 1 {
			return authToken, "", 0
		}
		return authFail, "", 0
	}
	h, q := r.Header, r.URL.Query()
	tss, sig, nonce, ver := h.Get("X-Lanvia-TS"), h.Get("X-Lanvia-Sig"), h.Get("X-Lanvia-Nonce"), h.Get("X-Lanvia-Auth")
	if tss == "" { // EventSource / 下载链接无法加请求头，走 query
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

// openString 解开网页加密的短字符串（base64url 编码的加密流）。
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

// nonceCache 记住 v2 写请求用过的 nonce（签名有效期内），同一个请求第二次到达时拒绝。
type nonceCache struct {
	mu   sync.Mutex
	seen map[string]int64 // nonce -> 过期时间
	next int64            // 下次清理过期项的时间
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

// isLocalAdmin 判断请求是否来自本机浏览器（仪表盘）。
// 仅凭来源 IP 不够：恶意网页可以通过 DNS rebinding 或跨站表单打到 localhost，
// 所以还要求 Host 是 localhost，且写操作的 Origin 与 Host 一致。
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

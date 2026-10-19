package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
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

func signRequest(req *http.Request, c creds, nodeID string) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req.Header.Set("X-Lanvia-TS", ts)
	req.Header.Set("X-Lanvia-Sig", signature(c.key, ts, req.Method, req.URL.Path))
	req.Header.Set("X-Lanvia-Node", nodeID)
}

func checkSigned(r *http.Request, c creds) bool {
	if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(b)), []byte(c.token)) == 1
	}
	ts, sig := r.Header.Get("X-Lanvia-TS"), r.Header.Get("X-Lanvia-Sig")
	if ts == "" { // EventSource / <img> 无法加请求头，走 query
		ts, sig = r.URL.Query().Get("_t"), r.URL.Query().Get("_s")
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := time.Since(time.UnixMilli(n)); d > maxSkew || d < -maxSkew {
		return false
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	return hmac.Equal([]byte(sig), []byte(signature(c.key, ts, method, r.URL.Path)))
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

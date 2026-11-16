package core

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Echoes back the query parameters and request body, to inspect what the auth middleware hands to the handler after decrypting
func echo(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	io.WriteString(w, r.URL.RawQuery+"|"+string(b))
}

type signOpt struct {
	nonce, query, plainQuery string // plainQuery is encrypted into _q when non-empty
	body                     []byte
	encBody                  bool
}

func v2req(c creds, method, path string, o signOpt) *http.Request {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if o.nonce == "" {
		o.nonce = randHex(8)
	}
	signedQ := o.query
	url := path
	if o.plainQuery != "" {
		signedQ = o.plainQuery
		url += "?_q=" + base64.RawURLEncoding.EncodeToString(sealAll([]byte(o.plainQuery), c.enc))
	} else if o.query != "" {
		url += "?" + o.query
	}
	body := o.body
	if o.encBody {
		body = sealAll(body, c.enc)
	}
	r := httptest.NewRequest(method, url, bytes.NewReader(body))
	r.Header.Set("X-Lanvia-Auth", "2")
	r.Header.Set("X-Lanvia-TS", ts)
	r.Header.Set("X-Lanvia-Nonce", o.nonce)
	r.Header.Set("X-Lanvia-Sig", signature2(c.key, ts, o.nonce, method, path, signedQ))
	if o.encBody {
		r.Header.Set("X-Lanvia-Body-Enc", "1")
	}
	return r
}

func serve(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// Decrypts the (possibly) encrypted response
func respBody(t *testing.T, w *httptest.ResponseRecorder, c creds) (string, bool) {
	t.Helper()
	if w.Header().Get("X-Lanvia-Enc") != "1" {
		return w.Body.String(), false
	}
	b, err := io.ReadAll(newOpenReader(w.Body, c.enc))
	if err != nil {
		t.Fatalf("decrypt response: %v", err)
	}
	return string(b), true
}

func TestAuthV2(t *testing.T) {
	a := testApp(t, nil)
	c := a.creds()
	h := a.requireAuth(echo)

	// Normal request: the response is encrypted and the query parameters are passed to the handler as-is
	w := serve(h, v2req(c, "GET", "/api/x", signOpt{query: "a=1&b=2"}))
	if body, enc := respBody(t, w, c); w.Code != 200 || !enc || body != "a=1&b=2|" {
		t.Fatalf("v2 GET: %d enc=%v %q", w.Code, enc, body)
	}
	// Tampered query parameters
	r := v2req(c, "GET", "/api/x", signOpt{query: "a=1"})
	r.URL.RawQuery = "a=2"
	if w := serve(h, r); w.Code != 401 {
		t.Fatalf("tampered query accepted: %d", w.Code)
	}
	// Removing the _plain=1 stated in the signature does not work either; it stays plaintext only when the signature says so
	w = serve(h, v2req(c, "GET", "/api/x", signOpt{query: "_plain=1"}))
	if _, enc := respBody(t, w, c); w.Code != 200 || enc {
		t.Fatalf("_plain: %d enc=%v", w.Code, enc)
	}
	r = v2req(c, "GET", "/api/x", signOpt{})
	r.URL.RawQuery = "_plain=1"
	if w := serve(h, r); w.Code != 401 {
		t.Fatalf("added _plain accepted: %d", w.Code)
	}
	// Encrypted query parameters and request body
	w = serve(h, v2req(c, "POST", "/api/x", signOpt{plainQuery: "q=秘密", body: []byte("正文"), encBody: true}))
	if body, _ := respBody(t, w, c); w.Code != 200 || body != "q=秘密|正文" {
		t.Fatalf("encrypted query/body: %d %q", w.Code, body)
	}
	// Replay: the same write request is rejected the second time; GET may repeat (image URLs in web pages are reused)
	r1 := v2req(c, "POST", "/api/x", signOpt{nonce: "aaaaaaaaaaaaaaaa"})
	r2 := v2req(c, "POST", "/api/x", signOpt{nonce: "aaaaaaaaaaaaaaaa"})
	if w := serve(h, r1); w.Code != 200 {
		t.Fatalf("first POST: %d", w.Code)
	}
	if w := serve(h, r2); w.Code != 409 {
		t.Fatalf("replayed POST: %d, want 409", w.Code)
	}
	g := v2req(c, "GET", "/api/x", signOpt{nonce: "bbbbbbbbbbbbbbbb"})
	for i := 0; i < 2; i++ {
		g2 := g.Clone(g.Context())
		if w := serve(h, g2); w.Code != 200 {
			t.Fatalf("repeated GET %d: %d", i, w.Code)
		}
	}
	// Different pairing code
	if w := serve(h, v2req(deriveCreds("ABCDEFGHJK"), "GET", "/api/x", signOpt{})); w.Code != 401 {
		t.Fatalf("wrong code: %d", w.Code)
	}
	// When only encrypted connections are allowed, _plain is ignored
	a.updateCfg(func(c *Config) { c.StrictEnc = true })
	w = serve(h, v2req(c, "GET", "/api/x", signOpt{query: "_plain=1"}))
	if _, enc := respBody(t, w, c); !enc {
		t.Fatal("strict mode: response not encrypted")
	}
}

func TestAuthV1AndToken(t *testing.T) {
	a := testApp(t, nil)
	c := a.creds()
	h := a.requireAuth(echo)
	v1 := func(enc bool) *http.Request {
		r := httptest.NewRequest("POST", "/api/x?a=1", strings.NewReader("hi"))
		signRequest(r, c, "n1", false)
		if enc {
			r.Header.Set("X-Lanvia-Enc", "1")
		}
		return r
	}
	// Older-version devices: work as usual, whether to encrypt depends on what the other side asks for
	if w := serve(h, v1(false)); w.Code != 200 || w.Header().Get("X-Lanvia-Enc") == "1" {
		t.Fatalf("v1 plain: %d", w.Code)
	}
	if w := serve(h, v1(true)); w.Code != 200 || w.Header().Get("X-Lanvia-Enc") != "1" {
		t.Fatalf("v1 enc: %d", w.Code)
	}
	// v1 cannot carry encrypted query parameters
	r := v1(true)
	r.URL.RawQuery = "_q=xyz"
	if w := serve(h, r); w.Code != 401 {
		t.Fatalf("v1 with _q: %d", w.Code)
	}
	// Sign v2 on the Go side, to confirm it matches the hand-written signature in the test
	r = httptest.NewRequest("GET", "/api/x?since=5", nil)
	signRequest(r, c, "n1", true)
	if w := serve(h, r); w.Code != 200 || w.Header().Get("X-Lanvia-Enc") != "1" {
		t.Fatalf("signRequest v2: %d", w.Code)
	}

	tok := func(token string) *http.Request {
		r := httptest.NewRequest("GET", "/api/x", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}
	if w := serve(h, tok(c.token)); w.Code != 200 {
		t.Fatalf("token: %d", w.Code)
	}
	// Old tokens stop working after regeneration
	old := c.token
	a.updateCfg(func(c *Config) { c.Token = randHex(16) })
	if w := serve(h, tok(old)); w.Code != 401 {
		t.Fatalf("old token after reset: %d", w.Code)
	}
	if w := serve(h, tok(a.creds().token)); w.Code != 200 {
		t.Fatalf("new token: %d", w.Code)
	}
	// Token access turned off
	a.updateCfg(func(c *Config) { c.TokenOff = true })
	if w := serve(h, tok(a.creds().token)); w.Code != 403 {
		t.Fatalf("token off: %d", w.Code)
	}
	// When only encrypted connections are allowed: unencrypted old-version requests are rejected
	a.updateCfg(func(c *Config) { c.StrictEnc = true })
	if w := serve(h, v1(false)); w.Code != 403 {
		t.Fatalf("strict v1 plain: %d", w.Code)
	}
}

func TestSealEvent(t *testing.T) {
	c := deriveCreds("K7QFM2XD9P")
	out := sealEvent(sseFrame("add", map[string]string{"text": "秘密"}), c.enc)
	s := string(out)
	if !strings.HasPrefix(s, "event: add\ndata: ") || !strings.HasSuffix(s, "\n\n") || strings.Contains(s, "秘密") {
		t.Fatalf("bad event: %q", s)
	}
	data := strings.TrimSuffix(strings.TrimPrefix(s, "event: add\ndata: "), "\n\n")
	plain, err := openString(data, c.enc)
	if err != nil || plain != `{"text":"秘密"}` {
		t.Fatalf("open: %v %q", err, plain)
	}
}

package core

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rsc.io/qr"
)

const maxUpload = 4 << 30

type Info struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	OS      string       `json:"os"`
	Version string       `json:"version"`
	Port    int          `json:"port"`
	Now     int64        `json:"now"`
	Clients []ClientInfo `json:"clients"`

	// 以下字段只返回给本机仪表盘
	Local     bool          `json:"local,omitempty"`
	Code      string        `json:"code,omitempty"`
	Token     string        `json:"token,omitempty"`
	URLs      []string      `json:"urls,omitempty"`
	Settings  *Settings     `json:"settings,omitempty"`
	Clipboard bool          `json:"clipboard,omitempty"`
	Peers     []*Peer       `json:"peers,omitempty"`
	Nearby    []Nearby      `json:"nearby,omitempty"`
	Manual    []ManualPeer  `json:"manual,omitempty"`
	Storage   *StorageUsage `json:"storage,omitempty"`
	DataDir   string        `json:"dataDir,omitempty"`
}

func (a *App) routes(web fs.FS) http.Handler {
	m := http.NewServeMux()
	static := http.FileServerFS(web)
	m.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})
	m.HandleFunc("GET /api/time", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]int64{"now": nowMs()})
	})

	auth := a.requireAuth
	local := a.requireLocal
	m.HandleFunc("GET /api/info", auth(a.handleInfo))
	m.HandleFunc("GET /api/events", auth(a.handleEvents))
	m.HandleFunc("GET /api/history", auth(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.store.Snapshot()) }))
	m.HandleFunc("GET /api/latest", auth(a.handleLatest))
	m.HandleFunc("POST /api/clip", auth(a.handleClip))
	m.HandleFunc("POST /api/upload", auth(a.handleUpload))
	m.HandleFunc("GET /api/blob/{id}", auth(a.handleBlob))
	m.HandleFunc("DELETE /api/items/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		a.remove(r.PathValue("id"), "")
		writeJSON(w, 200, ok)
	}))
	m.HandleFunc("POST /api/items/{id}/edit", auth(a.handleEdit))
	m.HandleFunc("GET /api/items/{id}/versions", auth(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		it := a.store.Get(id)
		if it == nil {
			errJSON(w, 404, "not found")
			return
		}
		writeJSON(w, 200, map[string]any{"item": it, "versions": a.store.Versions(id)})
	}))
	m.HandleFunc("POST /api/items/{id}/expiry", auth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TTL  int64 `json:"ttl"` // 秒；0 表示跟随全局设置
			Keep bool  `json:"keep"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.TTL < 0 || req.TTL > 10*365*86400 {
			errJSON(w, 400, "bad request")
			return
		}
		it, err := a.SetItemExpiry(r.PathValue("id"), time.Duration(req.TTL)*time.Second, req.Keep)
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, it)
	}))
	m.HandleFunc("POST /api/peers", local(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Addr   string `json:"addr"`
			Remove bool   `json:"remove"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			errJSON(w, 400, "bad json")
			return
		}
		if req.Remove {
			a.RemoveManualPeer(req.Addr)
			writeJSON(w, 200, a.info(true))
			return
		}
		mp, err := a.AddManualPeer(req.Addr)
		res := map[string]any{"peer": mp, "info": a.info(true)}
		if err != nil {
			res["error"] = manualErrText(err)
		}
		writeJSON(w, 200, res)
	}))
	m.HandleFunc("POST /api/storage/clear-replicas", local(func(w http.ResponseWriter, r *http.Request) {
		n, freed := a.ClearReplicas()
		writeJSON(w, 200, map[string]any{"count": n, "freed": freed, "info": a.info(true)})
	}))
	m.HandleFunc("GET /api/clipboard", local(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, a.ClipboardState())
	}))
	m.HandleFunc("POST /api/send-clipboard", local(func(w http.ResponseWriter, r *http.Request) {
		it, err := a.SendClipboard()
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, it)
	}))
	m.HandleFunc("POST /api/clear", auth(func(w http.ResponseWriter, r *http.Request) {
		a.clear(nowMs(), "")
		writeJSON(w, 200, ok)
	}))
	m.HandleFunc("POST /api/relay", auth(a.handleRelay))
	m.HandleFunc("POST /api/settings", local(a.handleSettings))
	m.HandleFunc("POST /api/join", local(a.handleJoin))
	m.HandleFunc("GET /api/qr", local(handleQR))
	m.HandleFunc("POST /api/copy/{id}", local(func(w http.ResponseWriter, r *http.Request) {
		if err := a.CopyToClipboard(r.PathValue("id")); err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, ok)
	}))
	// 电脑端保存文件：then=open 用默认程序打开，then=reveal 在文件夹中显示
	m.HandleFunc("POST /api/save/{id}", local(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.SaveFile(r.PathValue("id"))
		if err != nil {
			errJSON(w, 400, err.Error())
			return
		}
		switch r.URL.Query().Get("then") {
		case "open":
			OpenFile(p)
		case "reveal":
			RevealFile(p)
		}
		writeJSON(w, 200, map[string]string{"path": p, "dir": downloadsDir()})
	}))
	m.HandleFunc("POST /api/reveal", local(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Path string }
		if json.NewDecoder(r.Body).Decode(&req) != nil || !inDownloads(req.Path) {
			errJSON(w, 400, "bad path")
			return
		}
		RevealFile(req.Path)
		writeJSON(w, 200, ok)
	}))
	m.HandleFunc("POST /api/desktop/{action}", local(func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		hook := a.desktopHook
		a.mu.RUnlock()
		id := r.URL.Query().Get("id")
		if id != "" && !idRe.MatchString(id) {
			errJSON(w, 400, "bad id")
			return
		}
		if hook == nil || !hook(r.PathValue("action"), id) {
			errJSON(w, 404, "not in desktop app")
			return
		}
		writeJSON(w, 200, ok)
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		m.ServeHTTP(w, r)
	})
}

var ok = map[string]bool{"ok": true}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (a *App) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isLocalAdmin(r) {
			h(w, r)
			return
		}
		if checkSigned(r, a.creds()) {
			// 配对码相同的节点发来签名请求：如果还不认识它，就连回去（只在一边手动添加也能双向同步）
			if r.Header.Get("X-Lanvia-Sig") != "" {
				if port := r.Header.Get("X-Lanvia-Port"); port != "" {
					if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
						a.peers.noticeCaller(r.Header.Get("X-Lanvia-Node"), net.JoinHostPort(host, port))
					}
				}
			}
			h(w, r)
			return
		}
		errJSON(w, 401, "unauthorized")
	}
}

func (a *App) requireLocal(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isLocalAdmin(r) {
			h(w, r)
			return
		}
		errJSON(w, 403, "local only")
	}
}

func (a *App) info(local bool) *Info {
	c := a.cfgSnapshot()
	in := &Info{ID: c.ID, Name: c.Name, OS: runtime.GOOS, Version: Version, Port: c.Port, Now: nowMs(), Clients: a.hub.List()}
	if local {
		s := c.Settings
		in.Local, in.Code, in.Token, in.Settings = true, formatCode(c.Code), a.creds().token, &s
		in.Clipboard, in.Peers, in.Nearby, in.DataDir = a.clip.ok, a.peers.List(), a.peers.NearbyList(), a.dir
		in.Manual = a.peers.ManualList()
		u := a.StorageUsage()
		in.Storage = &u
		for _, l := range lanAddrs() {
			in.URLs = append(in.URLs, "http://"+net.JoinHostPort(l.IP.String(), strconv.Itoa(c.Port)))
		}
	}
	return in
}

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.info(isLocalAdmin(r)))
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, okf := w.(http.Flusher)
	if !okf {
		errJSON(w, 500, "streaming unsupported")
		return
	}
	q := r.URL.Query()
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		name = "浏览器"
	}
	c := &Client{
		ClientInfo: ClientInfo{ID: randHex(6), Name: trimRunes(name, 40), Kind: trimRunes(q.Get("kind"), 12), Since: nowMs(), Local: isLocalAdmin(r)},
		ch:         make(chan []byte, 256),
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	self := a.cfgSnapshot()
	w.Write(sseFrame("hello", map[string]any{
		"you":   c.ID,
		"node":  map[string]string{"id": self.ID, "name": self.Name, "os": runtime.GOOS},
		"items": a.store.List(),
		// 设备列表只在变化时推送；本机窗口连上时列表不变，所以首次连接必须带上当前列表
		"devices": a.devices(),
		"local":   c.Local,
		"now":     nowMs(),
	}))
	fl.Flush()
	a.hub.Add(c)
	defer a.hub.Remove(c)

	t := time.NewTicker(20 * time.Second) // 保活：iOS 和部分路由器会断开空闲连接
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-c.ch:
			w.Write(msg)
			fl.Flush()
		case <-t.C:
			io.WriteString(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (a *App) handleLatest(w http.ResponseWriter, r *http.Request) {
	it := a.store.LatestText()
	if it == nil {
		w.WriteHeader(204)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, it.Text)
}

// handleClip 同时支持 JSON（网页端）和纯文本（iOS 快捷指令 / curl）。
func (a *App) handleClip(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTextLen+1024))
	if err != nil {
		errJSON(w, 413, "too large")
		return
	}
	var req struct{ Text, From string }
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if json.Unmarshal(body, &req) != nil {
			errJSON(w, 400, "bad json")
			return
		}
	} else {
		req.Text, req.From = string(body), r.URL.Query().Get("from")
	}
	if req.From == "" {
		req.From = "快捷指令"
	}
	if strings.TrimSpace(req.Text) == "" || !utf8.ValidString(req.Text) {
		errJSON(w, 400, "empty text")
		return
	}
	writeJSON(w, 200, a.newText(req.Text, trimRunes(req.From, 40), true))
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := sanitizeName(q.Get("name"))
	id := randHex(8)
	final := a.store.blobPath(id)
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, maxUpload))
	f.Close()
	if err != nil || n == 0 {
		os.Remove(part)
		errJSON(w, 400, "upload failed")
		return
	}
	os.Rename(part, final)
	ct := r.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = mime.TypeByExtension(filepath.Ext(name))
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	from := trimRunes(q.Get("from"), 40)
	if from == "" {
		from = "快捷指令"
	}
	it := &Item{ID: id, Kind: "file", Name: name, Mime: ct, Size: n, From: from, Origin: a.cfgSnapshot().ID, TS: nowMs()}
	a.ingest(it, "", false)
	go a.enforceQuota() // 原件不删，必要时给它腾出副本占的空间
	writeJSON(w, 200, it)
}

func (a *App) handleBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	it := a.store.Get(id)
	if it == nil || it.Kind != "file" {
		errJSON(w, 404, "not found")
		return
	}
	dl := r.URL.Query().Get("dl") == "1"
	if f, err := os.Open(a.store.blobPath(id)); err == nil {
		defer f.Close()
		st, _ := f.Stat()
		blobHeaders(w, it, dl)
		http.ServeContent(w, r, it.Name, st.ModTime(), f)
		return
	}
	// 本机没有文件内容：从发送方或其他有这个文件的电脑取，同时缓存一份
	if r.Header.Get("X-Lanvia-Hop") != "" {
		errJSON(w, 404, "not found")
		return
	}
	rg := r.Header.Get("Range")
	resp, err := a.fetchFromPeers(r.Context(), it, rg)
	if err != nil {
		errJSON(w, 502, err.Error())
		return
	}
	defer resp.Body.Close()
	for _, k := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	blobHeaders(w, it, dl)
	w.WriteHeader(resp.StatusCode)
	if resp.StatusCode != 200 || rg != "" {
		io.Copy(w, resp.Body)
		return
	}
	quotaMu.Lock()
	fits := a.makeRoom(it.Size)
	quotaMu.Unlock()
	if !fits { // 超出文件占用上限：只转发，不缓存
		io.Copy(w, resp.Body)
		return
	}
	part := a.store.blobPath(id) + "." + randHex(4) + ".part"
	f, err := os.Create(part)
	if err != nil {
		io.Copy(w, resp.Body)
		return
	}
	n, err := io.Copy(w, io.TeeReader(resp.Body, f))
	f.Close()
	if err == nil && n == it.Size {
		os.Rename(part, a.store.blobPath(id))
	} else {
		os.Remove(part)
	}
}

func blobHeaders(w http.ResponseWriter, it *Item, dl bool) {
	ct := it.Mime
	if ct == "" {
		ct = "application/octet-stream"
	}
	inline := !dl && ct != "image/svg+xml" && (strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "audio/") || ct == "application/pdf")
	disp := "attachment"
	if inline {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(it.Name))
	h.Set("Content-Security-Policy", "sandbox") // 上传的 HTML/SVG 不能在本站执行脚本
	h.Set("Cache-Control", "private, max-age=86400")
}

func (a *App) handleEdit(w http.ResponseWriter, r *http.Request) {
	var req struct{ Text, From string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTextLen*2)).Decode(&req); err != nil {
		errJSON(w, 400, "bad json")
		return
	}
	if req.From == "" {
		req.From = "快捷指令"
	}
	it, err := a.EditItem(r.PathValue("id"), req.Text, trimRunes(req.From, 40))
	if err != nil {
		errJSON(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, it)
}

func (a *App) handleRelay(w http.ResponseWriter, r *http.Request) {
	var env Envelope
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTextLen*2)).Decode(&env); err != nil {
		errJSON(w, 400, "bad envelope")
		return
	}
	from := r.Header.Get("X-Lanvia-Node")
	switch env.Op {
	case "add":
		if validItem(env.Item) {
			a.ingest(env.Item, from, true)
		}
	case "edit":
		if idRe.MatchString(env.ID) && validVersion(env.Version) {
			a.applyRemoteEdit(env.ID, env.Version, from)
		}
	case "meta":
		if idRe.MatchString(env.ID) && env.Item != nil && env.Item.MetaTS > 0 {
			if it := a.store.applyMeta(env.ID, env.Item.ExpiresAt, env.Item.Keep, env.Item.MetaTS); it != nil {
				a.afterMeta(it, from)
				a.prune()
			}
		}
	case "delete":
		a.remove(env.ID, from)
	case "clear":
		if env.TS > 0 && env.TS <= nowMs()+int64(maxSkew/time.Millisecond) {
			a.clear(env.TS, from)
		}
	}
	writeJSON(w, 200, ok)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AutoApply   *bool   `json:"autoApply"`
		AutoFetchMB *int    `json:"autoFetchMB"`
		StorageMB   *int    `json:"storageMB"`
		Watch       *bool   `json:"watch"`
		Name        *string `json:"name"`
		HistoryDays *int    `json:"historyDays"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		errJSON(w, 400, "bad json")
		return
	}
	if m := req.StorageMB; m != nil && (*m < 0 || *m > 1<<24) {
		errJSON(w, 400, "文件占用上限无效")
		return
	}
	if m := req.AutoFetchMB; m != nil && (*m < -1 || *m > 1<<20) {
		errJSON(w, 400, "自动下载上限无效")
		return
	}
	if d := req.HistoryDays; d != nil && (*d < 0 || *d > 3650) {
		errJSON(w, 400, "保留天数应在 0 到 3650 之间")
		return
	}
	if req.Watch != nil && *req.Watch {
		a.clip.resync() // 切回主动模式时不补发被动期间复制的内容
	}
	a.updateCfg(func(c *Config) {
		if req.HistoryDays != nil {
			c.HistoryDays = *req.HistoryDays
		}
		if req.AutoFetchMB != nil {
			c.AutoFetchMB = *req.AutoFetchMB
		}
		if req.StorageMB != nil {
			c.StorageMB = *req.StorageMB
		}
		if req.AutoApply != nil {
			c.AutoApply = *req.AutoApply
		}
		if req.Watch != nil {
			c.Watch = *req.Watch
		}
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			c.Name = trimRunes(strings.TrimSpace(*req.Name), 40)
		}
	})
	if req.HistoryDays != nil {
		a.store.setRetention(*req.HistoryDays)
		a.prune()
	}
	if req.AutoFetchMB != nil || req.StorageMB != nil {
		a.enforceQuota()
		go a.fetchMissing() // 上限调大了，补下之前跳过的
	}
	writeJSON(w, 200, a.info(true))
}

// handleJoin 换成另一台设备的配对码（加入它的空间），或重新生成配对码（踢掉所有已配对设备）。
func (a *App) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code  string `json:"code"`
		Reset bool   `json:"reset"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		errJSON(w, 400, "bad json")
		return
	}
	code := normalizeCode(req.Code)
	if req.Reset {
		code = newCode()
	} else if len(code) != codeLen {
		errJSON(w, 400, "配对码应为 10 位")
		return
	}
	a.updateCfg(func(c *Config) { c.Code = code })
	a.peers.reset()
	log.Printf("配对码已更新为 %s", formatCode(code))
	writeJSON(w, 200, a.info(true))
}

func handleQR(w http.ResponseWriter, r *http.Request) {
	c, err := qr.Encode(r.URL.Query().Get("text"), qr.M)
	if err != nil {
		errJSON(w, 400, err.Error())
		return
	}
	var sb strings.Builder
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				sb.WriteByte('1')
			} else {
				sb.WriteByte('0')
			}
		}
	}
	writeJSON(w, 200, map[string]any{"size": c.Size, "bits": sb.String()})
}

// manualErrText 把试连失败的原因翻译成用户看得懂的话。
func manualErrText(err error) string {
	switch {
	case err == errBadCode:
		return "对方的配对码和本机不同（未保存）。请先在“加入另一台电脑”里统一配对码"
	case err == errSelf:
		return err.Error()
	case strings.Contains(err.Error(), "地址") || strings.Contains(err.Error(), "请填写"):
		return err.Error()
	}
	return "暂时连不上（地址已保存，会自动重试）。请确认对方的 Lanvia 正在运行、IP 和端口正确，以及防火墙允许连接"
}

func sanitizeName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == "/" {
		s = "file"
	}
	return trimRunes(s, 120)
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

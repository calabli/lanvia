package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const beaconPort = 8687

type Beacon struct {
	App   string `json:"app"`
	V     int    `json:"v"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	OS    string `json:"os"`
	Port  int    `json:"port"`
	Space string `json:"space"`
}

// Peer 是同一空间内的另一个 Lanvia 节点。
type Peer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Addr   string `json:"addr"`
	Manual bool   `json:"manual"`
	Enc    bool   `json:"enc"` // 对方支持加密（v0.5 起）
	// 通过“手动添加”的哪个地址找到的（可能是主机名，和 Addr 不同）
	ManualAddr string       `json:"manualAddr,omitempty"`
	Clients    []ClientInfo `json:"clients"`
	seen       time.Time
	fails      int
	beacon     bool // 也能通过广播发现
}

// Nearby 是广播里看到、但配对码不同的节点，展示在仪表盘上方便用户加入。
type Nearby struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OS   string `json:"os"`
	Addr string `json:"addr"`
	seen time.Time
}

type Peers struct {
	a       *App
	mu      sync.Mutex
	m       map[string]*Peer
	nearby  map[string]*Nearby
	manual  []string
	probing map[string]bool
	hc      *http.Client

	noDiscovery bool
}

func newPeers(a *App, manual []string) *Peers {
	return &Peers{
		a: a, manual: manual,
		m: map[string]*Peer{}, nearby: map[string]*Nearby{}, probing: map[string]bool{},
		hc: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *Peers) start() {
	if !p.noDiscovery {
		go p.listen()
		go p.beaconLoop()
	}
	go p.pollLoop()
}

func (p *Peers) reset() {
	p.mu.Lock()
	p.m = map[string]*Peer{}
	p.mu.Unlock()
}

func (p *Peers) List() []*Peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Peer, 0, len(p.m))
	for _, x := range p.m {
		c := *x
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (p *Peers) NearbyList() []Nearby {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []Nearby{}
	for _, x := range p.nearby {
		out = append(out, *x)
	}
	return out
}

func (p *Peers) Get(id string) *Peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if x := p.m[id]; x != nil {
		c := *x
		return &c
	}
	return nil
}

// ---- 发现 ----

func (p *Peers) listen() {
	lc := net.ListenConfig{Control: reuseControl}
	pc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf(":%d", beaconPort))
	if err != nil {
		log.Printf(L("无法监听广播端口 %d（自动发现不可用，可使用 --peer）: %v", "Can't listen on broadcast port %d (discovery unavailable, use --peer): %v"), beaconPort, err)
		return
	}
	buf := make([]byte, 2048)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		var b Beacon
		if json.Unmarshal(buf[:n], &b) != nil || b.App != "lanvia" {
			continue
		}
		if ua, ok := src.(*net.UDPAddr); ok {
			p.onBeacon(b, ua.IP)
		}
	}
}

func (p *Peers) onBeacon(b Beacon, ip net.IP) {
	self := p.a.cfgSnapshot()
	if b.ID == self.ID || b.Port <= 0 {
		return
	}
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(b.Port))
	p.mu.Lock()
	defer p.mu.Unlock()
	if b.Space != p.a.creds().space {
		p.nearby[b.ID] = &Nearby{ID: b.ID, Name: b.Name, OS: b.OS, Addr: addr, seen: time.Now()}
		return
	}
	delete(p.nearby, b.ID)
	if x := p.m[b.ID]; x != nil {
		x.Addr, x.beacon = addr, true
		return
	}
	if !p.probing[addr] {
		p.probing[addr] = true
		go p.probe(addr, false)
	}
}

func (p *Peers) beaconLoop() {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		log.Printf(L("无法发送广播: %v", "Can't send broadcasts: %v"), err)
		return
	}
	for {
		c := p.a.cfgSnapshot()
		msg, _ := json.Marshal(Beacon{App: "lanvia", V: 1, ID: c.ID, Name: c.Name, OS: runtime.GOOS, Port: c.Port, Space: p.a.creds().space})
		dsts := map[string]bool{"255.255.255.255": true}
		for _, a := range lanAddrs() {
			dsts[a.Bcast.String()] = true
		}
		for d := range dsts {
			conn.WriteToUDP(msg, &net.UDPAddr{IP: net.ParseIP(d), Port: beaconPort})
		}
		time.Sleep(3 * time.Second)
	}
}

var (
	errBadCode = errors.New("对方的配对码和本机不同")
	errSelf    = errors.New("这是本机的地址")
)

// probe 用签名请求访问对方的 /api/info：只有配对码相同才会成功。
func (p *Peers) probe(addr string, manual bool) (*Info, error) {
	defer func() { p.mu.Lock(); delete(p.probing, addr); p.mu.Unlock() }()
	info, err := p.fetchInfo(addr)
	if err != nil {
		return nil, err
	}
	if info.ID == p.a.cfgSnapshot().ID {
		return nil, errSelf
	}
	p.mu.Lock()
	x, existed := p.m[info.ID]
	if !existed {
		x = &Peer{ID: info.ID}
		p.m[info.ID] = x
	}
	x.Name, x.OS, x.Addr, x.Clients, x.seen, x.fails = info.Name, info.OS, addr, info.Clients, time.Now(), 0
	x.Enc = info.Enc
	x.Manual = x.Manual || manual
	if manual {
		x.ManualAddr = addr
	}
	p.mu.Unlock()
	if !existed {
		log.Printf(L("已连接设备: %s (%s)", "Connected: %s (%s)"), info.Name, addr)
		go p.syncHistory(x.ID, addr)
	}
	return info, nil
}

// noticeCaller 处理一个配对码相同、但本机还不认识的节点发来的请求：主动连回去。
// 这样手动添加地址只需要在一边做，另一边的新内容也能发过去。
func (p *Peers) noticeCaller(id, addr string) {
	if id == "" || id == p.a.cfgSnapshot().ID {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m[id] != nil || p.probing[addr] {
		return
	}
	p.probing[addr] = true
	go p.probe(addr, false)
}

// ---- 手动添加的地址 ----

type ManualPeer struct {
	Addr      string `json:"addr"`
	Connected bool   `json:"connected"`
	Name      string `json:"name,omitempty"`
}

func (p *Peers) ManualList() []ManualPeer {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []ManualPeer{}
	for _, addr := range p.manual {
		mp := ManualPeer{Addr: addr}
		for _, x := range p.m {
			if x.ManualAddr == addr || x.Addr == addr {
				mp.Connected, mp.Name = true, x.Name
			}
		}
		out = append(out, mp)
	}
	return out
}

func (p *Peers) setManual(list []string) {
	p.mu.Lock()
	p.manual = append([]string(nil), list...)
	p.mu.Unlock()
}

// forget 移除手动地址后，断开只靠这个地址连上、广播里也看不到的设备。
func (p *Peers) forget(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, x := range p.m {
		if x.ManualAddr == addr {
			x.ManualAddr, x.Manual = "", false
			if !x.beacon {
				delete(p.m, id)
			}
		}
	}
}

func (p *Peers) pollLoop() {
	for {
		time.Sleep(4 * time.Second)
		p.mu.Lock()
		known := map[string]bool{}
		var ps []*Peer
		for _, x := range p.m {
			known[x.Addr] = true
			if x.ManualAddr != "" {
				known[x.ManualAddr] = true
			}
			c := *x
			ps = append(ps, &c)
		}
		for id, n := range p.nearby {
			if time.Since(n.seen) > 15*time.Second {
				delete(p.nearby, id)
			}
		}
		var todo []string
		for _, m := range p.manual {
			if !known[m] && !p.probing[m] {
				p.probing[m] = true
				todo = append(todo, m)
			}
		}
		p.mu.Unlock()
		for _, m := range todo {
			go p.probe(m, true)
		}
		for _, x := range ps {
			go p.refresh(x.ID, x.Addr)
		}
	}
}

func (p *Peers) refresh(id, addr string) {
	info, err := p.fetchInfo(addr)
	p.mu.Lock()
	defer p.mu.Unlock()
	x := p.m[id]
	if x == nil {
		return
	}
	if err != nil || info.ID != id {
		x.fails++
		if x.fails >= 3 {
			log.Printf(L("设备离线: %s", "Offline: %s"), x.Name)
			delete(p.m, id)
		}
		return
	}
	x.Name, x.OS, x.Clients, x.seen, x.fails = info.Name, info.OS, info.Clients, time.Now(), 0
	x.Enc = info.Enc
}

// ---- 节点间请求 ----

func (p *Peers) newReq(ctx context.Context, method, addr, path string, body []byte) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, "http://"+addr+path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := p.a.cfgSnapshot()
	signRequest(req, p.a.creds(), c.ID)
	req.Header.Set("X-Lanvia-Port", strconv.Itoa(c.Port)) // 让对方知道怎么连回来
	req.Header.Set("X-Lanvia-Enc", "1")                   // 请对方加密响应；旧版本会忽略，照常返回明文
	return req
}

var errPlaintext = errors.New("对方未加密（旧版本），而本机设置为只允许加密连接")

// do 发出节点间请求，并透明地解密响应。
func (p *Peers) do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Header.Get("X-Lanvia-Enc") == "1" {
		resp.Body = openReadCloser{newOpenReader(resp.Body, p.a.creds().enc), resp.Body}
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
		return resp, nil
	}
	if p.a.cfgSnapshot().StrictEnc && resp.StatusCode != http.StatusUnauthorized {
		resp.Body.Close()
		return nil, errPlaintext
	}
	return resp, nil
}

func (p *Peers) getJSON(addr, path string, v any) error {
	resp, err := p.do(p.hc, p.newReq(context.Background(), "GET", addr, path, nil))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errBadCode
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (p *Peers) fetchInfo(addr string) (*Info, error) {
	var info Info
	return &info, p.getJSON(addr, "/api/info", &info)
}

func (p *Peers) syncHistory(id, addr string) {
	var snap storeFile
	if err := p.getJSON(addr, "/api/history", &snap); err != nil {
		log.Printf(L("同步历史失败: %v", "History sync failed: %v"), err)
		return
	}
	p.a.merge(snap, id)
}

type Envelope struct {
	Op      string       `json:"op"` // add | delete | clear | edit | meta | fav | sync
	Item    *Item        `json:"item,omitempty"`
	ID      string       `json:"id,omitempty"`
	TS      int64        `json:"ts,omitempty"`
	Version *TextVersion `json:"version,omitempty"` // edit：新版本
	Snap    *storeFile   `json:"snap,omitempty"`    // sync：从其他设备同步来、需要继续转发的内容
}

// relay 把变更转发给除 except 以外的所有节点。
// 每个节点只转发“自己第一次见到”的变更，所以即使不是全连通也能覆盖整个网络，且不会形成环路。
func (p *Peers) relay(env Envelope, except string) {
	body, _ := json.Marshal(env)
	strict := p.a.cfgSnapshot().StrictEnc
	for _, x := range p.List() {
		if x.ID == except || (strict && !x.Enc) {
			continue
		}
		go func(x *Peer) {
			b := body
			if x.Enc { // 对方支持时，请求体也加密
				b = sealAll(body, p.a.creds().enc)
			}
			req := p.newReq(context.Background(), "POST", x.Addr, "/api/relay", b)
			if x.Enc {
				req.Header.Set("X-Lanvia-Body-Enc", "1")
				req.Header.Set("Content-Type", "application/octet-stream")
			}
			resp, err := p.do(p.hc, req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}(x)
	}
}

// normalizeAddr 把用户填的地址整理成 host:port。可以带 http://、不带端口（默认 8686）。
func normalizeAddr(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "http://"), "https://")
	if i := strings.IndexAny(s, "/#?"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "", errors.New("请填写对方的 IP 地址")
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host, port = strings.Trim(s, "[]"), strconv.Itoa(defaultPort)
	}
	n, err := strconv.Atoi(port)
	if host == "" || err != nil || n < 1 || n > 65535 || strings.ContainsAny(host, " ,;") {
		return "", errors.New("地址格式不对，例如 192.168.1.20 或 192.168.1.20:8686")
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// AddManualPeer 保存一个手动地址并立即试连；连不上也会保存，之后自动重试。
func (a *App) AddManualPeer(raw string) (ManualPeer, error) {
	addr, err := normalizeAddr(raw)
	if err != nil {
		return ManualPeer{}, err
	}
	a.peers.mu.Lock()
	busy := a.peers.probing[addr]
	a.peers.probing[addr] = true
	a.peers.mu.Unlock()
	var info *Info
	if !busy {
		info, err = a.peers.probe(addr, true)
	}
	if err == errSelf || err == errBadCode { // 这两种重试也没用，不保存
		return ManualPeer{Addr: addr}, err
	}
	a.updateCfg(func(c *Config) {
		if !contains(c.Peers, addr) {
			c.Peers = append(c.Peers, addr)
		}
	})
	a.peers.setManual(a.cfgSnapshot().Peers)
	mp := ManualPeer{Addr: addr}
	if info != nil {
		mp.Connected, mp.Name = true, info.Name
	}
	return mp, err
}

func (a *App) RemoveManualPeer(addr string) {
	a.updateCfg(func(c *Config) {
		kept := c.Peers[:0]
		for _, x := range c.Peers {
			if x != addr {
				kept = append(kept, x)
			}
		}
		c.Peers = kept
	})
	a.peers.setManual(a.cfgSnapshot().Peers)
	a.peers.forget(addr)
}

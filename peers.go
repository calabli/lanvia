package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
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
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	OS      string       `json:"os"`
	Addr    string       `json:"addr"`
	Manual  bool         `json:"manual"`
	Clients []ClientInfo `json:"clients"`
	seen    time.Time
	fails   int
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
}

func newPeers(a *App, manual []string) *Peers {
	return &Peers{
		a: a, manual: manual,
		m: map[string]*Peer{}, nearby: map[string]*Nearby{}, probing: map[string]bool{},
		hc: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *Peers) start() {
	go p.listen()
	go p.beaconLoop()
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
		log.Printf("无法监听广播端口 %d（自动发现不可用，可使用 --peer）: %v", beaconPort, err)
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
		x.Addr = addr
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
		log.Printf("无法发送广播: %v", err)
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

// probe 用签名请求访问对方的 /api/info：只有配对码相同才会成功。
func (p *Peers) probe(addr string, manual bool) {
	defer func() { p.mu.Lock(); delete(p.probing, addr); p.mu.Unlock() }()
	info, err := p.fetchInfo(addr)
	if err != nil || info.ID == p.a.cfgSnapshot().ID {
		return
	}
	p.mu.Lock()
	x, existed := p.m[info.ID]
	if !existed {
		x = &Peer{ID: info.ID}
		p.m[info.ID] = x
	}
	x.Name, x.OS, x.Addr, x.Clients, x.seen, x.fails = info.Name, info.OS, addr, info.Clients, time.Now(), 0
	x.Manual = x.Manual || manual
	p.mu.Unlock()
	if !existed {
		log.Printf("已连接设备: %s (%s)", info.Name, addr)
		go p.syncHistory(x.ID, addr)
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
			log.Printf("设备离线: %s", x.Name)
			delete(p.m, id)
		}
		return
	}
	x.Name, x.OS, x.Clients, x.seen, x.fails = info.Name, info.OS, info.Clients, time.Now(), 0
}

// ---- 节点间请求 ----

func (p *Peers) newReq(ctx context.Context, method, addr, path string, body []byte) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, "http://"+addr+path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	signRequest(req, p.a.creds(), p.a.cfgSnapshot().ID)
	return req
}

func (p *Peers) getJSON(addr, path string, v any) error {
	resp, err := p.hc.Do(p.newReq(context.Background(), "GET", addr, path, nil))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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
		log.Printf("同步历史失败: %v", err)
		return
	}
	p.a.merge(snap)
}

type Envelope struct {
	Op   string `json:"op"` // add | delete | clear
	Item *Item  `json:"item,omitempty"`
	ID   string `json:"id,omitempty"`
	TS   int64  `json:"ts,omitempty"`
}

// relay 把变更转发给除 except 以外的所有节点。
// 每个节点只转发“自己第一次见到”的变更，所以即使不是全连通也能覆盖整个网络，且不会形成环路。
func (p *Peers) relay(env Envelope, except string) {
	body, _ := json.Marshal(env)
	for _, x := range p.List() {
		if x.ID == except {
			continue
		}
		go func(addr string) {
			resp, err := p.hc.Do(p.newReq(context.Background(), "POST", addr, "/api/relay", body))
			if err != nil {
				return
			}
			resp.Body.Close()
		}(x.Addr)
	}
}

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"
)

type App struct {
	dir   string
	mu    sync.RWMutex
	cfg   Config
	cr    creds
	store *Store
	hub   *Hub
	peers *Peers
	clip  *ClipWatcher

	onChange    func()
	desktopHook func(action, id string) bool
	saved       map[string]string // 已保存到“下载”的文件：id -> 路径
}

func newApp(dir string, cfg *Config, manualPeers []string) *App {
	a := &App{dir: dir, cfg: *cfg, cr: deriveCreds(cfg.Code), hub: newHub(), saved: map[string]string{}}
	a.store = openStore(dir)
	a.store.setRetention(cfg.HistoryDays)
	a.peers = newPeers(a, manualPeers)
	a.clip = newClipWatcher(a)
	return a
}

func (a *App) cfgSnapshot() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) creds() creds {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cr
}

func (a *App) updateCfg(fn func(*Config)) error {
	a.mu.Lock()
	fn(&a.cfg)
	a.cr = deriveCreds(a.cfg.Code)
	c := a.cfg
	a.mu.Unlock()
	a.hub.Broadcast("settings", c.Settings) // 托盘菜单里改的设置，打开着的窗口也要跟着变
	a.changed()
	return c.save(a.dir)
}

// ---- 内容变更：本地产生、远端转发、历史合并都汇集到这里 ----

func (a *App) newText(text, from string, apply bool) *Item {
	it := &Item{ID: randHex(8), Kind: "text", Text: text, From: from, Origin: a.cfgSnapshot().ID, TS: nowMs()}
	a.ingest(it, "", apply)
	return it
}

// ingest 加入一条新内容。apply 表示内容来自别的设备，应写入本机剪贴板。
func (a *App) ingest(it *Item, fromPeer string, apply bool) bool {
	if !a.store.Add(it) {
		return false
	}
	a.hub.Broadcast("item", it)
	if apply && it.Kind == "text" && a.cfgSnapshot().AutoApply {
		if err := a.clip.Apply(it.Text); err != nil && a.clip.ok {
			log.Printf("写入剪贴板失败: %v", err)
		}
	}
	a.peers.relay(Envelope{Op: "add", Item: it}, fromPeer)
	a.changed()
	log.Printf("%s  %s", it.From, preview(it))
	return true
}

func (a *App) remove(id, fromPeer string) {
	if a.store.Delete(id) {
		a.hub.Broadcast("delete", map[string]string{"id": id})
		a.peers.relay(Envelope{Op: "delete", ID: id}, fromPeer)
		a.changed()
	}
}

func (a *App) clear(ts int64, fromPeer string) {
	if a.store.Clear(ts) {
		a.hub.Broadcast("clear", map[string]int64{"ts": ts})
		a.peers.relay(Envelope{Op: "clear", TS: ts}, fromPeer)
		a.changed()
	}
}

// merge 合并对方的历史：只补齐缺失的记录，不写剪贴板。
func (a *App) merge(s storeFile) {
	if s.ClearedAt > 0 {
		a.clear(s.ClearedAt, "")
	}
	for id := range s.Tomb {
		a.remove(id, "")
	}
	n := 0
	for i := len(s.Items) - 1; i >= 0; i-- {
		it := s.Items[i]
		if !validItem(it) {
			continue
		}
		if a.store.Add(it) {
			a.hub.Broadcast("item", it)
			n++
		} else if it.MetaTS > 0 {
			// 已有的内容：补上对方更新过的保留设置
			if u := a.store.applyMeta(it.ID, it.ExpiresAt, it.Keep, it.MetaTS); u != nil {
				a.hub.Broadcast("update", u)
				n++
			}
		}
	}
	// 合并编辑历史。对方的内容里已经是编辑后的文本，所以这里不能把当前文本当成原始版本
	for id, vs := range s.Versions {
		if it, added, _ := a.store.applyVersions(id, vs, false); added {
			a.hub.Broadcast("update", it)
			n++
		}
	}
	if n > 0 {
		a.changed()
		log.Printf("从对端同步了 %d 条历史", n)
	}
}

func validItem(it *Item) bool {
	if it == nil || !idRe.MatchString(it.ID) || it.Origin == "" {
		return false
	}
	switch it.Kind {
	case "text":
		return len(it.Text) <= maxTextLen && utf8.ValidString(it.Text)
	case "file":
		return it.Name != ""
	}
	return false
}

func preview(it *Item) string {
	if it.Kind == "file" {
		return "[文件] " + it.Name
	}
	r := []rune(it.Text)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return string(r)
}

// ---- 在线设备 ----

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Node bool   `json:"node"` // 运行 Lanvia 的电脑
	Host bool   `json:"host"` // 当前浏览器连接的那台
	Via  string `json:"via,omitempty"`
}

func osKind(goos string) string {
	switch goos {
	case "darwin":
		return "mac"
	case "windows", "linux":
		return goos
	}
	return "web"
}

func (a *App) devices() []Device {
	c := a.cfgSnapshot()
	out := []Device{{ID: c.ID, Name: c.Name, Kind: osKind(runtime.GOOS), Node: true, Host: true}}
	addClients := func(cs []ClientInfo, via string) {
		for _, x := range cs {
			if !x.Local { // 电脑上自己的仪表盘不重复显示
				out = append(out, Device{ID: x.ID, Name: x.Name, Kind: x.Kind, Via: via})
			}
		}
	}
	addClients(a.hub.List(), c.Name)
	for _, p := range a.peers.List() {
		out = append(out, Device{ID: p.ID, Name: p.Name, Kind: osKind(p.OS), Node: true})
		addClients(p.Clients, p.Name)
	}
	return out
}

// pruneLoop 定期清理过期历史。单条可以设成 1 小时后删除，所以每分钟检查一次（只是遍历内存里的列表）。
func (a *App) pruneLoop() {
	for {
		a.prune()
		time.Sleep(time.Minute)
	}
}

// SetItemExpiry 设置单条内容的保留时长并同步到所有设备。ttl 为 0 且 keep 为 false 时恢复跟随全局设置。
func (a *App) SetItemExpiry(id string, ttl time.Duration, keep bool) (*Item, error) {
	if a.store.Get(id) == nil {
		return nil, errors.New("内容不存在")
	}
	now := nowMs()
	var exp int64
	if ttl > 0 && !keep {
		exp = now + ttl.Milliseconds()
	}
	it := a.store.applyMeta(id, exp, keep, now)
	if it == nil {
		return nil, errors.New("设置失败，请重试")
	}
	a.afterMeta(it, "")
	return it, nil
}

func (a *App) afterMeta(it *Item, fromPeer string) {
	a.hub.Broadcast("update", it)
	a.peers.relay(Envelope{Op: "meta", ID: it.ID, Item: &Item{ExpiresAt: it.ExpiresAt, Keep: it.Keep, MetaTS: it.MetaTS}}, fromPeer)
	a.changed()
}

func (a *App) prune() {
	gone := a.store.Prune()
	for _, id := range gone {
		a.hub.Broadcast("delete", map[string]string{"id": id})
	}
	if len(gone) > 0 {
		log.Printf("已清理 %d 条过期历史", len(gone))
		a.changed()
	}
}

func (a *App) presenceLoop() {
	var last []byte
	for {
		time.Sleep(time.Second)
		b, _ := json.Marshal(a.devices())
		if !bytes.Equal(b, last) {
			last = b
			a.hub.Broadcast("devices", json.RawMessage(b))
		}
	}
}

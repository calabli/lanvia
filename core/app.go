package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"runtime"
	"strings"
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
	onIncoming  func(Item) // 其他设备实时发来新内容（桌面通知用）
	desktopHook func(action, id string) bool
	saved       map[string]string // 已保存到“下载”的文件：id -> 路径
	fetch       *fetcher          // 后台下载其他设备的文件

	quotaMu  sync.Mutex // 保护 reserved，以及“腾空间”这一步
	reserved int64      // 正在下载、还没落盘的副本预留的空间（.part 文件不计入占用）
}

func newApp(dir string, cfg *Config, manualPeers []string) *App {
	a := &App{dir: dir, cfg: *cfg, cr: deriveCreds(cfg.Code), hub: newHub(), saved: map[string]string{}, fetch: newFetcher()}
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
	a.bcastItem("item", it)
	if apply && it.Kind == "text" && a.cfgSnapshot().AutoApply && !a.Paused() {
		if err := a.clip.Apply(it.Text); err != nil && a.clip.ok {
			log.Printf(L("写入剪贴板失败: %v", "Writing to clipboard failed: %v"), err)
		}
	}
	a.peers.relay(Envelope{Op: "add", Item: it}, fromPeer)
	if fromPeer != "" && a.wantFetch(it) {
		a.enqueueFetch(it.ID)
	}
	a.changed()
	// 其他设备或手机发来的（不含本机剪贴板和本机仪表盘发出的，也不含历史同步）
	if c := a.cfgSnapshot(); it.From != c.Name {
		a.mu.RLock()
		fn := a.onIncoming
		a.mu.RUnlock()
		if fn != nil {
			go fn(*it)
		}
	}
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
// 合并得到的新内容会再转发给其他设备（除了 from）：手动添加设备时网络可能是一条链（C–A–B），
// 只靠各自连上时拉一次历史，C 的内容到不了 B。转发用同样的“历史”格式，接收方同样走 merge，
// 只转发自己第一次见到的部分，所以不会无限循环。
func (a *App) merge(s storeFile, from string) {
	if s.ClearedAt > 0 {
		a.clear(s.ClearedAt, "")
	}
	for id := range s.Tomb {
		a.remove(id, "")
	}
	var fresh []*Item            // 本机新增或更新过的内容（当前状态）
	touched := map[string]bool{} // 需要连同版本一起转发的内容
	for i := len(s.Items) - 1; i >= 0; i-- {
		it := s.Items[i]
		if !validItem(it) {
			continue
		}
		if a.store.Add(it) {
			a.bcastItem("item", it)
			touched[it.ID] = true
			continue
		}
		// 已有的内容：补上对方更新过的保留设置和收藏
		if it.MetaTS > 0 {
			if u := a.store.applyMeta(it.ID, it.ExpiresAt, it.Keep, it.MetaTS); u != nil {
				a.bcastItem("update", u)
				touched[it.ID] = true
			}
		}
		if it.FavTS > 0 {
			if u := a.store.applyFav(it.ID, it.Fav, it.Title, it.FavTS); u != nil {
				a.bcastItem("update", u)
				touched[it.ID] = true
			}
		}
	}
	// 合并编辑历史。对方的内容里已经是编辑后的文本，所以这里不能把当前文本当成原始版本
	for id, vs := range s.Versions {
		if it, added, _ := a.store.applyVersions(id, vs, false); added {
			a.bcastItem("update", it)
			touched[id] = true
		}
	}
	for id := range touched {
		if it := a.store.Get(id); it != nil {
			fresh = append(fresh, it)
		}
	}
	go a.fetchMissing() // 新设备上线：顺便补下之前没取到的文件
	if len(fresh) == 0 {
		return
	}
	a.changed()
	log.Printf(L("从对端同步了 %d 条历史", "Synced %d items from a peer"), len(fresh))
	a.relaySync(fresh, from)
}

// relaySync 把一批内容（连同各自的全部版本）转发给其他设备，按大小分批，避免单个请求过大。
func (a *App) relaySync(items []*Item, from string) {
	const batchLimit = 1 << 20
	batch, size := storeFile{Versions: map[string][]*TextVersion{}}, 0
	flush := func() {
		if len(batch.Items) > 0 {
			b := batch
			a.peers.relay(Envelope{Op: "sync", Snap: &b}, from)
		}
		batch, size = storeFile{Versions: map[string][]*TextVersion{}}, 0
	}
	for _, it := range items {
		vs := a.store.Versions(it.ID)
		n := len(it.Text) + 300
		for _, v := range vs {
			n += len(v.Text) + 100
		}
		if size > 0 && size+n > batchLimit {
			flush()
		}
		batch.Items = append(batch.Items, it)
		if it.Rev > 0 {
			batch.Versions[it.ID] = vs
		}
		size += n
	}
	flush()
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
		return L("[文件] ", "[file] ") + it.Name
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
	Node bool   `json:"node"`          // 运行 Lanvia 的电脑
	Host bool   `json:"host"`          // 当前浏览器连接的那台
	Enc  bool   `json:"enc,omitempty"` // 与本机之间的传输已加密（节点）
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
		out = append(out, Device{ID: p.ID, Name: p.Name, Kind: osKind(p.OS), Node: true, Enc: p.Enc})
		addClients(p.Clients, p.Name)
	}
	return out
}

// pruneLoop 定期清理过期历史。单条可以设成 1 小时后删除，所以每分钟检查一次（只是遍历内存里的列表）。
func (a *App) pruneLoop() {
	for i := 0; ; i++ {
		a.prune()
		a.autoResume()
		if i%5 == 4 {
			a.enforceQuota() // 顺便清理残留文件
			a.fetchMissing() // 每 5 分钟重试一次没下载成功的文件
		}
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

// SetFavorite 收藏 / 取消收藏一条内容并命名，同步到所有设备。
func (a *App) SetFavorite(id string, fav bool, title string) (*Item, error) {
	if a.store.Get(id) == nil {
		return nil, errors.New("内容不存在")
	}
	it := a.store.applyFav(id, fav, trimRunes(strings.TrimSpace(title), 60), nowMs())
	if it == nil {
		return nil, errors.New("设置失败，请重试")
	}
	a.afterFav(it, "")
	return it, nil
}

func (a *App) afterFav(it *Item, fromPeer string) {
	a.bcastItem("update", it)
	a.peers.relay(Envelope{Op: "fav", ID: it.ID, Item: &Item{Fav: it.Fav, Title: it.Title, FavTS: it.FavTS}}, fromPeer)
	a.changed()
}

// Favorites 返回所有收藏，最新的在前。
func (a *App) Favorites() []Item {
	var out []Item
	for _, it := range a.store.List() {
		if it.Fav {
			out = append(out, *it)
		}
	}
	return out
}

func (a *App) afterMeta(it *Item, fromPeer string) {
	a.bcastItem("update", it)
	a.peers.relay(Envelope{Op: "meta", ID: it.ID, Item: &Item{ExpiresAt: it.ExpiresAt, Keep: it.Keep, MetaTS: it.MetaTS}}, fromPeer)
	a.changed()
}

func (a *App) prune() {
	gone := a.store.Prune()
	for _, id := range gone {
		a.hub.Broadcast("delete", map[string]string{"id": id})
	}
	if len(gone) > 0 {
		log.Printf(L("已清理 %d 条过期历史", "Removed %d expired items"), len(gone))
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

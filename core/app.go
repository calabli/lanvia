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
	onIncoming  func(Item) // new content sent in real time by other devices (for desktop notifications)
	desktopHook func(action, id string) bool
	saved       map[string]string // files saved to the "Downloads" folder: id -> path
	fetch       *fetcher          // downloads files from other devices in the background

	lower lowerCache // lowercased body cache used by full-text search

	quotaMu  sync.Mutex // guards reserved and the "make room" step
	reserved int64      // space reserved for replicas that are downloading and not yet on disk (.part files do not count towards usage)

	nonces   nonceCache   // nonces used by v2 write requests, to prevent replay
	eids     eidCache     // IDs of sync messages already forwarded, see compat.go
	upd      *UpdateState // desktop update state, see update.go
	delivery deliveryLog  // Recent, in-memory receipt results; never part of synced history.
}

// credsFor derives the credentials from the config; a regenerated token takes precedence over one derived from the pairing code.
func credsFor(c *Config) creds {
	cr := deriveCreds(c.Code)
	if c.Token != "" {
		cr.token = c.Token
	}
	return cr
}

func newApp(dir string, cfg *Config, manualPeers []string) *App {
	a := &App{dir: dir, cfg: *cfg, cr: credsFor(cfg), hub: newHub(), saved: map[string]string{}, fetch: newFetcher()}
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
	a.cr = credsFor(&a.cfg)
	c := a.cfg
	a.mu.Unlock()
	a.hub.Broadcast("settings", c.Settings) // settings changed in the tray menu must reach the open windows too
	a.changed()
	return c.save(a.dir)
}

// ---- Content changes: locally produced, remotely forwarded and history-merged changes all funnel through here ----

func (a *App) newText(text, from string, apply bool, sender ...string) *Item {
	it := &Item{ID: randHex(8), Kind: "text", Text: text, From: from, Origin: a.cfgSnapshot().ID, TS: nowMs()}
	a.ingest(it, "", apply, sender...)
	return it
}

// ingest adds a new item. apply means the item comes from another device and should be written to the local clipboard.
func (a *App) ingest(it *Item, fromPeer string, apply bool, sender ...string) bool {
	if !a.store.Add(it) {
		return false
	}
	a.bcastItem("item", it)
	if apply && a.cfgSnapshot().AutoApply && !a.Paused() {
		switch {
		case it.Kind == "text":
			if err := a.clip.Apply(it.Text); err != nil && a.clip.ok {
				log.Printf(L("写入剪贴板失败: %v", "Writing to clipboard failed: %v"), err)
			}
		case it.Kind == "file" && isClipImageMime(it.Mime) && it.Size <= maxClipImage && a.clip.ok:
			go a.applyImage(it) // the image may have to be downloaded from the peer first
		}
	}
	a.peers.relay(Envelope{Op: "add", Item: it}, fromPeer, sender...)
	if fromPeer != "" && a.wantFetch(it) {
		a.enqueueFetch(it.ID)
	}
	a.changed()
	// sent by another device or a phone (not from the local clipboard or the local dashboard, and not from history sync)
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

// maxBatchDelete is the maximum number of items deleted in one batch (after "select all" on the web page).
const maxBatchDelete = 5000

// removeMany deletes in bulk and returns the number of items actually deleted. This machine writes one tombstone per item; the web page only receives a single deletes event and refreshes once;
// other devices still receive one delete per item (older versions understand them too), but each device sends them in order, so it does not open hundreds or thousands of connections at once.
func (a *App) removeMany(ids []string) int {
	var gone []string
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if a.store.Delete(id) {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return 0
	}
	a.hub.Broadcast("deletes", map[string][]string{"ids": gone})
	bodies := make([][]byte, len(gone))
	for i, id := range gone {
		env := Envelope{Op: "delete", ID: id, EID: randHex(8)}
		a.eids.first(env.EID) // do not forward a message of our own that came back around
		bodies[i], _ = json.Marshal(env)
	}
	a.peers.relaySeq(bodies, "")
	a.changed()
	return len(gone)
}

func (a *App) clear(ts int64, fromPeer string) {
	if a.store.Clear(ts) {
		a.hub.Broadcast("clear", map[string]int64{"ts": ts})
		a.peers.relay(Envelope{Op: "clear", TS: ts}, fromPeer)
		a.changed()
	}
}

// merge merges the peer's history: it only fills in missing records and does not touch the clipboard.
// newly merged items are forwarded on to other devices (except from): when devices are added manually the network may be a chain (C–A–B),
// and pulling the history once when each pair connects would never get C's items to B. Forwarding uses the same "history" format and the receiver goes through merge as well,
// forwarding only the parts it has seen for the first time, so it cannot loop forever.
func (a *App) merge(s storeFile, from string) {
	if s.ClearedAt > 0 {
		a.clear(s.ClearedAt, "")
	}
	for id := range s.Tomb {
		a.remove(id, "")
	}
	var fresh []*Item            // items newly added or updated on this machine (current state)
	touched := map[string]bool{} // items that need to be forwarded together with their versions
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
		// existing item: fill in the retention setting, favorite, tags and checklist the peer updated, plus unknown fields missing locally
		if u := a.store.adoptExtra(it.ID, it.Extra); u != nil {
			touched[it.ID] = true
		}
		if len(it.Tags) > 0 {
			if u := a.store.applyTags(it.ID, it.Tags); u != nil {
				a.bcastItem("update", u)
				touched[it.ID] = true
			}
		}
		if it.Kind == "todo" {
			if u := a.store.applyTodo(it.ID, it.Todo, it.Text, it.TextTS); u != nil {
				a.bcastItem("update", u)
				touched[it.ID] = true
			}
		}
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
	// Merge the edit history. The peer's item already holds the edited text, so the current text must not be treated as the original version here
	for id, vs := range s.Versions {
		if it, added, _ := a.store.applyVersions(id, vs, false); added {
			a.bcastItem("update", it)
			touched[id] = true
		}
	}
	for id, cs := range s.Comments {
		if it := a.store.applyComments(id, cs); it != nil {
			a.bcastItem("update", it)
			touched[id] = true
		}
	}
	for id := range touched {
		if it := a.store.Get(id); it != nil {
			fresh = append(fresh, it)
		}
	}
	go a.fetchMissing() // a new device came online: also fetch the files that were missed earlier
	if len(fresh) == 0 {
		return
	}
	a.changed()
	log.Printf(L("从对端同步了 %d 条历史", "Synced %d items from a peer"), len(fresh))
	a.relaySync(fresh, from)
}

// relaySync forwards a batch of items (together with all their versions) to other devices, splitting them by size so no single request gets too large.
func (a *App) relaySync(items []*Item, from string) {
	const batchLimit = 1 << 20
	newBatch := func() storeFile {
		return storeFile{Versions: map[string][]*TextVersion{}, Comments: map[string][]*Comment{}}
	}
	batch, size := newBatch(), 0
	flush := func() {
		if len(batch.Items) > 0 {
			b := batch
			a.peers.relay(Envelope{Op: "sync", Snap: &b}, from)
		}
		batch, size = newBatch(), 0
	}
	for _, it := range items {
		vs, cs := a.store.Versions(it.ID), a.store.allComments(it.ID)
		n := len(it.Text) + 300 + len(it.Todo)*150
		for _, v := range vs {
			n += len(v.Text) + 100
		}
		for _, c := range cs {
			n += len(c.Text) + 100
		}
		if size > 0 && size+n > batchLimit {
			flush()
		}
		batch.Items = append(batch.Items, it)
		if it.Rev > 0 {
			batch.Versions[it.ID] = vs
		}
		if len(cs) > 0 {
			batch.Comments[it.ID] = cs
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
	case "todo":
		return validTodo(it)
	}
	return false
}

func preview(it *Item) string {
	switch it.Kind {
	case "file":
		return L("[文件] ", "[file] ") + it.Name
	case "todo":
		return L("[清单] ", "[list] ") + it.Text
	}
	r := []rune(it.Text)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return string(r)
}

// ---- Online devices ----

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Node bool   `json:"node"`          // a computer running Lanvia
	Host bool   `json:"host"`          // the one the current browser is connected to
	Enc  bool   `json:"enc,omitempty"` // the transfer to and from this machine is encrypted (node)
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
			if !x.Local { // do not show the dashboard of this computer itself again
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

// pruneLoop periodically cleans up expired history. A single item can be set to be deleted after 1 hour, so it checks every minute (just walking the in-memory list).
func (a *App) pruneLoop() {
	for i := 0; ; i++ {
		a.prune()
		a.autoResume()
		if i%5 == 4 {
			a.enforceQuota() // also clean up leftover files
			a.fetchMissing() // retry the files that failed to download every 5 minutes
		}
		time.Sleep(time.Minute)
	}
}

// SetItemExpiry sets the retention duration of a single item and syncs it to all devices. When ttl is 0 and keep is false the item follows the global setting again.
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

// SetFavorite favorites or unfavorites an item and names it, syncing to all devices.
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

// Favorites returns all favorites, newest first.
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

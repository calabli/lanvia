package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Item struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // text | file | todo (checklist, see todo.go)
	Text   string `json:"text,omitempty"`
	Name   string `json:"name,omitempty"`
	Mime   string `json:"mime,omitempty"`
	Size   int64  `json:"size,omitempty"`
	From   string `json:"from"`   // display name of the source device
	Origin string `json:"origin"` // node ID that holds the file content
	TS     int64  `json:"ts"`     // milliseconds

	// Edit info (see versions.go). Text always holds the content of the current version.
	Rev      int    `json:"rev,omitempty"`      // number of times it was edited (version count - 1)
	EditedAt int64  `json:"editedAt,omitempty"` // time of the current version
	EditedBy string `json:"editedBy,omitempty"` // editor of the current version

	// Per-item retention duration, synced to all devices (the newer MetaTS wins). When both are empty, follow the local global setting.
	ExpiresAt int64 `json:"expiresAt,omitempty"` // expiry time (milliseconds)
	Keep      bool  `json:"keep,omitempty"`      // keep forever: unaffected by the global setting and the item count limit
	MetaTS    int64 `json:"metaTs,omitempty"`    // time the retention setting was last changed

	// Favorite (frequent snippet), synced to all devices, the newer FavTS wins. Favorites never expire and are unaffected by the item count limit.
	Fav   bool   `json:"fav,omitempty"`
	Title string `json:"title,omitempty"` // snippet name, e.g. "Home WiFi", usable for search
	FavTS int64  `json:"favTs,omitempty"`

	// number of comments that are not deleted (see comments.go), computed locally from the comments
	Comments int `json:"comments,omitempty"`

	// Tags (see tags.go): tag -> time it was last added (positive) or removed (negative)
	Tags map[string]int64 `json:"tags,omitempty"`

	// Checklist (Kind is todo): the entries, plus the time the title (Text) was last changed
	Todo   []*TodoEntry `json:"todo,omitempty"`
	TextTS int64        `json:"textTs,omitempty"`

	// only present in the replica pushed to the web: full text length when the body is truncated
	TextLen int `json:"textLen,omitempty"`
	// only present in history.json: body too long, stored in texts/<id>.txt
	TextFile bool `json:"textFile,omitempty"`

	// fields this version does not understand (features of later versions), stored and forwarded as-is, see compat.go
	Extra map[string]json.RawMessage `json:"-"`
}

// Each entry in the list pushed to the web carries at most this many bytes of body; longer bodies are fetched in full when previewing or editing (GET /api/items/{id}).
// History sync between nodes still carries the full text.
const previewBytes = 16 << 10

// forClient returns the replica used for the web list: the body is truncated when too long, and TextLen records the full length.
func (it *Item) forClient() *Item {
	if it.Kind != "text" || len(it.Text) <= previewBytes {
		return it
	}
	c := *it
	cut := previewBytes
	for cut > 0 && !utf8.RuneStart(c.Text[cut]) { // do not split a multi-byte character
		cut--
	}
	c.Text, c.TextLen = c.Text[:cut], utf8.RuneCountInString(it.Text)
	return &c
}

// pinned reports whether this item should not be cleaned up automatically: kept forever, favorited, or a checklist.
func (it *Item) pinned() bool { return it.Keep || it.Fav || it.Kind == "todo" }

// expired reports whether an item has expired: the per-item setting wins, otherwise the local global retention duration applies.
func (it *Item) expired(now, keepMs int64) bool {
	switch {
	case it.pinned():
		return false
	case it.ExpiresAt > 0:
		return it.ExpiresAt <= now
	default:
		return keepMs > 0 && it.TS < now-keepMs
	}
}

const (
	maxItems   = 300
	maxTextLen = 2 << 20
	tombTTL    = 7 * 24 * time.Hour
)

var idRe = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

type Store struct {
	mu        sync.Mutex
	path      string
	blobDir   string
	items     []*Item          // newest first (by TS, descending)
	byID      map[string]*Item // indexed by ID, kept in sync with items
	tomb      map[string]int64
	clearedAt int64
	versions  map[string][]*TextVersion // content ID -> history versions (including the original), only for edited items
	textDir   string                    // long bodies: texts/<id>.txt
	verDir    string                    // history versions: versions/<id>.json
	comments  map[string][]*Comment     // content ID -> comments (including the tombstones of deleted ones), oldest first
	comDir    string                    // comments: comments/<id>.json
	dirtyCom  map[string]bool           // items whose comment files need to be written
	dirtyText map[string]bool           // items whose long body files need to be written (or deleted)
	dirtyVer  map[string]bool           // items whose version files need to be written
	seen      map[string]int64          // time each item last changed on this machine (added, edited, favorited, retention setting), used for incremental sync
	keepMs    atomic.Int64              // history retention duration (milliseconds), 0 means forever
	dirty     chan struct{}
}

type storeFile struct {
	Items     []*Item                   `json:"items"`
	Tomb      map[string]int64          `json:"tomb"`
	ClearedAt int64                     `json:"clearedAt"`
	Versions  map[string][]*TextVersion `json:"versions,omitempty"`
	Comments  map[string][]*Comment     `json:"comments,omitempty"`
	Seen      map[string]int64          `json:"seen,omitempty"` // history.json only: time each item last changed on this machine
	Now       int64                     `json:"now,omitempty"`  // sync response only: the responder's current time, where the next incremental sync starts
}

// Storage layout: history.json only holds the index (each item's metadata and short body); bodies over 16 KB go to texts/<id>.txt,
// edit history goes to versions/<id>.json. Each change only rewrites the small files that changed, and the index is written last,
// so even if the power goes out midway, the files the index refers to are already there.
func openStore(dir string) *Store {
	s := &Store{
		path:      filepath.Join(dir, "history.json"),
		blobDir:   filepath.Join(dir, "blobs"),
		textDir:   filepath.Join(dir, "texts"),
		verDir:    filepath.Join(dir, "versions"),
		comDir:    filepath.Join(dir, "comments"),
		comments:  map[string][]*Comment{},
		dirtyCom:  map[string]bool{},
		tomb:      map[string]int64{},
		byID:      map[string]*Item{},
		versions:  map[string][]*TextVersion{},
		dirtyText: map[string]bool{},
		dirtyVer:  map[string]bool{},
		seen:      map[string]int64{},
		dirty:     make(chan struct{}, 1),
	}
	for _, d := range []string{s.blobDir, s.textDir, s.verDir, s.comDir} {
		os.MkdirAll(d, 0o700)
	}
	s.load()
	go s.saver()
	return s
}

func (s *Store) textPath(id string) string { return filepath.Join(s.textDir, id+".txt") }
func (s *Store) verPath(id string) string  { return filepath.Join(s.verDir, id+".json") }
func (s *Store) comPath(id string) string  { return filepath.Join(s.comDir, id+".json") }

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var f storeFile
	if json.Unmarshal(b, &f) != nil {
		return
	}
	s.clearedAt = f.ClearedAt
	if f.Tomb != nil {
		s.tomb = f.Tomb
	}
	migrate := len(f.Versions) > 0 // older versions kept all history versions in history.json
	for _, it := range f.Items {
		if it.TextFile {
			t, err := os.ReadFile(s.textPath(it.ID))
			if err != nil {
				log.Printf(L("找不到正文文件，跳过：%s", "Text file missing, skipped: %s"), it.ID)
				continue
			}
			it.Text, it.TextFile = string(t), false
		} else if len(it.Text) > previewBytes {
			s.dirtyText[it.ID] = true // long body in the old format: move it to its own file on the next save
			migrate = true
		}
		s.items = append(s.items, it)
	}
	known := map[string]bool{}
	now := nowMs()
	sort.SliceStable(s.items, func(i, j int) bool { return s.items[i].TS > s.items[j].TS }) // keep the time ordering, see indexOf
	for _, it := range s.items {
		known[it.ID] = true
		s.byID[it.ID] = it
		if v, ok := f.Seen[it.ID]; ok {
			s.seen[it.ID] = v
		} else {
			s.seen[it.ID] = now // old data has no record: treat it as just changed, so it is synced one extra time at most
		}
	}
	for id, vs := range f.Versions {
		if known[id] {
			s.versions[id], s.dirtyVer[id] = vs, true
		}
	}
	// read the version files and clean up leftover files that have no matching item
	if ents, err := os.ReadDir(s.verDir); err == nil {
		for _, e := range ents {
			id := strings.TrimSuffix(e.Name(), ".json")
			if !known[id] {
				os.Remove(filepath.Join(s.verDir, e.Name()))
				continue
			}
			if _, done := s.versions[id]; done {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(s.verDir, e.Name())); err == nil {
				var vs []*TextVersion
				if json.Unmarshal(b, &vs) == nil && len(vs) > 0 {
					s.versions[id] = vs
				}
			}
		}
	}
	// comment files; the comment counts are recomputed from the files
	if ents, err := os.ReadDir(s.comDir); err == nil {
		for _, e := range ents {
			id := strings.TrimSuffix(e.Name(), ".json")
			if !known[id] {
				os.Remove(filepath.Join(s.comDir, e.Name()))
				continue
			}
			if b, err := os.ReadFile(filepath.Join(s.comDir, e.Name())); err == nil {
				var cs []*Comment
				if json.Unmarshal(b, &cs) == nil {
					s.comments[id] = cs
				}
			}
		}
	}
	for _, it := range s.items {
		it.Comments = liveComments(s.comments[it.ID])
	}
	if ents, err := os.ReadDir(s.textDir); err == nil {
		for _, e := range ents {
			if !known[strings.TrimSuffix(e.Name(), ".txt")] {
				os.Remove(filepath.Join(s.textDir, e.Name()))
			}
		}
	}
	if migrate {
		log.Print(L("正在把历史记录迁移为新的存储格式", "Migrating history to the new storage format"))
		s.markDirty()
	}
}

// dropFiles deletes all files attached to an item on disk (the caller holds the lock).
func (s *Store) dropFiles(id string) {
	os.Remove(s.blobPath(id))
	os.Remove(s.thumbPath(id))
	os.Remove(s.textPath(id))
	os.Remove(s.verPath(id))
	os.Remove(s.comPath(id))
	delete(s.versions, id)
	delete(s.comments, id)
	delete(s.dirtyCom, id)
	delete(s.dirtyText, id)
	delete(s.dirtyVer, id)
	delete(s.seen, id)
}

func (s *Store) blobPath(id string) string { return filepath.Join(s.blobDir, id) }

func (s *Store) hasBlob(id string) bool {
	_, err := os.Stat(s.blobPath(id))
	return err == nil
}

// Add inserts a record; duplicate, deleted, or older than the "clear" timestamp records are ignored.
func (s *Store) Add(it *Item) bool {
	if !idRe.MatchString(it.ID) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dead := s.tomb[it.ID]; dead || it.TS <= s.clearedAt {
		return false
	}
	if it.expired(nowMs(), s.keepMs.Load()) { // expired items are no longer accepted (including ones synced from other devices)
		return false
	}
	if s.byID[it.ID] != nil {
		return false
	}
	it.Comments = liveComments(s.comments[it.ID]) // the comment count from the peer does not count; comments are merged separately afterwards
	if it.Kind == "todo" {
		it.Todo, _ = mergeTodo(nil, it.Todo) // tidy the order and drop the text of tombstoned entries
	}
	if len(it.Tags) > 0 {
		it.Tags, _ = mergeTags(nil, it.Tags) // drop invalid tags
	}
	i := sort.Search(len(s.items), func(i int) bool { return s.items[i].TS < it.TS })
	s.items = append(s.items, nil)
	copy(s.items[i+1:], s.items[i:])
	s.items[i] = it
	s.byID[it.ID] = it
	s.touch(it.ID)
	if len(it.Text) > previewBytes {
		s.dirtyText[it.ID] = true
	}
	// when over the limit, delete the oldest first, skipping the ones kept forever
	for i := len(s.items) - 1; len(s.items) > maxItems && i >= 0; i-- {
		if old := s.items[i]; !old.pinned() {
			s.removeAt(i)
			s.dropFiles(old.ID)
		}
	}
	s.markDirty()
	return true
}

// Delete reports whether this is a new deletion (one that still needs to be propagated to other nodes).
func (s *Store) Delete(id string) bool {
	if !idRe.MatchString(id) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dead := s.tomb[id]; dead {
		return false
	}
	s.tomb[id] = time.Now().UnixMilli()
	if i := s.indexOf(id); i >= 0 {
		s.removeAt(i)
	}
	s.dropFiles(id)
	s.markDirty()
	return true
}

func (s *Store) Clear(ts int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts <= s.clearedAt {
		return false
	}
	s.clearedAt = ts
	kept := s.items[:0]
	for _, x := range s.items {
		if x.TS > ts {
			kept = append(kept, x)
		} else {
			delete(s.byID, x.ID)
			s.dropFiles(x.ID)
		}
	}
	for i := len(kept); i < len(s.items); i++ {
		s.items[i] = nil // release the cleared items
	}
	s.items = kept
	s.markDirty()
	return true
}

func (s *Store) setRetention(days int) {
	s.keepMs.Store(int64(days) * 24 * int64(time.Hour/time.Millisecond))
}

// Prune deletes expired items (together with their files and history versions) and returns the deleted IDs.
// Expiry is a local setting: no tombstone is written and other devices are not notified.
func (s *Store) Prune() []string {
	now, k := nowMs(), s.keepMs.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone []string
	kept := s.items[:0]
	for _, x := range s.items {
		if x.expired(now, k) {
			gone = append(gone, x.ID)
			delete(s.byID, x.ID)
			s.dropFiles(x.ID)
		} else {
			kept = append(kept, x)
		}
	}
	for i := len(kept); i < len(s.items); i++ {
		s.items[i] = nil
	}
	s.items = kept
	if len(gone) > 0 {
		s.markDirty()
	}
	return gone
}

// newerMeta reports whether a retention setting is newer than x's current one. On a tie (several devices changed within the same millisecond) a fixed rule
// decides: keep forever wins first, then the later expiry time. All devices use the same rule, so the result is consistent.
func newerMeta(ts, expiresAt int64, keep bool, x *Item) bool {
	if ts != x.MetaTS {
		return ts > x.MetaTS
	}
	if keep != x.Keep {
		return keep
	}
	return !keep && expiresAt > x.ExpiresAt
}

// applyFav updates the favorite state and title, accepting only newer changes; on a tie favoriting wins, then the larger title. All devices end up with the same result.
func (s *Store) applyFav(id string, fav bool, title string, ts int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return nil
	}
	x := s.items[i]
	newer := ts > x.FavTS || (ts == x.FavTS && (fav && !x.Fav || fav == x.Fav && title > x.Title))
	if !newer {
		return nil
	}
	n := *x
	n.Fav, n.Title, n.FavTS = fav, title, ts
	if !fav {
		n.Title = ""
	}
	s.setAt(i, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

// applyMeta updates an item's retention setting, accepting only changes newer than the current one.
func (s *Store) applyMeta(id string, expiresAt int64, keep bool, metaTS int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 || !newerMeta(metaTS, expiresAt, keep, s.items[i]) {
		return nil
	}
	n := *s.items[i] // copy on write
	n.ExpiresAt, n.Keep, n.MetaTS = expiresAt, keep, metaTS
	if keep {
		n.ExpiresAt = 0
	}
	s.setAt(i, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

func (s *Store) Get(id string) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

// indexOf returns the position of an item in s.items, or -1 when not found (the caller holds the lock).
// items are sorted newest to oldest: look up the time by ID first, then binary search for the position.
func (s *Store) indexOf(id string) int {
	it := s.byID[id]
	if it == nil {
		return -1
	}
	i := sort.Search(len(s.items), func(i int) bool { return s.items[i].TS <= it.TS })
	for ; i < len(s.items) && s.items[i].TS == it.TS; i++ {
		if s.items[i].ID == id {
			return i
		}
	}
	for i, x := range s.items { // fall back to a linear scan when the order is broken (should not happen)
		if x.ID == id {
			return i
		}
	}
	return -1
}

// setAt replaces the i-th entry with a new replica (copy on write, the caller holds the lock).
func (s *Store) setAt(i int, n *Item) {
	s.items[i] = n
	s.byID[n.ID] = n
}

// removeAt removes the i-th entry (the caller holds the lock).
func (s *Store) removeAt(i int) {
	delete(s.byID, s.items[i].ID)
	s.items = append(s.items[:i], s.items[i+1:]...)
}

func (s *Store) List() []*Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Item(nil), s.items...)
}

func (s *Store) LatestText() *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.items {
		if x.Kind == "text" {
			return x
		}
	}
	return nil
}

// Snapshot is used to sync history between nodes.
// Snapshot returns the items changed after since (local time), together with their versions and deletion records; when since <= 0 it returns everything.
// Times always use the locally recorded "last change time", so they do not depend on the clocks of the individual devices, and they also cover the case where an old item was just synced in from another device.
func (s *Store) Snapshot(since int64) storeFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := storeFile{Items: []*Item{}, Tomb: map[string]int64{}, ClearedAt: s.clearedAt, Versions: map[string][]*TextVersion{}, Comments: map[string][]*Comment{}, Now: nowMs()}
	for k, v := range s.tomb {
		if v > since {
			out.Tomb[k] = v
		}
	}
	for _, it := range s.items {
		if since > 0 && s.seen[it.ID] <= since {
			continue
		}
		out.Items = append(out.Items, it)
		if vs, ok := s.versions[it.ID]; ok {
			out.Versions[it.ID] = append([]*TextVersion(nil), vs...)
		}
		if cs, ok := s.comments[it.ID]; ok && len(cs) > 0 {
			out.Comments[it.ID] = append([]*Comment(nil), cs...)
		}
	}
	return out
}

// touch records that an item changed on this machine (the caller holds the lock).
func (s *Store) touch(id string) { s.seen[id] = nowMs() }

func (s *Store) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *Store) saver() {
	for range s.dirty {
		time.Sleep(300 * time.Millisecond) // merge the writes that happen within a short time
		s.Flush()
	}
}

func (s *Store) Flush() {
	type textJob struct {
		id, text string
		large    bool
	}
	s.mu.Lock()
	cut := time.Now().Add(-tombTTL).UnixMilli()
	for k, v := range s.tomb {
		if v < cut {
			delete(s.tomb, k)
		}
	}
	// prepare the data to write inside the lock, and write the files outside the lock
	index := make([]*Item, len(s.items))
	byID := make(map[string]*Item, len(s.items))
	for i, it := range s.items {
		byID[it.ID] = it
		if len(it.Text) > previewBytes {
			c := *it
			c.Text, c.TextFile = "", true
			index[i] = &c
		} else {
			index[i] = it
		}
	}
	var texts []textJob
	for id := range s.dirtyText {
		if it := byID[id]; it != nil {
			texts = append(texts, textJob{id, it.Text, len(it.Text) > previewBytes})
		}
	}
	vers := map[string][]*TextVersion{}
	for id := range s.dirtyVer {
		if vs, ok := s.versions[id]; ok && byID[id] != nil {
			vers[id] = append([]*TextVersion(nil), vs...)
		}
	}
	coms := map[string][]*Comment{}
	for id := range s.dirtyCom {
		if cs, ok := s.comments[id]; ok && byID[id] != nil {
			coms[id] = append([]*Comment(nil), cs...)
		}
	}
	s.dirtyText, s.dirtyVer, s.dirtyCom = map[string]bool{}, map[string]bool{}, map[string]bool{}
	b, err := json.Marshal(storeFile{Items: index, Tomb: s.tomb, ClearedAt: s.clearedAt, Seen: s.seen})
	s.mu.Unlock()
	if err != nil {
		return
	}

	failed := false
	for _, t := range texts {
		if !t.large {
			os.Remove(s.textPath(t.id)) // it got shorter after an edit
		} else if writeAtomic(s.textPath(t.id), []byte(t.text)) != nil {
			failed = true
		}
	}
	for id, vs := range vers {
		vb, _ := json.Marshal(vs)
		if writeAtomic(s.verPath(id), vb) != nil {
			failed = true
		}
	}
	for id, cs := range coms {
		cb, _ := json.Marshal(cs)
		if writeAtomic(s.comPath(id), cb) != nil {
			failed = true
		}
	}
	if failed { // do not update the index when the attached files could not be written, so the index never points at a missing file; try again next time
		log.Print(L("保存历史失败：写入正文或版本文件出错", "Saving history failed: couldn't write text or version files"))
		s.mu.Lock()
		for _, t := range texts {
			s.dirtyText[t.id] = true
		}
		for id := range vers {
			s.dirtyVer[id] = true
		}
		for id := range coms {
			s.dirtyCom[id] = true
		}
		s.mu.Unlock()
		s.markDirty()
		return
	}
	if err := writeAtomic(s.path, b); err != nil {
		log.Printf(L("保存历史失败: %v", "Saving history failed: %v"), err)
	}
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

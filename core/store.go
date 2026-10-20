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
	Kind   string `json:"kind"` // text | file
	Text   string `json:"text,omitempty"`
	Name   string `json:"name,omitempty"`
	Mime   string `json:"mime,omitempty"`
	Size   int64  `json:"size,omitempty"`
	From   string `json:"from"`   // 来源设备的显示名
	Origin string `json:"origin"` // 持有文件内容的节点 ID
	TS     int64  `json:"ts"`     // 毫秒

	// 编辑信息（见 versions.go）。Text 始终是当前版本的内容。
	Rev      int    `json:"rev,omitempty"`      // 被编辑过的次数（版本数 - 1）
	EditedAt int64  `json:"editedAt,omitempty"` // 当前版本的时间
	EditedBy string `json:"editedBy,omitempty"` // 当前版本的编辑者

	// 单条保留时长，同步到所有设备（以 MetaTS 较新的为准）。都为空时跟随本机的全局设置。
	ExpiresAt int64 `json:"expiresAt,omitempty"` // 到期时间（毫秒）
	Keep      bool  `json:"keep,omitempty"`      // 永久保留：不受全局设置和数量上限影响
	MetaTS    int64 `json:"metaTs,omitempty"`    // 最近一次修改保留设置的时间

	// 收藏（常用片段），同步到所有设备，以 FavTS 较新的为准。收藏的内容不会过期，也不受数量上限影响。
	Fav   bool   `json:"fav,omitempty"`
	Title string `json:"title,omitempty"` // 片段名称，例如“家里 WiFi”，可用于搜索
	FavTS int64  `json:"favTs,omitempty"`

	// 只出现在推给网页的副本里：正文被截断时的全文字数
	TextLen int `json:"textLen,omitempty"`
	// 只出现在 history.json 里：正文太长，存在 texts/<id>.txt
	TextFile bool `json:"textFile,omitempty"`
}

// 推给网页的列表里每条最多带这么多字节的正文；更长的在预览、编辑时再取全文（GET /api/items/{id}）。
// 节点之间同步历史仍然传全文。
const previewBytes = 16 << 10

// forClient 返回给网页列表用的副本：正文过长时截断，TextLen 记下全文字数。
func (it *Item) forClient() *Item {
	if it.Kind != "text" || len(it.Text) <= previewBytes {
		return it
	}
	c := *it
	cut := previewBytes
	for cut > 0 && !utf8.RuneStart(c.Text[cut]) { // 不切断多字节字符
		cut--
	}
	c.Text, c.TextLen = c.Text[:cut], utf8.RuneCountInString(it.Text)
	return &c
}

// pinned 表示这条内容不应被自动清理：永久保留或已收藏。
func (it *Item) pinned() bool { return it.Keep || it.Fav }

// expired 判断一条内容是否已过期：单条设置优先，否则按本机的全局保留时长。
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
	items     []*Item // 新的在前
	tomb      map[string]int64
	clearedAt int64
	versions  map[string][]*TextVersion // 内容 ID -> 历史版本（含原始版本），只有被编辑过的才有
	textDir   string                    // 长正文：texts/<id>.txt
	verDir    string                    // 历史版本：versions/<id>.json
	dirtyText map[string]bool           // 需要写入（或删除）长正文文件的内容
	dirtyVer  map[string]bool           // 需要写入版本文件的内容
	keepMs    atomic.Int64              // 历史保留时长（毫秒），0 表示永久
	dirty     chan struct{}
}

type storeFile struct {
	Items     []*Item                   `json:"items"`
	Tomb      map[string]int64          `json:"tomb"`
	ClearedAt int64                     `json:"clearedAt"`
	Versions  map[string][]*TextVersion `json:"versions,omitempty"`
}

// 保存方式：history.json 只放索引（各条内容的元数据和短正文）；超过 16 KB 的正文放在 texts/<id>.txt，
// 编辑历史放在 versions/<id>.json。每次变化只重写改动过的小文件，最后再写索引，
// 所以即使中途断电，索引引用的文件也都已经在了。
func openStore(dir string) *Store {
	s := &Store{
		path:      filepath.Join(dir, "history.json"),
		blobDir:   filepath.Join(dir, "blobs"),
		textDir:   filepath.Join(dir, "texts"),
		verDir:    filepath.Join(dir, "versions"),
		tomb:      map[string]int64{},
		versions:  map[string][]*TextVersion{},
		dirtyText: map[string]bool{},
		dirtyVer:  map[string]bool{},
		dirty:     make(chan struct{}, 1),
	}
	for _, d := range []string{s.blobDir, s.textDir, s.verDir} {
		os.MkdirAll(d, 0o700)
	}
	s.load()
	go s.saver()
	return s
}

func (s *Store) textPath(id string) string { return filepath.Join(s.textDir, id+".txt") }
func (s *Store) verPath(id string) string  { return filepath.Join(s.verDir, id+".json") }

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
	migrate := len(f.Versions) > 0 // 旧版本把历史版本都放在 history.json 里
	for _, it := range f.Items {
		if it.TextFile {
			t, err := os.ReadFile(s.textPath(it.ID))
			if err != nil {
				log.Printf("找不到正文文件，跳过：%s", it.ID)
				continue
			}
			it.Text, it.TextFile = string(t), false
		} else if len(it.Text) > previewBytes {
			s.dirtyText[it.ID] = true // 旧格式的长正文：下次保存时移到单独的文件
			migrate = true
		}
		s.items = append(s.items, it)
	}
	known := map[string]bool{}
	for _, it := range s.items {
		known[it.ID] = true
	}
	for id, vs := range f.Versions {
		if known[id] {
			s.versions[id], s.dirtyVer[id] = vs, true
		}
	}
	// 读取版本文件，顺便清理没有对应内容的残留文件
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
	if ents, err := os.ReadDir(s.textDir); err == nil {
		for _, e := range ents {
			if !known[strings.TrimSuffix(e.Name(), ".txt")] {
				os.Remove(filepath.Join(s.textDir, e.Name()))
			}
		}
	}
	if migrate {
		log.Printf("正在把历史记录迁移为新的存储格式")
		s.markDirty()
	}
}

// dropFiles 删除一条内容在磁盘上的所有附属文件（调用方持有锁）。
func (s *Store) dropFiles(id string) {
	os.Remove(s.blobPath(id))
	os.Remove(s.textPath(id))
	os.Remove(s.verPath(id))
	delete(s.versions, id)
	delete(s.dirtyText, id)
	delete(s.dirtyVer, id)
}

func (s *Store) blobPath(id string) string { return filepath.Join(s.blobDir, id) }

func (s *Store) hasBlob(id string) bool {
	_, err := os.Stat(s.blobPath(id))
	return err == nil
}

// Add 插入一条记录；重复、已删除或早于“清空”时间点的记录会被忽略。
func (s *Store) Add(it *Item) bool {
	if !idRe.MatchString(it.ID) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dead := s.tomb[it.ID]; dead || it.TS <= s.clearedAt {
		return false
	}
	if it.expired(nowMs(), s.keepMs.Load()) { // 已过期的不再收（包括其他设备同步来的）
		return false
	}
	for _, x := range s.items {
		if x.ID == it.ID {
			return false
		}
	}
	i := sort.Search(len(s.items), func(i int) bool { return s.items[i].TS < it.TS })
	s.items = append(s.items, nil)
	copy(s.items[i+1:], s.items[i:])
	s.items[i] = it
	if len(it.Text) > previewBytes {
		s.dirtyText[it.ID] = true
	}
	// 超过上限时从最旧的开始删，跳过“永久保留”的
	for i := len(s.items) - 1; len(s.items) > maxItems && i >= 0; i-- {
		if old := s.items[i]; !old.pinned() {
			s.items = append(s.items[:i], s.items[i+1:]...)
			s.dropFiles(old.ID)
		}
	}
	s.markDirty()
	return true
}

// Delete 返回 true 表示这是一次新的删除（需要继续向其他节点传播）。
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
	for i, x := range s.items {
		if x.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			break
		}
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
			s.dropFiles(x.ID)
		}
	}
	s.items = kept
	s.markDirty()
	return true
}

func (s *Store) setRetention(days int) {
	s.keepMs.Store(int64(days) * 24 * int64(time.Hour/time.Millisecond))
}

// Prune 删除过期的内容（连同文件和历史版本），返回被删除的 ID。
// 过期是本机的设置，不写墓碑、不通知其他设备。
func (s *Store) Prune() []string {
	now, k := nowMs(), s.keepMs.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone []string
	kept := s.items[:0]
	for _, x := range s.items {
		if x.expired(now, k) {
			gone = append(gone, x.ID)
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

// newerMeta 判断一次保留设置是否比 x 现有的更新。时间相同（多台设备同一毫秒修改）时按固定规则
// 决胜：永久保留优先，其次到期时间更晚的优先。所有设备用同样的规则，结果一致。
func newerMeta(ts, expiresAt int64, keep bool, x *Item) bool {
	if ts != x.MetaTS {
		return ts > x.MetaTS
	}
	if keep != x.Keep {
		return keep
	}
	return !keep && expiresAt > x.ExpiresAt
}

// applyFav 更新收藏状态和名称，只接受更新的修改；时间相同时收藏优先、名称较大的优先，各设备结果一致。
func (s *Store) applyFav(id string, fav bool, title string, ts int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.items {
		if x.ID != id {
			continue
		}
		newer := ts > x.FavTS || (ts == x.FavTS && (fav && !x.Fav || fav == x.Fav && title > x.Title))
		if !newer {
			return nil
		}
		n := *x
		n.Fav, n.Title, n.FavTS = fav, title, ts
		if !fav {
			n.Title = ""
		}
		s.items[i] = &n
		s.markDirty()
		return &n
	}
	return nil
}

// applyMeta 更新一条内容的保留设置，只接受比现有设置更新的修改。
func (s *Store) applyMeta(id string, expiresAt int64, keep bool, metaTS int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.items {
		if x.ID != id {
			continue
		}
		if !newerMeta(metaTS, expiresAt, keep, x) {
			return nil
		}
		n := *x // 写时复制
		n.ExpiresAt, n.Keep, n.MetaTS = expiresAt, keep, metaTS
		if keep {
			n.ExpiresAt = 0
		}
		s.items[i] = &n
		s.markDirty()
		return &n
	}
	return nil
}

func (s *Store) Get(id string) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.items {
		if x.ID == id {
			return x
		}
	}
	return nil
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

// Snapshot 用于节点间同步历史。
func (s *Store) Snapshot() storeFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := make(map[string]int64, len(s.tomb))
	for k, v := range s.tomb {
		t[k] = v
	}
	v := make(map[string][]*TextVersion, len(s.versions))
	for k, vs := range s.versions {
		v[k] = append([]*TextVersion(nil), vs...)
	}
	return storeFile{Items: append([]*Item(nil), s.items...), Tomb: t, ClearedAt: s.clearedAt, Versions: v}
}

func (s *Store) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *Store) saver() {
	for range s.dirty {
		time.Sleep(300 * time.Millisecond) // 合并短时间内的多次写入
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
	// 在锁里整理好要写的数据，写文件放到锁外
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
	s.dirtyText, s.dirtyVer = map[string]bool{}, map[string]bool{}
	b, err := json.Marshal(storeFile{Items: index, Tomb: s.tomb, ClearedAt: s.clearedAt})
	s.mu.Unlock()
	if err != nil {
		return
	}

	failed := false
	for _, t := range texts {
		if !t.large {
			os.Remove(s.textPath(t.id)) // 编辑后变短了
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
	if failed { // 附属文件没写成功就不更新索引，避免索引指向不存在的文件；下次再试
		log.Printf("保存历史失败：写入正文或版本文件出错")
		s.mu.Lock()
		for _, t := range texts {
			s.dirtyText[t.id] = true
		}
		for id := range vers {
			s.dirtyVer[id] = true
		}
		s.mu.Unlock()
		s.markDirty()
		return
	}
	if err := writeAtomic(s.path, b); err != nil {
		log.Printf("保存历史失败: %v", err)
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

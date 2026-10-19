package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
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
	dirty     chan struct{}
}

type storeFile struct {
	Items     []*Item          `json:"items"`
	Tomb      map[string]int64 `json:"tomb"`
	ClearedAt int64            `json:"clearedAt"`
}

func openStore(dir string) *Store {
	s := &Store{
		path:    filepath.Join(dir, "history.json"),
		blobDir: filepath.Join(dir, "blobs"),
		tomb:    map[string]int64{},
		dirty:   make(chan struct{}, 1),
	}
	os.MkdirAll(s.blobDir, 0o700)
	if b, err := os.ReadFile(s.path); err == nil {
		var f storeFile
		if json.Unmarshal(b, &f) == nil {
			s.items, s.clearedAt = f.Items, f.ClearedAt
			if f.Tomb != nil {
				s.tomb = f.Tomb
			}
		}
	}
	go s.saver()
	return s
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
	for _, x := range s.items {
		if x.ID == it.ID {
			return false
		}
	}
	i := sort.Search(len(s.items), func(i int) bool { return s.items[i].TS < it.TS })
	s.items = append(s.items, nil)
	copy(s.items[i+1:], s.items[i:])
	s.items[i] = it
	for len(s.items) > maxItems {
		old := s.items[len(s.items)-1]
		s.items = s.items[:len(s.items)-1]
		os.Remove(s.blobPath(old.ID))
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
	os.Remove(s.blobPath(id))
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
			os.Remove(s.blobPath(x.ID))
		}
	}
	s.items = kept
	s.markDirty()
	return true
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
	return storeFile{Items: append([]*Item(nil), s.items...), Tomb: t, ClearedAt: s.clearedAt}
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
	s.mu.Lock()
	cut := time.Now().Add(-tombTTL).UnixMilli()
	for k, v := range s.tomb {
		if v < cut {
			delete(s.tomb, k)
		}
	}
	b, err := json.Marshal(storeFile{Items: s.items, Tomb: s.tomb, ClearedAt: s.clearedAt})
	s.mu.Unlock()
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("保存历史失败: %v", err)
		return
	}
	os.Rename(tmp, s.path)
}

package core

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 标签：每条内容可以有若干标签，同步到所有设备，可以按标签筛选和搜索。
//
// Item.Tags 记录每个标签最近一次被添加或移除的时间：正数表示添加于该时间，负数表示移除于该时间（墓碑）。
// 同步时按标签各自取时间较新的一方（时间相同时“添加”优先），所以两台设备同时给同一条内容
// 一边加 A、一边删 B，两个修改都会保留；删掉的标签也不会被同步回来。
// 旧版本不认识这个字段，会忽略它（经过旧版本转发的内容会丢掉标签，直接相连的新版本之间照常同步）。

const (
	maxTags       = 20 // 每条内容最多的标签数
	maxTagEntries = 60 // 含墓碑最多保留这么多
	maxTagLen     = 30 // 每个标签最多的字数
)

// normalizeTag 整理用户输入的标签：去掉开头的 #、首尾空白，连续空白合并成一个，过长截断。不合法时返回空。
func normalizeTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#＃")
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '，' }), " ")
	if utf8.RuneCountInString(s) > maxTagLen {
		s = string([]rune(s)[:maxTagLen])
	}
	if !utf8.ValidString(s) {
		return ""
	}
	return strings.TrimSpace(s)
}

func absTS(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// LiveTags 返回当前的标签（按名称排序）。
func (it *Item) LiveTags() []string {
	var out []string
	for k, v := range it.Tags {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// mergeTags 把 in 并入 cur，返回新的表和是否有变化。cur 不会被修改。
func mergeTags(cur, in map[string]int64) (map[string]int64, bool) {
	var out map[string]int64
	for k, v := range in {
		if v == 0 || k == "" || normalizeTag(k) != k {
			continue
		}
		old, ok := cur[k]
		if ok && (absTS(v) < absTS(old) || (absTS(v) == absTS(old) && (v == old || old > 0))) {
			continue // 本机的更新，或者一样
		}
		if out == nil {
			out = make(map[string]int64, len(cur)+len(in))
			for ck, cv := range cur {
				out[ck] = cv
			}
		}
		out[k] = v
	}
	if out == nil {
		return cur, false
	}
	// 太多时先丢最旧的墓碑
	for len(out) > maxTagEntries {
		oldest, ts := "", int64(0)
		for k, v := range out {
			if v < 0 && (oldest == "" || -v < ts || (-v == ts && k < oldest)) {
				oldest, ts = k, -v
			}
		}
		if oldest == "" {
			break
		}
		delete(out, oldest)
	}
	return out, true
}

// applyTags 把标签的变化并入一条内容。返回更新后的内容副本，没有变化时返回 nil。
func (s *Store) applyTags(id string, in map[string]int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 {
		return nil
	}
	tags, changed := mergeTags(s.items[i].Tags, in)
	if !changed {
		return nil
	}
	n := *s.items[i] // 写时复制
	n.Tags = tags
	s.setAt(i, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

// SetTags 在本机给一条内容添加、移除标签，并同步给所有设备。
func (a *App) SetTags(id string, add, remove []string) (*Item, error) {
	cur := a.store.Get(id)
	if cur == nil {
		return nil, errors.New("内容不存在")
	}
	live := cur.LiveTags()
	// 和已有标签只差大小写的，沿用已有的写法（例如已经有 Work，再加 work 时还是 Work）
	known := map[string]string{}
	for _, t := range a.AllTags() {
		known[strings.ToLower(t.Name)] = t.Name
	}
	now := nowMs()
	ch := map[string]int64{}
	for _, t := range remove {
		if t = normalizeTag(t); t != "" && cur.Tags[t] > 0 {
			ch[t] = -max(now, cur.Tags[t]+1)
		}
	}
	n := len(live)
	for _, t := range add {
		if t = normalizeTag(t); t == "" {
			continue
		}
		if k, ok := known[strings.ToLower(t)]; ok {
			t = k
		}
		if cur.Tags[t] > 0 || ch[t] > 0 {
			continue
		}
		if n >= maxTags {
			return nil, errors.New("每条内容最多 20 个标签")
		}
		n++
		ch[t] = max(now, absTS(cur.Tags[t])+1)
	}
	if len(ch) == 0 {
		return cur, nil
	}
	it := a.store.applyTags(id, ch)
	if it == nil {
		return cur, nil
	}
	a.afterTags(it, ch, "")
	return it, nil
}

func (a *App) afterTags(it *Item, ch map[string]int64, fromPeer string) {
	a.bcastItem("update", it)
	a.peers.relay(Envelope{Op: "tags", ID: it.ID, Item: &Item{Tags: ch}}, fromPeer)
	a.changed()
}

// TagCount 是一个标签和用到它的内容数。
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AllTags 返回所有在用的标签，按使用次数从多到少。
func (a *App) AllTags() []TagCount {
	cnt := map[string]int{}
	for _, it := range a.store.List() {
		for _, t := range it.LiveTags() {
			cnt[t]++
		}
	}
	out := make([]TagCount, 0, len(cnt))
	for k, v := range cnt {
		out = append(out, TagCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

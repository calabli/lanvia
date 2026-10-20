package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// TextVersion 是一条文本内容的一个版本。
//
// 同步规则（多台设备同时编辑也能收敛到同一结果）：
//   - 版本集合只增不减，按 ID 去重，各节点之间求并集；
//   - 当前内容取 (TS, ID) 最大的版本，所以同时编辑时较晚的一方生效，另一方保留在历史里；
//   - 原始内容作为第一个版本，ID 与内容 ID 相同。它在第一次被编辑时由各节点按同样的规则生成，
//     所以哪台设备生成的都一样。
type TextVersion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	By   string `json:"by"`
	TS   int64  `json:"ts"`
}

const maxVersions = 50

func newer(a, b *TextVersion) bool {
	if a.TS != b.TS {
		return a.TS > b.TS
	}
	return a.ID > b.ID
}

func validVersion(v *TextVersion) bool {
	return v != nil && idRe.MatchString(v.ID) && v.TS > 0 && len(v.Text) <= maxTextLen && utf8.ValidString(v.Text)
}

// applyVersions 把一组版本并入某条内容。synthOriginal 为 true 时，如果这条内容还没有历史，
// 先把当前文本记为原始版本（只有在确实没见过任何编辑时，当前文本才是原始内容）。
// 返回更新后的内容副本、是否有新版本、当前内容是否改变。
func (s *Store) applyVersions(id string, in []*TextVersion, synthOriginal bool) (*Item, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, x := range s.items {
		if x.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 || s.items[idx].Kind != "text" {
		return nil, false, false
	}
	it := s.items[idx]
	vs := s.versions[id]
	if len(vs) == 0 && synthOriginal {
		vs = []*TextVersion{{ID: it.ID, Text: it.Text, By: it.From, TS: it.TS}}
	}
	seen := make(map[string]bool, len(vs))
	for _, v := range vs {
		seen[v.ID] = true
	}
	added := false
	for _, v := range in {
		if validVersion(v) && !seen[v.ID] {
			seen[v.ID] = true
			vs = append(vs, v)
			added = true
		}
	}
	if !added {
		return nil, false, false
	}
	sort.Slice(vs, func(i, j int) bool { return newer(vs[j], vs[i]) }) // 旧的在前
	// 版本太多时丢掉最旧的编辑，但保留原始版本
	if len(vs) > maxVersions {
		keep := []*TextVersion{}
		if vs[0].ID == id {
			keep = append(keep, vs[0])
		}
		vs = append(keep, vs[len(vs)-(maxVersions-len(keep)):]...)
	}
	s.versions[id] = vs

	cur := vs[len(vs)-1]
	n := *it // 写时复制：旧的指针可能正被其他 goroutine 序列化
	n.Text, n.Rev = cur.Text, len(vs)-1
	n.EditedAt, n.EditedBy = 0, ""
	if cur.ID != it.ID {
		n.EditedAt, n.EditedBy = cur.TS, cur.By
	}
	s.items[idx] = &n
	s.markDirty()
	return &n, true, n.Text != it.Text
}

// Versions 返回某条内容的所有版本，新的在前。没被编辑过时只有原始版本。
func (s *Store) Versions(id string) []*TextVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.versions[id]
	if len(vs) == 0 {
		for _, x := range s.items {
			if x.ID == id && x.Kind == "text" {
				return []*TextVersion{{ID: x.ID, Text: x.Text, By: x.From, TS: x.TS}}
			}
		}
		return nil
	}
	out := make([]*TextVersion, len(vs))
	for i, v := range vs {
		out[len(vs)-1-i] = v
	}
	return out
}

// isLatest 判断这条内容是否是最新的一条（也就是各设备剪贴板里的那条）。
func (s *Store) isLatest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items) > 0 && s.items[0].ID == id
}

// EditItem 在本机修改一条文本，生成新版本并同步给所有设备。
func (a *App) EditItem(id, text, by string) (*Item, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("内容不能为空")
	}
	if len(text) > maxTextLen || !utf8.ValidString(text) {
		return nil, errors.New("内容过长")
	}
	cur := a.store.Get(id)
	if cur == nil || cur.Kind != "text" {
		return nil, errors.New("内容不存在或不能编辑")
	}
	if cur.Text == text {
		return cur, nil
	}
	v := &TextVersion{ID: randHex(8), Text: text, By: by, TS: nowMs()}
	it, _, changed := a.store.applyVersions(id, []*TextVersion{v}, true)
	if it == nil {
		return nil, errors.New("内容不存在或不能编辑")
	}
	a.afterEdit(it, changed, v, "")
	return it, nil
}

// applyRemoteEdit 处理其他节点转发来的编辑。
func (a *App) applyRemoteEdit(id string, v *TextVersion, fromPeer string) {
	if it, added, changed := a.store.applyVersions(id, []*TextVersion{v}, true); added {
		a.afterEdit(it, changed, v, fromPeer)
	}
}

func (a *App) afterEdit(it *Item, changed bool, v *TextVersion, fromPeer string) {
	a.bcastItem("update", it)
	// 改的是最新一条时，各电脑剪贴板里的内容也跟着更新
	if changed && a.store.isLatest(it.ID) && a.cfgSnapshot().AutoApply {
		a.clip.Apply(it.Text)
	}
	a.peers.relay(Envelope{Op: "edit", ID: it.ID, Version: v}, fromPeer)
	a.changed()
}

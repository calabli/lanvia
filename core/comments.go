package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// Comment 是对一条内容的评论。
//
// 同步规则与编辑历史相同（多台设备同时评论、删除也能收敛到同一结果）：
//   - 评论集合按 ID 去重，各节点之间求并集；
//   - 删除不真的删掉，而是记下删除时间（墓碑），这样删除也能传播，不会被别的设备同步回来；
//   - 同一条评论两边的删除时间取较早的非零值，所以哪边先合并结果都一样。
type Comment struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	By   string `json:"by"`
	TS   int64  `json:"ts"`
	Del  int64  `json:"del,omitempty"` // 删除时间，0 表示未删除
}

const (
	maxComments   = 200 // 每条内容最多保留的评论数（含已删除的墓碑）
	maxCommentLen = 4000
)

func validComment(c *Comment) bool {
	return c != nil && idRe.MatchString(c.ID) && c.TS > 0 && c.Del >= 0 &&
		len(c.Text) <= maxCommentLen && utf8.ValidString(c.Text) && len(c.By) <= 200 && utf8.ValidString(c.By)
}

func liveComments(cs []*Comment) int {
	n := 0
	for _, c := range cs {
		if c.Del == 0 {
			n++
		}
	}
	return n
}

// applyComments 把一组评论（新增或删除）并入某条内容。返回更新后的内容副本，没有变化时返回 nil。
func (s *Store) applyComments(id string, in []*Comment) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexOf(id)
	if idx < 0 {
		return nil
	}
	cs := s.comments[id]
	byID := make(map[string]*Comment, len(cs))
	for _, c := range cs {
		byID[c.ID] = c
	}
	changed := false
	for _, c := range in {
		if !validComment(c) {
			continue
		}
		old := byID[c.ID]
		if old == nil {
			n := *c
			cs = append(cs, &n)
			byID[c.ID] = &n
			changed = true
			continue
		}
		// 已有的评论：只可能多出一个删除标记
		if c.Del > 0 && (old.Del == 0 || c.Del < old.Del) {
			n := *old // 写时复制
			n.Del, n.Text = c.Del, ""
			for i := range cs {
				if cs[i] == old {
					cs[i] = &n
				}
			}
			byID[c.ID] = &n
			changed = true
		}
	}
	if !changed {
		return nil
	}
	sort.Slice(cs, func(i, j int) bool { // 旧的在前
		if cs[i].TS != cs[j].TS {
			return cs[i].TS < cs[j].TS
		}
		return cs[i].ID < cs[j].ID
	})
	// 太多时先丢最旧的墓碑，再丢最旧的评论
	for len(cs) > maxComments {
		drop := 0
		for i, c := range cs {
			if c.Del > 0 {
				drop = i
				break
			}
		}
		cs = append(cs[:drop], cs[drop+1:]...)
	}
	s.comments[id] = cs
	s.dirtyCom[id] = true
	s.touch(id)
	n := *s.items[idx]
	n.Comments = liveComments(cs)
	s.setAt(idx, &n)
	s.markDirty()
	return &n
}

// Comments 返回某条内容未删除的评论，旧的在前。
func (s *Store) Comments(id string) []*Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*Comment{}
	for _, c := range s.comments[id] {
		if c.Del == 0 {
			out = append(out, c)
		}
	}
	return out
}

// allComments 返回某条内容的全部评论（含墓碑），用于同步。
func (s *Store) allComments(id string) []*Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Comment(nil), s.comments[id]...)
}

// AddComment 在本机给一条内容添加评论，并同步给所有设备。
func (a *App) AddComment(id, text, by string) (*Comment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("评论不能为空")
	}
	if len(text) > maxCommentLen || !utf8.ValidString(text) {
		return nil, errors.New("评论过长")
	}
	c := &Comment{ID: randHex(8), Text: text, By: trimRunes(by, 40), TS: nowMs()}
	it := a.store.applyComments(id, []*Comment{c})
	if it == nil {
		return nil, errors.New("内容不存在")
	}
	a.afterComment(it, c, "")
	return c, nil
}

// DeleteComment 删除一条评论（所有设备上都删除）。
func (a *App) DeleteComment(id, cid string) error {
	var cur *Comment
	for _, c := range a.store.Comments(id) {
		if c.ID == cid {
			cur = c
		}
	}
	if cur == nil {
		return errors.New("评论不存在")
	}
	c := *cur
	c.Del = max(nowMs(), c.TS)
	if it := a.store.applyComments(id, []*Comment{&c}); it != nil {
		a.afterComment(it, &c, "")
	}
	return nil
}

func (a *App) applyRemoteComment(id string, c *Comment, fromPeer string) {
	if it := a.store.applyComments(id, []*Comment{c}); it != nil {
		a.afterComment(it, c, fromPeer)
	}
}

func (a *App) afterComment(it *Item, c *Comment, fromPeer string) {
	a.bcastItem("update", it) // 网页据此更新评论数；正在看这条的预览会重新取评论
	a.peers.relay(Envelope{Op: "comment", ID: it.ID, Comment: c}, fromPeer)
	a.changed()
}

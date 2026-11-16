package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// Comment is a comment on an item.
//
// The sync rules are the same as for the edit history (several devices commenting and deleting at the same time still converge on the same result):
//   - the comment set is deduplicated by ID and unioned across nodes;
//   - a delete does not really remove the comment, it records the deletion time (a tombstone), so the deletion can spread and is not synced back by another device;
//   - both sides take the earlier non-zero deletion time for the same comment, so the result of merging is the same.
type Comment struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	By   string `json:"by"`
	TS   int64  `json:"ts"`
	Del  int64  `json:"del,omitempty"` // deletion time, 0 means not deleted
}

const (
	maxComments   = 200 // maximum number of comments kept per item (including the tombstones of deleted ones)
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

// applyComments merges a set of comments (additions or deletions) into an item. It returns the updated copy of the item, or nil when nothing changed.
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
		// an existing comment: it can only gain a deletion marker
		if c.Del > 0 && (old.Del == 0 || c.Del < old.Del) {
			n := *old // copy on write
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
	sort.Slice(cs, func(i, j int) bool { // oldest first
		if cs[i].TS != cs[j].TS {
			return cs[i].TS < cs[j].TS
		}
		return cs[i].ID < cs[j].ID
	})
	// when there are too many, drop the oldest tombstones first, then the oldest comments
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

// Comments returns the comments of an item that are not deleted, oldest first.
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

// allComments returns all comments of an item (including the tombstones), for syncing.
func (s *Store) allComments(id string) []*Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Comment(nil), s.comments[id]...)
}

// AddComment adds a comment to an item on this machine and syncs it to every device.
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

// DeleteComment deletes a comment (on every device).
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
	a.bcastItem("update", it) // the web page updates the comment count from this; the preview of this item reloads the comments
	a.peers.relay(Envelope{Op: "comment", ID: it.ID, Comment: c}, fromPeer)
	a.changed()
}

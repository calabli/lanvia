package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// TextVersion is one version of a text item.
//
// Sync rules (several devices editing at the same time still converge on the same result):
//   - the version set only grows, is deduplicated by ID and unioned across nodes;
//   - the current text is the version with the largest (TS, ID), so the later edit wins and the other one is kept in the history;
//   - the original text is the first version, with the same ID as the item; every node generates it with the same rule on the first edit,
//     so the result is the same everywhere.
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

// applyVersions merges a set of versions into an item. When synthOriginal is true and the item has
// no history yet, the current text is recorded as the original version (only when no edit was really
// seen is the current text the original one). It returns the updated copy of the item, whether there are new versions and whether the current text changed.
func (s *Store) applyVersions(id string, in []*TextVersion, synthOriginal bool) (*Item, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexOf(id)
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
	sort.Slice(vs, func(i, j int) bool { return newer(vs[j], vs[i]) }) // oldest first
	// drop the oldest edits when there are too many versions, but keep the original version
	if len(vs) > maxVersions {
		keep := []*TextVersion{}
		if vs[0].ID == id {
			keep = append(keep, vs[0])
		}
		vs = append(keep, vs[len(vs)-(maxVersions-len(keep)):]...)
	}
	s.versions[id] = vs
	s.dirtyVer[id] = true
	s.touch(id)

	cur := vs[len(vs)-1]
	n := *it // copy on write: the old pointer may be serialized by another goroutine right now
	n.Text, n.Rev = cur.Text, len(vs)-1
	n.EditedAt, n.EditedBy = 0, ""
	if cur.ID != it.ID {
		n.EditedAt, n.EditedBy = cur.TS, cur.By
	}
	s.setAt(idx, &n)
	if n.Text != it.Text && (len(n.Text) > previewBytes || len(it.Text) > previewBytes) {
		s.dirtyText[id] = true // the long text changed, or it switched between long and short
	}
	s.markDirty()
	return &n, true, n.Text != it.Text
}

// Versions returns all versions of an item, newest first. An item that was never edited has only the original version.
func (s *Store) Versions(id string) []*TextVersion {
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.versions[id]
	if len(vs) == 0 {
		if x := s.byID[id]; x != nil && x.Kind == "text" {
			return []*TextVersion{{ID: x.ID, Text: x.Text, By: x.From, TS: x.TS}}
		}
		return nil
	}
	out := make([]*TextVersion, len(vs))
	for i, v := range vs {
		out[len(vs)-1-i] = v
	}
	return out
}

// isLatest reports whether an item is the newest one (the one in the clipboard of every device).
func (s *Store) isLatest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items) > 0 && s.items[0].ID == id
}

// EditItem edits a text on this machine, creates a new version and syncs it to every device.
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

// applyRemoteEdit handles an edit forwarded by another node.
func (a *App) applyRemoteEdit(id string, v *TextVersion, fromPeer string) {
	if it, added, changed := a.store.applyVersions(id, []*TextVersion{v}, true); added {
		a.afterEdit(it, changed, v, fromPeer)
	}
}

func (a *App) afterEdit(it *Item, changed bool, v *TextVersion, fromPeer string) {
	a.bcastItem("update", it)
	// when the newest item was changed, the clipboard on every computer is updated too
	if changed && a.store.isLatest(it.ID) && a.cfgSnapshot().AutoApply {
		a.clip.Apply(it.Text)
	}
	a.peers.relay(Envelope{Op: "edit", ID: it.ID, Version: v}, fromPeer)
	a.changed()
}

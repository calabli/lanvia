package core

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tags: each entry can carry a number of tags, which are synced to all devices and can be used to filter and search.
//
// Item.Tags records the time when each tag was last added or removed: a positive value means it was added at that time, a negative value means it was removed at that time (tombstone).
// During sync each tag keeps whichever side has the newer timestamp (on a tie "added" wins), so when two devices change the same entry at the same time,
// one adding A and the other removing B, both changes are kept; a removed tag is not synced back either.
// Older versions do not know this field and ignore it (content relayed through an old version loses its tags, while directly connected new versions still sync them as usual).

const (
	maxTags       = 20 // maximum number of tags per entry
	maxTagEntries = 60 // at most this many entries are kept, tombstones included
	maxTagLen     = 30 // maximum number of characters per tag
)

// normalizeTag cleans up a tag entered by the user: drops a leading #, trims the surrounding whitespace, collapses runs of whitespace into one and truncates it when too long. Returns an empty string when it is not valid.
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

// LiveTags returns the current tags, sorted by name.
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

// mergeTags merges in into cur and returns the new map together with whether anything changed. cur is not modified.
func mergeTags(cur, in map[string]int64) (map[string]int64, bool) {
	var out map[string]int64
	for k, v := range in {
		if v == 0 || k == "" || normalizeTag(k) != k {
			continue
		}
		old, ok := cur[k]
		if ok && (absTS(v) < absTS(old) || (absTS(v) == absTS(old) && (v == old || old > 0))) {
			continue // the local update, or the same
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
	// when there are too many, drop the oldest tombstones first
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

// applyTags merges the tag changes into an entry. It returns a copy of the updated entry, or nil when nothing changed.
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
	n := *s.items[i] // copy on write
	n.Tags = tags
	s.setAt(i, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

// SetTags adds and removes tags of an entry on this machine and syncs the result to all devices.
func (a *App) SetTags(id string, add, remove []string) (*Item, error) {
	cur := a.store.Get(id)
	if cur == nil {
		return nil, errors.New("内容不存在")
	}
	live := cur.LiveTags()
	// tags that differ from an existing one only in case reuse the existing spelling (for example, with Work already present, adding work still yields Work)
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

// TagCount is a tag together with the number of entries using it.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AllTags returns all tags in use, sorted by usage count from most to least used.
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

package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// TodoEntry is one entry of a checklist (to-dos, shopping list, things to bring): a checklist is an item with Kind "todo", where Text is the checklist title and Todo holds the entries.
//
// The sync rules merge by "entry", so two devices ticking, adding or editing different entries at the same time never overwrite each other:
//   - entries are deduplicated by ID and unioned across nodes, ordered by creation time;
//   - the text and the tick state each take the newer modification time (ties are broken by a fixed rule);
//   - a deletion is recorded as a tombstone (taking the earlier deletion time), and deleted entries are not synced back;
//   - the title takes the newer TextTS.
//
// Checklists are never cleaned up automatically because of the retention duration or the item limit (see pinned); they can only be deleted by hand.
// Older versions do not know this kind of item and simply ignore it (not shown, not forwarded).
type TodoEntry struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	TextTS int64  `json:"textTs"` // time the text was last changed
	Done   bool   `json:"done,omitempty"`
	DoneTS int64  `json:"doneTs,omitempty"` // time the tick state was last changed
	By     string `json:"by,omitempty"`     // who added it
	TS     int64  `json:"ts"`               // creation time, which determines the order
	Del    int64  `json:"del,omitempty"`    // deletion time, 0 means not deleted
}

const (
	maxTodoEntries = 300 // at most this many entries that are not deleted
	maxTodoKept    = 500 // at most this many are kept, tombstones included
	maxTodoText    = 1000
	maxTodoTitle   = 200
)

func validTodoEntry(e *TodoEntry) bool {
	return e != nil && idRe.MatchString(e.ID) && e.TS > 0 && e.TextTS >= 0 && e.DoneTS >= 0 && e.Del >= 0 &&
		len(e.Text) <= maxTodoText && utf8.ValidString(e.Text) && len(e.By) <= 200 && utf8.ValidString(e.By)
}

func validTodo(it *Item) bool {
	if len(it.Text) > maxTodoTitle*4 || !utf8.ValidString(it.Text) || len(it.Todo) > maxTodoKept {
		return false
	}
	for _, e := range it.Todo {
		if !validTodoEntry(e) {
			return false
		}
	}
	return true
}

// mergeTodoEntry merges the two states of the same entry and returns the result (independent of the argument order).
func mergeTodoEntry(a, b *TodoEntry) TodoEntry {
	m := *a
	if b.TextTS > a.TextTS || (b.TextTS == a.TextTS && b.Text > a.Text) {
		m.Text, m.TextTS = b.Text, b.TextTS
	}
	if b.DoneTS > a.DoneTS || (b.DoneTS == a.DoneTS && b.Done && !a.Done) {
		m.Done, m.DoneTS = b.Done, b.DoneTS
	}
	if b.Del > 0 && (m.Del == 0 || b.Del < m.Del) {
		m.Del = b.Del
	}
	if m.Del > 0 {
		m.Text = "" // a tombstone needs no content
	}
	return m
}

func sortTodo(es []*TodoEntry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].TS != es[j].TS {
			return es[i].TS < es[j].TS
		}
		return es[i].ID < es[j].ID
	})
}

// mergeTodo merges in into cur and returns the new list plus whether anything changed. cur is not modified.
func mergeTodo(cur, in []*TodoEntry) ([]*TodoEntry, bool) {
	out := append([]*TodoEntry(nil), cur...)
	idx := make(map[string]int, len(out))
	for i, e := range out {
		idx[e.ID] = i
	}
	changed := false
	for _, e := range in {
		if !validTodoEntry(e) {
			continue
		}
		i, ok := idx[e.ID]
		if !ok {
			n := mergeTodoEntry(e, e)
			idx[e.ID] = len(out)
			out = append(out, &n)
			changed = true
			continue
		}
		if m := mergeTodoEntry(out[i], e); m != *out[i] {
			out[i] = &m
			changed = true
		}
	}
	if !changed {
		return cur, false
	}
	sortTodo(out)
	// when there are too many, drop the oldest tombstone first
	for len(out) > maxTodoKept {
		drop := -1
		for i, e := range out {
			if e.Del > 0 {
				drop = i
				break
			}
		}
		if drop < 0 {
			drop = 0
		}
		out = append(out[:drop], out[drop+1:]...)
	}
	return out, true
}

func liveTodo(es []*TodoEntry) int {
	n := 0
	for _, e := range es {
		if e.Del == 0 {
			n++
		}
	}
	return n
}

// applyTodo merges the changes to a checklist (some entries, the title) into the local copy. title only takes effect when titleTS is newer.
// It returns the updated copy of the item, or nil when nothing changed.
func (s *Store) applyTodo(id string, entries []*TodoEntry, title string, titleTS int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexOf(id)
	if idx < 0 || s.items[idx].Kind != "todo" {
		return nil
	}
	it := s.items[idx]
	es, changed := mergeTodo(it.Todo, entries)
	n := *it // copy on write
	n.Todo = es
	if titleTS > it.TextTS || (titleTS == it.TextTS && titleTS > 0 && title > it.Text) {
		if title != it.Text || titleTS != it.TextTS {
			n.Text, n.TextTS = title, titleTS
			changed = true
		}
	}
	if !changed {
		return nil
	}
	s.setAt(idx, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

// PlainText is the plain text form of an item: a checklist becomes a list of "- [x] entry" lines, used for copying, notifications and the tray menu.
func (it *Item) PlainText() string {
	switch it.Kind {
	case "file":
		return it.Name
	case "todo":
		var sb strings.Builder
		sb.WriteString(it.Text)
		for _, e := range it.Todo {
			if e.Del > 0 {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			if e.Done {
				sb.WriteString("- [x] ")
			} else {
				sb.WriteString("- [ ] ")
			}
			sb.WriteString(e.Text)
		}
		return sb.String()
	}
	return it.Text
}

func cleanTodoText(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	for len(s) > limit {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// parseTodoLine strips the common markers at the start of a checklist line ("- ", "* ", "1. ", "[ ]", "[x]", "☐", "☑") and returns the text and whether it is done.
func parseTodoLine(l string) (string, bool) {
	l = strings.TrimSpace(l)
	for _, p := range []string{"- ", "* ", "• ", "+ "} {
		l = strings.TrimPrefix(l, p)
	}
	if i := strings.Index(l, ". "); i > 0 && i <= 3 && strings.Trim(l[:i], "0123456789") == "" {
		l = l[i+2:]
	}
	done := false
	for _, p := range []string{"[x] ", "[X] ", "☑ ", "✅ ", "✔ "} {
		if strings.HasPrefix(l, p) {
			l, done = l[len(p):], true
		}
	}
	for _, p := range []string{"[ ] ", "[] ", "☐ "} {
		l = strings.TrimPrefix(l, p)
	}
	return strings.TrimSpace(l), done
}

// NewTodo creates a checklist. lines holds the initial entries, which may carry markers like "- [ ]".
func (a *App) NewTodo(title string, lines []string, from string, sender ...string) (*Item, error) {
	title = cleanTodoText(title, maxTodoTitle*4)
	now := nowMs()
	var es []*TodoEntry
	for i, l := range lines {
		text, done := parseTodoLine(l)
		if text = cleanTodoText(text, maxTodoText); text == "" {
			continue
		}
		if len(es) >= maxTodoEntries {
			return nil, errors.New("清单最多 300 项")
		}
		ts := now + int64(i) // keep the original order
		e := &TodoEntry{ID: randHex(8), Text: text, TextTS: ts, By: from, TS: ts, Done: done}
		if done {
			e.DoneTS = ts
		}
		es = append(es, e)
	}
	if title == "" && len(es) == 0 {
		return nil, errors.New("清单不能为空")
	}
	it := &Item{ID: randHex(8), Kind: "todo", Text: title, TextTS: now, Todo: es, From: from, Origin: a.cfgSnapshot().ID, TS: now}
	if !a.ingest(it, "", false, sender...) {
		return nil, errors.New("创建失败")
	}
	return it, nil
}

// TodoChange is one change to a checklist (sent by the web page).
type TodoChange struct {
	Add   []string `json:"add"`   // entries to add (several lines at once are allowed)
	ID    string   `json:"id"`    // the entry to modify
	Done  *bool    `json:"done"`  // tick / untick
	Text  *string  `json:"text"`  // change the text
	Del   bool     `json:"del"`   // delete this entry
	Title *string  `json:"title"` // change the title
	Clear bool     `json:"clear"` // delete all completed entries
	By    string   `json:"by"`
}

// UpdateTodo changes a checklist on this machine and syncs it to all devices.
func (a *App) UpdateTodo(id string, ch TodoChange) (*Item, error) {
	cur := a.store.Get(id)
	if cur == nil || cur.Kind != "todo" {
		return nil, errors.New("清单不存在")
	}
	now := nowMs()
	var es []*TodoEntry
	live := liveTodo(cur.Todo)
	next := now // new entries are placed after the existing ones (even if this machine's clock is behind the devices that added them)
	for _, e := range cur.Todo {
		next = max(next, e.TS+1)
	}
	for i, l := range ch.Add {
		text, done := parseTodoLine(l)
		if text = cleanTodoText(text, maxTodoText); text == "" {
			continue
		}
		if live >= maxTodoEntries {
			return nil, errors.New("清单最多 300 项")
		}
		live++
		ts := next + int64(i)
		e := &TodoEntry{ID: randHex(8), Text: text, TextTS: ts, By: trimRunes(ch.By, 40), TS: ts, Done: done}
		if done {
			e.DoneTS = ts
		}
		es = append(es, e)
	}
	var old *TodoEntry
	for _, e := range cur.Todo {
		if e.ID == ch.ID && e.Del == 0 {
			old = e
		}
	}
	if ch.ID != "" {
		if old == nil {
			return nil, errors.New("这一项不存在")
		}
		e := *old
		switch {
		case ch.Del:
			e.Del = max(now, e.TS)
		case ch.Done != nil:
			e.Done, e.DoneTS = *ch.Done, max(now, e.DoneTS+1)
		case ch.Text != nil:
			t := cleanTodoText(*ch.Text, maxTodoText)
			if t == "" {
				e.Del = max(now, e.TS) // clearing the text means deleting
			} else {
				e.Text, e.TextTS = t, max(now, e.TextTS+1)
			}
		}
		es = append(es, &e)
	}
	if ch.Clear {
		for _, x := range cur.Todo {
			if x.Done && x.Del == 0 {
				e := *x
				e.Del = max(now, e.TS)
				es = append(es, &e)
			}
		}
	}
	title, titleTS := cur.Text, cur.TextTS
	if ch.Title != nil {
		title, titleTS = cleanTodoText(*ch.Title, maxTodoTitle*4), max(now, cur.TextTS+1)
	}
	it := a.store.applyTodo(id, es, title, titleTS)
	if it == nil {
		return cur, nil
	}
	a.afterTodo(it, es, title, titleTS, ch.Title != nil, "")
	return it, nil
}

func (a *App) applyRemoteTodo(env *Item, fromPeer string) {
	if it := a.store.applyTodo(env.ID, env.Todo, env.Text, env.TextTS); it != nil {
		a.afterTodo(it, env.Todo, env.Text, env.TextTS, env.TextTS > 0, fromPeer)
	}
}

// afterTodo notifies the web page and forwards only the changed entries to other devices.
func (a *App) afterTodo(it *Item, es []*TodoEntry, title string, titleTS int64, withTitle bool, fromPeer string) {
	a.bcastItem("update", it)
	d := &Item{ID: it.ID, Kind: "todo", Todo: es}
	if withTitle {
		d.Text, d.TextTS = title, titleTS
	}
	a.peers.relay(Envelope{Op: "todo", ID: it.ID, Item: d}, fromPeer)
	a.changed()
}

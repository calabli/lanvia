package core

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// 清单（待办、购物清单、出门要带的东西）：Kind 为 "todo" 的内容，Text 是清单标题，Todo 是各项。
//
// 同步规则按“项”合并，所以两台设备同时勾选、添加、修改不同的项都不会互相覆盖：
//   - 项按 ID 去重，各节点之间求并集，顺序按创建时间；
//   - 文字、勾选状态各自按修改时间取新（时间相同按固定规则决胜）；
//   - 删除记为墓碑（取较早的删除时间），删除的项不会被同步回来；
//   - 标题按 TextTS 取新。
//
// 清单不会因为保留时长或数量上限被自动清理（见 pinned），只能手动删除。
// 旧版本不认识这种内容，会直接忽略（不显示、不转发）。
type TodoEntry struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	TextTS int64  `json:"textTs"` // 文字最近一次修改的时间
	Done   bool   `json:"done,omitempty"`
	DoneTS int64  `json:"doneTs,omitempty"` // 勾选状态最近一次修改的时间
	By     string `json:"by,omitempty"`     // 添加者
	TS     int64  `json:"ts"`               // 创建时间，决定顺序
	Del    int64  `json:"del,omitempty"`    // 删除时间，0 表示未删除
}

const (
	maxTodoEntries = 300 // 未删除的项最多这么多
	maxTodoKept    = 500 // 含墓碑最多保留这么多
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

// mergeTodoEntry 合并同一项的两个状态，返回合并结果（与参数顺序无关）。
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
		m.Text = "" // 墓碑不需要内容
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

// mergeTodo 把 in 并入 cur，返回新的列表和是否有变化。cur 不会被修改。
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
	// 太多时先丢最旧的墓碑
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

// applyTodo 把清单的变化（若干项、标题）并入本机。title 只在 titleTS 更新时生效。
// 返回更新后的内容副本，没有变化时返回 nil。
func (s *Store) applyTodo(id string, entries []*TodoEntry, title string, titleTS int64) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.indexOf(id)
	if idx < 0 || s.items[idx].Kind != "todo" {
		return nil
	}
	it := s.items[idx]
	es, changed := mergeTodo(it.Todo, entries)
	n := *it // 写时复制
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

// PlainText 是一条内容的纯文本形式：清单会变成“- [x] 项”的列表，用于复制、通知和托盘菜单。
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

// parseTodoLine 去掉清单行前面常见的标记（“- ”“* ”“1. ”“[ ]”“[x]”“☐”“☑”），返回文字和是否已完成。
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

// NewTodo 新建一个清单。lines 是初始的各项，可以带“- [ ]”之类的标记。
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
		ts := now + int64(i) // 保持原来的顺序
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

// TodoChange 是对清单的一次修改（网页发来的）。
type TodoChange struct {
	Add   []string `json:"add"`   // 新增的项（可以一次加多行）
	ID    string   `json:"id"`    // 要修改的项
	Done  *bool    `json:"done"`  // 勾选 / 取消勾选
	Text  *string  `json:"text"`  // 修改文字
	Del   bool     `json:"del"`   // 删除这一项
	Title *string  `json:"title"` // 修改标题
	Clear bool     `json:"clear"` // 删除所有已完成的项
	By    string   `json:"by"`
}

// UpdateTodo 在本机修改清单，并同步给所有设备。
func (a *App) UpdateTodo(id string, ch TodoChange) (*Item, error) {
	cur := a.store.Get(id)
	if cur == nil || cur.Kind != "todo" {
		return nil, errors.New("清单不存在")
	}
	now := nowMs()
	var es []*TodoEntry
	live := liveTodo(cur.Todo)
	next := now // 新增的项排在已有的项后面（即使本机时钟比添加那些项的设备慢）
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
				e.Del = max(now, e.TS) // 文字清空等于删除
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

// afterTodo 通知网页，并只把变化的项转发给其他设备。
func (a *App) afterTodo(it *Item, es []*TodoEntry, title string, titleTS int64, withTitle bool, fromPeer string) {
	a.bcastItem("update", it)
	d := &Item{ID: it.ID, Kind: "todo", Todo: es}
	if withTitle {
		d.Text, d.TextTS = title, titleTS
	}
	a.peers.relay(Envelope{Op: "todo", ID: it.ID, Item: d}, fromPeer)
	a.changed()
}

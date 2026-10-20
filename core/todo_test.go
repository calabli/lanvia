package core

import (
	"testing"
)

func todoStore(t *testing.T) (*Store, string) {
	s := openStore(t.TempDir())
	const id = "b000000000000001"
	s.Add(&Item{ID: id, Kind: "todo", Text: "购物", TextTS: 1, From: "x", Origin: "o", TS: nowMs(), Todo: []*TodoEntry{
		{ID: "e000000000000001", Text: "牛奶", TextTS: 10, TS: 10},
		{ID: "e000000000000002", Text: "鸡蛋", TextTS: 11, TS: 11},
	}})
	return s, id
}

func entry(s *Store, id, eid string) TodoEntry {
	for _, e := range s.Get(id).Todo {
		if e.ID == eid {
			return *e
		}
	}
	return TodoEntry{}
}

// 两台设备同时做不同的修改，以不同顺序合并后结果一致，且谁的修改都没丢
func TestTodoConverge(t *testing.T) {
	a, id := todoStore(t)
	b, _ := todoStore(t)
	changes := [][]*TodoEntry{
		{{ID: "e000000000000001", Text: "牛奶", TextTS: 10, TS: 10, Done: true, DoneTS: 100}},  // A 勾选牛奶
		{{ID: "e000000000000001", Text: "全脂牛奶", TextTS: 101, TS: 10}},                        // B 同时改了牛奶的文字
		{{ID: "e000000000000002", Text: "鸡蛋", TextTS: 11, TS: 11, Del: 102}},                 // B 删了鸡蛋
		{{ID: "e000000000000002", Text: "一打鸡蛋", TextTS: 103, TS: 11}},                        // A 晚一点改鸡蛋（删除仍然生效）
		{{ID: "e000000000000003", Text: "面包", TextTS: 104, TS: 104, By: "iPhone"}},           // 新增
		{{ID: "e000000000000001", Text: "牛奶", TextTS: 10, TS: 10, Done: false, DoneTS: 100}}, // 同一时间取消勾选：勾选优先
	}
	for _, c := range changes {
		a.applyTodo(id, c, "", 0)
	}
	for i := len(changes) - 1; i >= 0; i-- {
		b.applyTodo(id, changes[i], "", 0)
	}
	ga, gb := a.Get(id).Todo, b.Get(id).Todo
	if len(ga) != 3 || len(gb) != 3 {
		t.Fatalf("len a=%d b=%d", len(ga), len(gb))
	}
	for i := range ga {
		if *ga[i] != *gb[i] {
			t.Fatalf("diverged at %d:\n %+v\n %+v", i, ga[i], gb[i])
		}
	}
	if m := entry(a, id, "e000000000000001"); m.Text != "全脂牛奶" || !m.Done {
		t.Fatalf("milk = %+v (want text edit and check both kept)", m)
	}
	if e := entry(a, id, "e000000000000002"); e.Del != 102 || e.Text != "" {
		t.Fatalf("eggs = %+v (want deleted)", e)
	}
	if ga[2].ID != "e000000000000003" {
		t.Fatal("order should follow creation time")
	}
	// 标题按时间取新；旧的标题不会覆盖
	a.applyTodo(id, nil, "周末购物", 50)
	if a.applyTodo(id, nil, "旧标题", 20) != nil || a.Get(id).Text != "周末购物" {
		t.Fatalf("title = %q", a.Get(id).Text)
	}
	if a.applyTodo(id, changes[0], "", 0) != nil {
		t.Fatal("re-applying should be a no-op")
	}
}

func TestTodoPersistPinnedAndText(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	s.setRetention(1) // 保留 1 天
	const id = "b000000000000001"
	old := nowMs() - 3*86400_000
	if !s.Add(&Item{ID: id, Kind: "todo", Text: "出门", From: "x", Origin: "o", TS: old, Todo: []*TodoEntry{
		{ID: "e000000000000002", Text: "钥匙", TextTS: 2, TS: 2, Done: true, DoneTS: 3},
		{ID: "e000000000000001", Text: "钱包", TextTS: 1, TS: 1},
		{ID: "e000000000000003", Text: "删掉的", TextTS: 1, TS: 3, Del: 5},
	}}) {
		t.Fatal("an old todo list should not count as expired")
	}
	s.Prune()
	s.Flush()
	r := openStore(dir)
	it := r.Get(id)
	if it == nil || len(it.Todo) != 3 || it.Todo[0].Text != "钱包" || it.Todo[2].Text != "" {
		t.Fatalf("after reload: %+v", it)
	}
	if got, want := it.PlainText(), "出门\n- [ ] 钱包\n- [x] 钥匙"; got != want {
		t.Fatalf("PlainText = %q, want %q", got, want)
	}
}

func TestParseTodoLine(t *testing.T) {
	cases := []struct {
		in   string
		text string
		done bool
	}{
		{"- [ ] 牛奶", "牛奶", false},
		{"- [x] 鸡蛋", "鸡蛋", true},
		{"* 面包", "面包", false},
		{"1. 苹果", "苹果", false},
		{"☑ 已买", "已买", true},
		{"  普通一行  ", "普通一行", false},
		{"2024. 不是序号", "2024. 不是序号", false},
	}
	for _, c := range cases {
		if text, done := parseTodoLine(c.in); text != c.text || done != c.done {
			t.Errorf("parseTodoLine(%q) = %q, %v", c.in, text, done)
		}
	}
}

func TestUpdateTodo(t *testing.T) {
	a := testApp(t, nil)
	it, err := a.NewTodo("购物", []string{"- [ ] 牛奶", "", "- [x] 鸡蛋"}, "Mac")
	if err != nil || len(it.Todo) != 2 || !it.Todo[1].Done {
		t.Fatalf("NewTodo: %v %+v", err, it)
	}
	milk := it.Todo[0].ID
	yes := true
	it, _ = a.UpdateTodo(it.ID, TodoChange{ID: milk, Done: &yes, By: "iPhone"})
	it, _ = a.UpdateTodo(it.ID, TodoChange{Add: []string{"面包", "  "}, By: "iPhone"})
	if len(it.Todo) != 3 || !it.Todo[0].Done || it.Todo[2].Text != "面包" || it.Todo[2].By != "iPhone" {
		t.Fatalf("after check/add: %+v", it.Todo)
	}
	it, _ = a.UpdateTodo(it.ID, TodoChange{Clear: true})
	if liveTodo(it.Todo) != 1 {
		t.Fatalf("clear done: live = %d", liveTodo(it.Todo))
	}
	if _, err := a.UpdateTodo(it.ID, TodoChange{ID: milk, Done: &yes}); err == nil {
		t.Fatal("changing a deleted entry should fail")
	}
	if _, err := a.NewTodo(" ", nil, "Mac"); err == nil {
		t.Fatal("empty list should fail")
	}
}

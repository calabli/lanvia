package core

import (
	"testing"
)

func addText(s *Store, id string) {
	s.Add(&Item{ID: id, Kind: "text", Text: "hello", From: "x", Origin: "o", TS: nowMs()})
}

func TestCommentsConverge(t *testing.T) {
	const id = "a000000000000001"
	c1 := &Comment{ID: "c000000000000001", Text: "第一条", By: "Mac", TS: 100}
	c2 := &Comment{ID: "c000000000000002", Text: "第二条", By: "iPhone", TS: 200}
	del1 := &Comment{ID: c1.ID, By: "Mac", TS: 100, Del: 300}
	del1b := &Comment{ID: c1.ID, By: "Mac", TS: 100, Del: 250} // 另一台设备更早删了同一条

	a, b := openStore(t.TempDir()), openStore(t.TempDir())
	addText(a, id)
	addText(b, id)
	// 两边以不同顺序收到同样的变化
	for _, c := range []*Comment{c1, c2, del1, del1b} {
		a.applyComments(id, []*Comment{c})
	}
	for _, c := range []*Comment{del1b, c2, del1, c1} {
		b.applyComments(id, []*Comment{c})
	}
	ga, gb := a.allComments(id), b.allComments(id)
	if len(ga) != 2 || len(gb) != 2 {
		t.Fatalf("len a=%d b=%d", len(ga), len(gb))
	}
	for i := range ga {
		if *ga[i] != *gb[i] {
			t.Fatalf("diverged at %d: %+v vs %+v", i, ga[i], gb[i])
		}
	}
	if ga[0].Del != 250 || ga[0].Text != "" {
		t.Fatalf("deleted comment: %+v", ga[0])
	}
	if live := a.Comments(id); len(live) != 1 || live[0].ID != c2.ID {
		t.Fatalf("live = %+v", live)
	}
	if n := a.Get(id).Comments; n != 1 {
		t.Fatalf("count = %d", n)
	}
	// 重复合并没有变化
	if a.applyComments(id, []*Comment{c1, c2}) != nil {
		t.Fatal("re-applying should be a no-op")
	}
	// 不认识的内容、无效评论被忽略
	if a.applyComments("ffffffffffffffff", []*Comment{c2}) != nil || a.applyComments(id, []*Comment{{ID: "bad", TS: 1}}) != nil {
		t.Fatal("invalid comment applied")
	}
}

func TestCommentsPersistAndSync(t *testing.T) {
	const id = "a000000000000001"
	dir := t.TempDir()
	s := openStore(dir)
	addText(s, id)
	s.applyComments(id, []*Comment{{ID: "c000000000000001", Text: "留着", By: "Mac", TS: 100}, {ID: "c000000000000002", Text: "删掉", By: "Mac", TS: 200, Del: 300}})
	since := nowMs() - 1
	snap := s.Snapshot(0)
	if len(snap.Comments[id]) != 2 {
		t.Fatalf("snapshot comments = %d", len(snap.Comments[id]))
	}
	s.Flush()

	r := openStore(dir)
	if n := r.Get(id).Comments; n != 1 || len(r.allComments(id)) != 2 {
		t.Fatalf("after reload: count=%d all=%d", n, len(r.allComments(id)))
	}
	// 新评论会让这条内容出现在增量同步里
	r.applyComments(id, []*Comment{{ID: "c000000000000003", Text: "新的", By: "PC", TS: 400}})
	if got := r.Snapshot(since); len(got.Items) != 1 || len(got.Comments[id]) != 3 {
		t.Fatalf("incremental: items=%d comments=%d", len(got.Items), len(got.Comments[id]))
	}
	// 内容删除后评论文件一起删除
	r.Delete(id)
	r.Flush()
	if q := openStore(dir); len(q.allComments(id)) != 0 {
		t.Fatal("comments survived item deletion")
	}
}

func TestCommentsCap(t *testing.T) {
	const id = "a000000000000001"
	s := openStore(t.TempDir())
	addText(s, id)
	var in []*Comment
	for i := 0; i < maxComments+20; i++ {
		c := &Comment{ID: randHex(8), Text: "x", By: "y", TS: int64(1000 + i)}
		if i == 150 {
			c.Del = 5000 // 一条墓碑：超出上限时最先被丢掉
		}
		in = append(in, c)
	}
	s.applyComments(id, in)
	all := s.allComments(id)
	if len(all) != maxComments {
		t.Fatalf("len = %d", len(all))
	}
	for _, c := range all {
		if c.Del > 0 {
			t.Fatal("tombstone should be dropped first")
		}
	}
	if all[0].TS != 1019 { // 丢了 1 条墓碑和最旧的 19 条（1000–1018）
		t.Fatalf("oldest kept TS = %d", all[0].TS)
	}
}

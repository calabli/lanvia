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
	del1b := &Comment{ID: c1.ID, By: "Mac", TS: 100, Del: 250} // another device deleted the same one earlier

	a, b := openStore(t.TempDir()), openStore(t.TempDir())
	addText(a, id)
	addText(b, id)
	// Both sides receive the same changes in a different order
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
	// Merging again changes nothing
	if a.applyComments(id, []*Comment{c1, c2}) != nil {
		t.Fatal("re-applying should be a no-op")
	}
	// Unknown items and invalid comments are ignored
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
	// A new comment makes this item show up in the incremental sync
	r.applyComments(id, []*Comment{{ID: "c000000000000003", Text: "新的", By: "PC", TS: 400}})
	if got := r.Snapshot(since); len(got.Items) != 1 || len(got.Comments[id]) != 3 {
		t.Fatalf("incremental: items=%d comments=%d", len(got.Items), len(got.Comments[id]))
	}
	// Deleting the item also deletes its comments file
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
			c.Del = 5000 // a tombstone: it is dropped first once the cap is exceeded
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
	if all[0].TS != 1019 { // one tombstone and the 19 oldest entries (1000–1018) were dropped
		t.Fatalf("oldest kept TS = %d", all[0].TS)
	}
}

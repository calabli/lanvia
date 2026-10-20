package core

import (
	"testing"
	"time"
)

func ids(f storeFile) map[string]bool {
	m := map[string]bool{}
	for _, it := range f.Items {
		m[it.ID] = true
	}
	return m
}

func TestIncrementalSnapshot(t *testing.T) {
	s := openStore(t.TempDir())
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: "a", From: "x", Origin: "o", TS: nowMs()})
	s.Add(&Item{ID: "b000000000000002", Kind: "text", Text: "b", From: "x", Origin: "o", TS: nowMs()})
	full := s.Snapshot(0)
	if len(full.Items) != 2 || full.Now == 0 {
		t.Fatalf("full snapshot: %d items, now=%d", len(full.Items), full.Now)
	}
	since := full.Now
	time.Sleep(5 * time.Millisecond)

	if got := s.Snapshot(since); len(got.Items) != 0 || len(got.Tomb) != 0 {
		t.Fatalf("nothing changed, but got %d items", len(got.Items))
	}
	// 收藏、编辑、新增、删除都算变化
	s.applyFav("a000000000000001", true, "名字", nowMs())
	s.applyVersions("b000000000000002", []*TextVersion{{ID: "c000000000000003", Text: "b2", By: "y", TS: nowMs()}}, true)
	// 一条“很旧”的内容（创建时间在两天前）刚从别的设备同步过来：对本机来说是新的，下次增量同步要带上
	s.Add(&Item{ID: "d000000000000004", Kind: "text", Text: "old but new here", From: "z", Origin: "o", TS: nowMs() - 2*86400_000})
	s.Delete("e000000000000005")

	got := s.Snapshot(since)
	want := map[string]bool{"a000000000000001": true, "b000000000000002": true, "d000000000000004": true}
	if g := ids(got); len(g) != 3 || !g["a000000000000001"] || !g["b000000000000002"] || !g["d000000000000004"] {
		t.Fatalf("changed items = %v, want %v", g, want)
	}
	if len(got.Versions["b000000000000002"]) != 2 {
		t.Fatal("versions of the edited item missing")
	}
	if _, ok := got.Tomb["e000000000000005"]; !ok {
		t.Fatal("new tombstone missing")
	}
}

// 最后变化时间要保存下来，重启后增量同步仍然正确
func TestSeenPersisted(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: "a", From: "x", Origin: "o", TS: nowMs()})
	s.Flush()
	since := nowMs() + 1
	time.Sleep(5 * time.Millisecond)
	r := openStore(dir)
	if got := r.Snapshot(since); len(got.Items) != 0 {
		t.Fatal("unchanged item reported as changed after restart")
	}
}

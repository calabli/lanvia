package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		return -1
	}
	return st.Size()
}

func TestStoreSplitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	big := strings.Repeat("长内容 log line\n", 8000) // 约 150 KB
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: "short", From: "x", Origin: "o", TS: nowMs() - 2000})
	s.Add(&Item{ID: "b000000000000002", Kind: "text", Text: big, From: "x", Origin: "o", TS: nowMs() - 1000})
	s.applyVersions("a000000000000001", []*TextVersion{{ID: "c000000000000003", Text: "short v2", By: "y", TS: nowMs()}}, true)
	s.Flush()

	if n := fileSize(t, filepath.Join(dir, "history.json")); n < 0 || n > 4096 {
		t.Fatalf("history.json should be a small index, size %d", n)
	}
	if fileSize(t, s.textPath("b000000000000002")) != int64(len(big)) {
		t.Fatal("long text not stored in its own file")
	}
	if fileSize(t, s.verPath("a000000000000001")) <= 0 {
		t.Fatal("versions not stored in their own file")
	}
	idx, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	if strings.Contains(string(idx), `"versions"`) {
		t.Fatal("index still contains versions")
	}

	r := openStore(dir)
	if got := r.Get("b000000000000002"); got == nil || got.Text != big || got.TextFile {
		t.Fatal("long text not restored")
	}
	if got := r.Get("a000000000000001"); got == nil || got.Text != "short v2" || got.Rev != 1 {
		t.Fatalf("edited item not restored: %+v", got)
	}
	if vs := r.Versions("a000000000000001"); len(vs) != 2 || vs[1].Text != "short" {
		t.Fatalf("versions not restored: %+v", vs)
	}
}

// 只重写改动过的内容的文件
func TestStoreWritesOnlyChanged(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	for _, id := range []string{"a000000000000001", "b000000000000002"} {
		s.Add(&Item{ID: id, Kind: "text", Text: "v0 " + id, From: "x", Origin: "o", TS: nowMs()})
		s.applyVersions(id, []*TextVersion{{ID: "f" + id[1:], Text: "v1 " + id, By: "y", TS: nowMs()}}, true)
	}
	s.Flush()
	before, _ := os.Stat(s.verPath("a000000000000001"))
	time.Sleep(20 * time.Millisecond)
	s.applyVersions("b000000000000002", []*TextVersion{{ID: "e000000000000009", Text: "v2", By: "y", TS: nowMs() + 1}}, true)
	s.Flush()
	after, _ := os.Stat(s.verPath("a000000000000001"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("unchanged item's version file was rewritten")
	}
	var vs []*TextVersion
	b, _ := os.ReadFile(s.verPath("b000000000000002"))
	if json.Unmarshal(b, &vs) != nil || len(vs) != 3 {
		t.Fatalf("changed item's versions not written: %d", len(vs))
	}
}

func TestStoreCleansFiles(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	big := strings.Repeat("x", previewBytes+10)
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: big, From: "x", Origin: "o", TS: nowMs() - 1000})
	s.applyVersions("a000000000000001", []*TextVersion{{ID: "c000000000000003", Text: big + "y", By: "y", TS: nowMs()}}, true)
	s.Add(&Item{ID: "b000000000000002", Kind: "text", Text: big, From: "x", Origin: "o", TS: nowMs()})
	s.Flush()
	// 编辑后变短：长正文文件删除
	s.applyVersions("b000000000000002", []*TextVersion{{ID: "d000000000000004", Text: "short now", By: "y", TS: nowMs() + 5}}, true)
	s.Flush()
	if fileSize(t, s.textPath("b000000000000002")) != -1 {
		t.Fatal("text file kept after the text became short")
	}
	// 删除：正文和版本文件一起删
	s.Delete("a000000000000001")
	if fileSize(t, s.textPath("a000000000000001")) != -1 || fileSize(t, s.verPath("a000000000000001")) != -1 {
		t.Fatal("files kept after delete")
	}
	if r := openStore(dir); r.Get("b000000000000002").Text != "short now" {
		t.Fatal("shortened text not restored")
	}
}

// 旧版本把版本和长正文都写在 history.json 里，打开后应迁移到新格式且不丢数据。
func TestStoreMigratesOldFormat(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("旧格式长正文\n", 5000)
	now := nowMs()
	old := storeFile{
		Items: []*Item{
			{ID: "a000000000000001", Kind: "text", Text: "v1", From: "x", Origin: "o", TS: now - 1000, Rev: 1, EditedAt: now, EditedBy: "y"},
			{ID: "b000000000000002", Kind: "text", Text: big, From: "x", Origin: "o", TS: now},
		},
		Tomb:     map[string]int64{"dead000000000000": now},
		Versions: map[string][]*TextVersion{"a000000000000001": {{ID: "a000000000000001", Text: "v0", By: "x", TS: now - 1000}, {ID: "c000000000000003", Text: "v1", By: "y", TS: now}}},
	}
	b, _ := json.Marshal(old)
	os.WriteFile(filepath.Join(dir, "history.json"), b, 0o600)

	s := openStore(dir)
	s.Flush()
	idx, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	if strings.Contains(string(idx), `"versions"`) || len(idx) > 4096 {
		t.Fatalf("not migrated, index size %d", len(idx))
	}
	r := openStore(dir)
	if r.Get("b000000000000002").Text != big || len(r.Versions("a000000000000001")) != 2 {
		t.Fatal("data lost during migration")
	}
	if _, dead := r.tomb["dead000000000000"]; !dead {
		t.Fatal("tombstones lost during migration")
	}
}

// 索引（byID）在各种增删改之后始终与列表一致，indexOf 能找到每一条
func TestStoreIndexConsistent(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	check := func(step string) {
		t.Helper()
		if len(s.byID) != len(s.items) {
			t.Fatalf("%s: byID %d, items %d", step, len(s.byID), len(s.items))
		}
		for i, it := range s.items {
			if s.byID[it.ID] != it || s.indexOf(it.ID) != i {
				t.Fatalf("%s: item %s at %d not indexed", step, it.ID, i)
			}
		}
	}
	now := nowMs()
	for i := 0; i < maxItems+50; i++ {
		ts := now - int64(i%40)*1000 // 很多条时间相同，考验二分查找后的线性部分
		s.Add(&Item{ID: fmt.Sprintf("%016x", i+1), Kind: "text", Text: "x", From: "x", Origin: "o", TS: ts, Keep: i%7 == 0})
	}
	check("add over cap")
	s.applyFav(s.items[10].ID, true, "t", now)
	s.applyMeta(s.items[20].ID, 0, true, now)
	s.applyTags(s.items[30].ID, map[string]int64{"a": now})
	check("apply")
	s.Delete(s.items[5].ID)
	s.Delete("ffffffffffffffff")
	check("delete")
	s.Clear(now - 20_000)
	check("clear")
	s.Flush()
	s = openStore(dir)
	check("reload")
}

package core

import (
	"strings"
	"testing"
)

func TestSearchFull(t *testing.T) {
	a := testApp(t, nil)
	long := strings.Repeat("filler line\n", 3000) + "第 3001 行：Deploy KEY = abc123\n" + strings.Repeat("tail\n", 10)
	a.store.Add(&Item{ID: "a000000000000001", Kind: "text", Text: long, From: "x", Origin: "o", TS: nowMs()})
	a.store.Add(&Item{ID: "b000000000000002", Kind: "text", Text: "short deploy key", From: "x", Origin: "o", TS: nowMs()})

	hits := a.SearchFull("deploy   key")
	if len(hits) != 1 || hits[0].ID != "a000000000000001" { // 短内容由网页自己搜，不在结果里
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Line != 3001 || !strings.Contains(hits[0].Snippet, "Deploy KEY = abc123") {
		t.Fatalf("line/snippet wrong: %+v", hits[0])
	}
	// 多个关键词分散在不同位置：定位到最晚出现的那个（摘要之外的）
	if h := a.SearchFull("filler abc123"); len(h) != 1 || h[0].Line != 3001 {
		t.Fatalf("should point at the later term: %+v", h)
	}
	if len(a.SearchFull("deploy nothere")) != 0 { // 所有关键词都要匹配
		t.Fatal("matched with a missing term")
	}
	if len(a.SearchFull("   ")) != 0 {
		t.Fatal("empty query matched")
	}
}

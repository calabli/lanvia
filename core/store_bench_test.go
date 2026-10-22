package core

import (
	"fmt"
	"testing"
)

// 内容很多时（收藏、清单、永久保留的不受 300 条上限限制）按 ID 查找、修改的耗时
func benchStore(b *testing.B, n int) (*Store, []string) {
	s := openStore(b.TempDir())
	ids := make([]string, n)
	now := nowMs()
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("%016x", i+1)
		s.Add(&Item{ID: ids[i], Kind: "text", Text: "x", From: "x", Origin: "o", TS: now - int64(n-i), Keep: true})
	}
	return s, ids
}

func BenchmarkStoreGet(b *testing.B) {
	s, ids := benchStore(b, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Get(ids[i%len(ids)])
	}
}

func BenchmarkStoreApplyFav(b *testing.B) {
	s, ids := benchStore(b, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.applyFav(ids[i%len(ids)], i%2 == 0, "", int64(i+1))
	}
}

func BenchmarkStoreAdd3000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchStore(b, 3000)
	}
}

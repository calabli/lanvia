package core

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// When concurrent downloads ask for space at the same time, the total must not exceed the limit (they used to check separately and then download, which went over together).
func TestReserveConcurrent(t *testing.T) {
	a := testApp(t, func(c *Config) { c.StorageMB = 1 })
	const size = 400 << 10
	var ok atomic.Int32
	var mu sync.Mutex
	var releases []func()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rel, good := a.reserve(size); good {
				ok.Add(1)
				mu.Lock()
				releases = append(releases, rel)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got := ok.Load(); got != 2 {
		t.Fatalf("granted %d reservations of 400 KB under a 1 MB limit, want 2", got)
	}
	if _, good := a.reserve(size); good {
		t.Fatal("third reservation granted while two are held")
	}
	releases[0]()
	releases[0]() // a repeated release must not free twice
	if _, good := a.reserve(size); !good {
		t.Fatal("reservation refused after one was released")
	}
	if _, good := a.reserve(size); good {
		t.Fatal("double release freed too much space")
	}
}

// When a copy already exists, reserving space first cleans up the old copy and does not exceed the limit.
func TestReserveEvictsReplicas(t *testing.T) {
	a := testApp(t, func(c *Config) { c.StorageMB = 1 })
	const size = 400 << 10
	for i, id := range []string{"aaaa000000000001", "aaaa000000000002"} {
		a.store.Add(&Item{ID: id, Kind: "file", Name: id, Size: size, From: "PC", Origin: "other-node", TS: nowMs() - int64(10-i)*1000})
		if err := os.WriteFile(a.store.blobPath(id), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rel, good := a.reserve(size)
	if !good {
		t.Fatal("reservation refused although an old copy could be evicted")
	}
	defer rel()
	u := a.StorageUsage()
	if u.Count != 1 || a.store.hasBlob("aaaa000000000001") {
		t.Fatalf("expected the oldest copy to be evicted, usage %+v", u)
	}
	if used := u.Own + u.Replicas + size; used > 1<<20 {
		t.Fatalf("over quota: %d", used)
	}
}

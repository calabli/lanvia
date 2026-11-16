package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRemoveMany(t *testing.T) {
	a := testApp(t, nil)
	defer a.store.Flush()
	var mu sync.Mutex
	var got []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env Envelope
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &env)
		mu.Lock()
		got = append(got, env.Op+":"+env.ID)
		mu.Unlock()
		writeJSON(w, 200, ok)
	}))
	defer peer.Close()

	one := a.newText("one", "MyMac", false)
	two := a.newText("two", "MyMac", false)
	keep := a.newText("keep", "MyMac", false)
	time.Sleep(50 * time.Millisecond) // delivering new items is asynchronous, wait for them to finish before attaching the peer
	a.peers.m["p"] = &Peer{ID: "p", Name: "PC", Addr: strings.TrimPrefix(peer.URL, "http://")}

	// Duplicate and malformed IDs do not count; an ID this machine does not have writes a tombstone and is forwarded just like a single delete (the other side may have it)
	unknown := "ffffffffffffffff"
	if n := a.removeMany([]string{one.ID, two.ID, one.ID, unknown, "../x"}); n != 3 {
		t.Fatalf("removed %d, want 3", n)
	}
	if l := a.store.List(); len(l) != 1 || l[0].ID != keep.ID {
		t.Fatalf("left %d items", len(l))
	}
	if n := a.removeMany([]string{one.ID}); n != 0 {
		t.Fatal("deleting twice should be a no-op")
	}
	want := []string{"delete:" + one.ID, "delete:" + two.ID, "delete:" + unknown}
	for i := 0; i < 100; i++ {
		mu.Lock()
		done := len(got) >= len(want)
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("relayed %v, want %v in order", got, want)
	}
}

func TestBatchDeleteRoute(t *testing.T) {
	a := testApp(t, nil)
	defer a.store.Flush()
	it := a.newText("x", "MyMac", false)
	h := a.routes(nil)
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost/api/items/delete", bytes.NewReader([]byte(body)))
		r.Host = "localhost"
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Origin", "http://localhost")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post(`{"ids":["` + it.ID + `"]}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	big, _ := json.Marshal(map[string][]string{"ids": make([]string, maxBatchDelete+1)})
	if w := post(string(big)); w.Code != 400 {
		t.Fatalf("oversized batch: %d", w.Code)
	}
}

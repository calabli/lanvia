package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitDelivery(t *testing.T, a *App, id string) Delivery {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range a.deliverySnapshot() {
			if d.ID != id {
				continue
			}
			pending := false
			for _, target := range d.Targets {
				pending = pending || target.State == "pending"
			}
			if !pending {
				return d
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("receipt did not settle")
	return Delivery{}
}

func TestDeliveryPartialFailureAndRetry(t *testing.T) {
	a := testApp(t, nil)
	defer a.store.Flush()
	var goodCalls, badCalls atomic.Int32
	var repaired atomic.Bool
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { goodCalls.Add(1); writeJSON(w, 200, ok) }))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		if repaired.Load() {
			writeJSON(w, 200, ok)
		} else {
			errJSON(w, 503, "offline")
		}
	}))
	defer bad.Close()
	a.peers.m["good"] = &Peer{ID: "good", Name: "Mac", Addr: strings.TrimPrefix(good.URL, "http://")}
	a.peers.m["bad"] = &Peer{ID: "bad", Name: "PC", Addr: strings.TrimPrefix(bad.URL, "http://")}
	it := a.newText("one copy only", "MyMac", false)
	d := waitDelivery(t, a, it.ID)
	if d.Targets[0].State != "received" || d.Targets[1].State != "failed" {
		t.Fatalf("partial result: %+v", d)
	}
	repaired.Store(true)
	r := httptest.NewRequest("POST", "/", nil)
	r.SetPathValue("id", it.ID)
	w := httptest.NewRecorder()
	a.handleDeliveryRetry(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	d = waitDelivery(t, a, it.ID)
	for _, target := range d.Targets {
		if target.State != "received" {
			t.Fatalf("retry: %+v", d)
		}
	}
	if goodCalls.Load() != 1 || badCalls.Load() != 2 || len(a.store.List()) != 1 {
		t.Fatalf("retry duplicated successful delivery or content: %d/%d", goodCalls.Load(), badCalls.Load())
	}
}

func TestDeliveryBrowserNeedsExplicitReceipt(t *testing.T) {
	a := testApp(t, nil)
	defer a.store.Flush()
	c := &Client{ClientInfo: ClientInfo{ID: "phone", Name: "iPhone"}, ch: make(chan []byte, 256)}
	a.hub.Add(c)
	defer a.hub.Remove(c)
	it := a.newText("receipt", "MyMac", false)
	d := a.deliverySnapshot()[0]
	if d.Targets[0].State != "pending" {
		t.Fatalf("enqueue is not a receipt: %+v", d)
	}
	ack := func(client string) int {
		b, _ := json.Marshal(map[string]string{"id": it.ID, "client": client})
		w := httptest.NewRecorder()
		a.handleDeliveryAck(w, httptest.NewRequest("POST", "/", bytes.NewReader(b)))
		return w.Code
	}
	if ack("unknown") != 400 || ack("phone") != 200 {
		t.Fatal("incorrect receipt validation")
	}
	d = waitDelivery(t, a, it.ID)
	if d.Targets[0].State != "received" {
		t.Fatal(d)
	}
	a.delivery.Lock()
	current := a.delivery.items[it.ID]
	a.delivery.Unlock()
	a.deliveryResult(current, "web:phone", "failed")
	if waitDelivery(t, a, it.ID).Targets[0].State != "received" {
		t.Fatal("late timeout overwrote receipt")
	}
}

func TestDeliveryNoTargetsCanRetryAfterDeviceConnects(t *testing.T) {
	a := testApp(t, nil)
	defer a.store.Flush()
	it := a.newText("offline", "MyMac", false)
	if len(waitDelivery(t, a, it.ID).Targets) != 0 {
		t.Fatal("expected local-only save")
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, ok) }))
	defer s.Close()
	a.peers.m["pc"] = &Peer{ID: "pc", Name: "PC", Addr: strings.TrimPrefix(s.URL, "http://")}
	r := httptest.NewRequest("POST", "/", nil)
	r.SetPathValue("id", it.ID)
	a.handleDeliveryRetry(httptest.NewRecorder(), r)
	d := waitDelivery(t, a, it.ID)
	if len(d.Targets) != 1 || d.Targets[0].State != "received" {
		t.Fatal(d)
	}
}

func TestDeliveryStrictEncryptionAndGeneration(t *testing.T) {
	a := testApp(t, func(c *Config) { c.StrictEnc = true })
	defer a.store.Flush()
	a.peers.m["old"] = &Peer{ID: "old", Name: "Old PC", Addr: "127.0.0.1:1"}
	it := a.newText("secure", "MyMac", false)
	if waitDelivery(t, a, it.ID).Targets[0].State != "blocked" {
		t.Fatal("plaintext target was not blocked")
	}
	a.delivery.Lock()
	old := a.delivery.items[it.ID]
	a.delivery.Unlock()
	r := httptest.NewRequest("POST", "/", nil)
	r.SetPathValue("id", it.ID)
	a.handleDeliveryRetry(httptest.NewRecorder(), r)
	a.deliveryResult(old, "peer:old", "received")
	if waitDelivery(t, a, it.ID).Targets[0].State != "blocked" {
		t.Fatal("old attempt changed current generation")
	}
}

package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Receipts describe item/metadata acceptance, never clipboard application or blob download.
type DeliveryTarget struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"` // pending | received | failed | blocked
}
type Delivery struct {
	ID      string           `json:"id"`
	Kind    string           `json:"kind"`
	TS      int64            `json:"ts"`
	Targets []DeliveryTarget `json:"targets"`
}
type deliveryLog struct {
	sync.Mutex
	items map[string]*Delivery
}

func (a *App) deliverySnapshot() []Delivery {
	a.delivery.Lock()
	defer a.delivery.Unlock()
	out := []Delivery{}
	for _, d := range a.delivery.items {
		out = append(out, cloneDelivery(d))
	}
	return out
}
func cloneDelivery(d *Delivery) Delivery {
	c := *d
	c.Targets = append([]DeliveryTarget{}, d.Targets...)
	return c
}

// A retry uses a fresh generation and contacts only targets without receipts.
func (a *App) deliverItem(it *Item, body []byte, sender string, previous *Delivery) {
	d := &Delivery{ID: it.ID, Kind: it.Kind, TS: nowMs(), Targets: []DeliveryTarget{}}
	peers := a.peers.List()
	clients := a.hub.List()
	if previous != nil && len(previous.Targets) > 0 {
		d.Targets = append(d.Targets, previous.Targets...)
		for i := range d.Targets {
			if d.Targets[i].State != "received" {
				d.Targets[i].State = "pending"
			}
		}
	} else {
		for _, p := range peers {
			d.Targets = append(d.Targets, DeliveryTarget{ID: "peer:" + p.ID, Name: p.Name, State: "pending"})
		}
		for _, c := range clients {
			if !c.Local && c.ID != sender {
				d.Targets = append(d.Targets, DeliveryTarget{ID: "web:" + c.ID, Name: c.Name, State: "pending"})
			}
		}
	}
	a.delivery.Lock()
	if a.delivery.items == nil {
		a.delivery.items = map[string]*Delivery{}
	}
	// Keep a bounded recent history, independent of stored content.
	if len(a.delivery.items) >= 128 && a.delivery.items[it.ID] == nil {
		oldID := ""
		var oldest int64
		for id, v := range a.delivery.items {
			if oldID == "" || v.TS < oldest {
				oldID, oldest = id, v.TS
			}
		}
		delete(a.delivery.items, oldID)
	}
	a.delivery.items[it.ID] = d
	a.hub.Broadcast("delivery", cloneDelivery(d))
	// Take a copy before asynchronous receipts can mutate targets.
	targets := append([]DeliveryTarget{}, d.Targets...)
	a.delivery.Unlock()
	for _, target := range targets {
		if target.State == "received" {
			continue
		}
		var peer *Peer
		for _, p := range peers {
			if target.ID == "peer:"+p.ID {
				peer = p
				break
			}
		}
		if peer != nil {
			if a.cfgSnapshot().StrictEnc && !peer.Enc {
				a.deliveryResult(d, target.ID, "blocked")
				continue
			}
			go func(p *Peer, id string) {
				b := body
				if p.Enc {
					b = sealAll(body, a.creds().enc)
				}
				req := a.peers.newReq(context.Background(), "POST", p.Addr, "/api/relay", b)
				if p.Enc {
					req.Header.Set("X-Lanvia-Body-Enc", "1")
					req.Header.Set("Content-Type", "application/octet-stream")
				}
				state := "failed"
				if resp, err := a.peers.do(a.peers.hc, req); err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if err == nil && resp.StatusCode == http.StatusOK {
						state = "received"
					}
				}
				a.deliveryResult(d, id, state)
			}(peer, target.ID)
			continue
		}
		found := false
		for _, c := range clients {
			if target.ID == "web:"+c.ID && !c.Local {
				found = true
				break
			}
		}
		if !found {
			a.deliveryResult(d, target.ID, "failed")
			continue
		}
		// Explicit browser receipt: enqueueing an SSE frame alone is not success.
		time.AfterFunc(10*time.Second, func() { a.deliveryResult(d, target.ID, "failed") })
	}
	if previous != nil {
		a.bcastItem("item", it)
	}
	// Browsers acknowledge this event, including on retry (duplicate item is harmless).
	a.hub.Broadcast("delivery-request", map[string]string{"id": it.ID})
}

func (a *App) deliveryResult(d *Delivery, target, state string) {
	a.delivery.Lock()
	defer a.delivery.Unlock()
	if a.delivery.items[d.ID] != d {
		return
	} // Ignore a superseded attempt.
	for i := range d.Targets {
		t := &d.Targets[i]
		if t.ID == target && (t.State == "pending" || state == "received") && t.State != state {
			t.State = state
			a.hub.Broadcast("delivery", cloneDelivery(d))
			return
		}
	}
}

func (a *App) handleDeliveryAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string
		Client string
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil {
		errJSON(w, 400, "bad receipt")
		return
	}
	// Only an active remote browser can acknowledge its own target.
	active := false
	for _, c := range a.hub.List() {
		if c.ID == req.Client && !c.Local {
			active = true
			break
		}
	}
	if !active {
		errJSON(w, 400, "inactive client")
		return
	}
	a.delivery.Lock()
	d := a.delivery.items[req.ID]
	a.delivery.Unlock()
	if d != nil {
		a.deliveryResult(d, "web:"+req.Client, "received")
	}
	writeJSON(w, 200, ok)
}
func (a *App) handleDeliveryRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	it := a.store.Get(id)
	if it == nil {
		errJSON(w, 404, "内容不存在")
		return
	}
	a.delivery.Lock()
	var prev *Delivery
	if d := a.delivery.items[id]; d != nil {
		for _, t := range d.Targets {
			if t.State == "pending" {
				a.delivery.Unlock()
				errJSON(w, 409, "仍在等待接收确认")
				return
			}
		}
		c := cloneDelivery(d)
		prev = &c
	}
	a.delivery.Unlock()
	env := Envelope{Op: "add", EID: randHex(8), Item: it}
	a.eids.first(env.EID)
	body, _ := json.Marshal(env)
	a.deliverItem(it, body, r.Header.Get("X-Lanvia-Client"), prev)
	writeJSON(w, 200, ok)
}

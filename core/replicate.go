package core

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

// File content initially lives only on the sender. So that it is still available after the sender shuts down:
//   - When downloading, ask the sender and then the other computers in turn, taking it from whoever has it (fetchFromPeers);
//   - Each computer downloads a copy of the files sent by other devices in the background (up to the "auto fetch" size limit),
//     and whatever could not be fetched at the time is retried periodically (fetcher).

var errNoSource = errors.New("发送方和其他设备都不在线，或都没有这个文件")

// Client used to transfer files between nodes: give up quickly on devices that cannot be reached, but the transfer itself is not time limited (large files).
var blobClient = &http.Client{Transport: &http.Transport{
	DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
	ResponseHeaderTimeout: 15 * time.Second,
}}

// fetchFromPeers asks the sender first, then the other computers, and returns the first response that has the file.
func (a *App) fetchFromPeers(ctx context.Context, it *Item, rangeHdr string) (*http.Response, error) {
	peers := a.peers.List()
	sort.SliceStable(peers, func(i, j int) bool { return peers[i].ID == it.Origin && peers[j].ID != it.Origin })
	for _, p := range peers {
		req := a.peers.newReq(ctx, "GET", p.Addr, "/api/blob/"+it.ID, nil)
		req.Header.Set("X-Lanvia-Hop", "1") // the peer only returns what it has locally, without asking anyone else
		if rangeHdr != "" {
			req.Header.Set("Range", rangeHdr)
		}
		resp, err := a.peers.do(blobClient, req)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			return resp, nil
		}
		resp.Body.Close()
	}
	return nil, errNoSource
}

// ---- background auto fetch ----

type fetcher struct {
	mu      sync.Mutex
	queued  map[string]bool
	lastTry map[string]time.Time
	q       chan string
}

func newFetcher() *fetcher {
	return &fetcher{queued: map[string]bool{}, lastTry: map[string]time.Time{}, q: make(chan string, 512)}
}

// The background only fetches the most recent files; older ones are taken from other devices on demand (this also avoids re-downloading copies that were cleaned up after a restart).
const autoFetchWindow = 3 * 24 * time.Hour

// wantFetch reports whether this file needs to be downloaded in the background.
func (a *App) wantFetch(it *Item) bool {
	if it == nil || it.Kind != "file" || a.store.hasBlob(it.ID) || it.TS < nowMs()-autoFetchWindow.Milliseconds() {
		return false
	}
	switch limit := a.cfgSnapshot().AutoFetchMB; {
	case limit == 0:
		return false
	case limit > 0 && it.Size > int64(limit)<<20:
		return false
	}
	return true
}

func (a *App) enqueueFetch(id string) {
	f := a.fetch
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.queued[id] || time.Since(f.lastTry[id]) < 2*time.Minute {
		return
	}
	select {
	case f.q <- id:
		f.queued[id] = true
	default: // the queue is full, try again in the next round
	}
}

// fetchMissing queues every file that should be fetched but has not been yet (called when a new device comes online and on periodic checks).
func (a *App) fetchMissing() {
	for _, it := range a.store.List() {
		if a.wantFetch(it) {
			a.enqueueFetch(it.ID)
		}
	}
}

func (a *App) fetchLoop() {
	for id := range a.fetch.q {
		a.fetchOne(id)
		a.fetch.mu.Lock()
		delete(a.fetch.queued, id)
		a.fetch.lastTry[id] = time.Now()
		for k, t := range a.fetch.lastTry { // do not let the records grow without bound
			if time.Since(t) > time.Hour {
				delete(a.fetch.lastTry, k)
			}
		}
		a.fetch.mu.Unlock()
	}
}

func (a *App) fetchOne(id string) {
	it := a.store.Get(id)
	if !a.wantFetch(it) {
		return
	}
	release, ok := a.reserve(it.Size) // if it does not fit, do not back it up; fetch it again when needed
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	resp, err := a.fetchFromPeers(ctx, it, "")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	part := a.store.blobPath(id) + "." + randHex(4) + ".part"
	f, err := os.Create(part)
	if err != nil {
		return
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil || n != it.Size || a.store.Get(id) == nil { // it was deleted while downloading, so do not keep it
		os.Remove(part)
		return
	}
	if err := os.Rename(part, a.store.blobPath(id)); err == nil {
		log.Printf(L("已备份文件到本机: %s", "Backed up file: %s"), it.Name)
	}
}

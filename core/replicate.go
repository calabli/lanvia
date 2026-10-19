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

// 文件内容最初只在发送方。为了发送方关机后还能拿到：
//   - 下载时依次向发送方和其他电脑请求，谁有就从谁那里取（fetchFromPeers）；
//   - 每台电脑在后台把其他设备发来的文件下载一份（不超过“自动下载”的大小上限），
//     当时没取到的会定期重试（fetcher）。

var errNoSource = errors.New("发送方和其他设备都不在线，或都没有这个文件")

// 节点间传文件用的客户端：连不上的设备尽快放弃，但传输本身不限时（大文件）。
var blobClient = &http.Client{Transport: &http.Transport{
	DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
	ResponseHeaderTimeout: 15 * time.Second,
}}

// fetchFromPeers 先问发送方，再问其他电脑，返回第一个有这个文件的响应。
func (a *App) fetchFromPeers(ctx context.Context, it *Item, rangeHdr string) (*http.Response, error) {
	peers := a.peers.List()
	sort.SliceStable(peers, func(i, j int) bool { return peers[i].ID == it.Origin && peers[j].ID != it.Origin })
	for _, p := range peers {
		req := a.peers.newReq(ctx, "GET", p.Addr, "/api/blob/"+it.ID, nil)
		req.Header.Set("X-Lanvia-Hop", "1") // 对方只返回自己本地有的，不再转问别人
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

// ---- 后台自动下载 ----

type fetcher struct {
	mu      sync.Mutex
	queued  map[string]bool
	lastTry map[string]time.Time
	q       chan string
}

func newFetcher() *fetcher {
	return &fetcher{queued: map[string]bool{}, lastTry: map[string]time.Time{}, q: make(chan string, 512)}
}

// 后台只补最近几天的文件；更早的需要时再从其他设备取（也避免清理掉的副本重启后又被下载回来）。
const autoFetchWindow = 3 * 24 * time.Hour

// wantFetch 判断这个文件是否需要在后台下载到本机。
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
	default: // 队列满了，下一轮再试
	}
}

// fetchMissing 把所有该下载但还没下载的文件放进队列（新设备上线、定期检查时调用）。
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
		for k, t := range a.fetch.lastTry { // 别让记录无限增长
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
	quotaMu.Lock()
	fits := a.makeRoom(it.Size) // 放不下就不备份，需要时再去取
	quotaMu.Unlock()
	if !fits {
		return
	}
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
	if err != nil || n != it.Size || a.store.Get(id) == nil { // 下载期间被删除了就不留
		os.Remove(part)
		return
	}
	if err := os.Rename(part, a.store.blobPath(id)); err == nil {
		log.Printf("已备份文件到本机: %s", it.Name)
	}
}

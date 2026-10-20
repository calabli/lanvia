package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Client 是一个通过 SSE 连接到本节点的浏览器（手机、平板或电脑）。
type Client struct {
	ClientInfo
	ch chan []byte
}

type ClientInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"` // ios | ipad | android | mac | windows | linux | web
	Since int64  `json:"since"`
	Local bool   `json:"local,omitempty"` // 本机仪表盘
}

type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
}

func newHub() *Hub { return &Hub{clients: map[*Client]struct{}{}} }

func (h *Hub) Add(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) List() []ClientInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ClientInfo, 0, len(h.clients))
	for c := range h.clients {
		out = append(out, c.ClientInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since < out[j].Since })
	return out
}

func sseFrame(event string, v any) []byte {
	b, _ := json.Marshal(v)
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
}

func (h *Hub) Broadcast(event string, v any) {
	msg := sseFrame(event, v)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default: // 慢客户端丢消息；重连时会拿到完整快照
		}
	}
}

func nowMs() int64 { return time.Now().UnixMilli() }

// bcastItem 把新增 / 更新的内容推给网页（长正文截断）。
func (a *App) bcastItem(event string, it *Item) { a.hub.Broadcast(event, it.forClient()) }

func clientItems(items []*Item) []*Item {
	out := make([]*Item, len(items))
	for i, it := range items {
		out[i] = it.forClient()
	}
	return out
}

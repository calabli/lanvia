package core

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"
)

// 向前兼容：让以后的新功能经过这个版本的设备时不被丢掉。
//
// 以前每加一个功能（评论、清单、标签），内容经过旧版本设备转发时，旧版本不认识的字段都会丢。
// 从这个版本起：
//   - 内容（Item）里不认识的字段原样保存在 Extra 里，存盘、转发、同步历史时都带着；
//   - 不认识的同步操作（Envelope.Op）带有编号（EID）时原样转发给其他设备，按编号去重防止绕圈。
//
// 所以以后的新功能，只要把每条内容的状态放在 Item 的新字段里，或者用带 EID 的新操作同步，
// 中间隔着这个版本的设备也能传过去。

const (
	maxExtraBytes = 16 << 10 // 每条内容保留的未知字段最多这么大
	maxExtraKeys  = 32
)

// itemKnown 是 Item 自己的 JSON 字段名。
var itemKnown = func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeOf(Item{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			m[name] = true
		}
	}
	return m
}()

type itemNoMethods Item // 去掉 MarshalJSON / UnmarshalJSON，避免递归

func (it *Item) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*itemNoMethods)(it)); err != nil {
		return err
	}
	it.Extra = nil
	var all map[string]json.RawMessage
	if json.Unmarshal(b, &all) != nil {
		return nil
	}
	size := 0
	for k, v := range all {
		if itemKnown[k] {
			continue
		}
		size += len(k) + len(v)
		if size > maxExtraBytes || len(it.Extra) >= maxExtraKeys {
			it.Extra = nil // 太大的不保留，免得被塞满
			return nil
		}
		if it.Extra == nil {
			it.Extra = map[string]json.RawMessage{}
		}
		it.Extra[k] = v
	}
	return nil
}

func (it Item) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(itemNoMethods(it))
	if err != nil || len(it.Extra) == 0 {
		return b, err
	}
	extra, err := json.Marshal(it.Extra)
	if err != nil || len(extra) <= 2 {
		return b, nil
	}
	// {"id":...} + {"未知字段":...} → {"id":...,"未知字段":...}
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1])
	buf.WriteByte(',')
	buf.Write(extra[1:])
	return buf.Bytes(), nil
}

// adoptExtra 本机这条内容没有未知字段、对方的有时，补上（例如经过旧版本转发时丢了，又从别处同步回来）。
func (s *Store) adoptExtra(id string, extra map[string]json.RawMessage) *Item {
	if len(extra) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(id)
	if i < 0 || len(s.items[i].Extra) > 0 {
		return nil
	}
	n := *s.items[i]
	n.Extra = extra
	s.setAt(i, &n)
	s.touch(id)
	s.markDirty()
	return &n
}

// eidCache 记住最近转发过的同步消息编号，同一条消息绕回来时不再转发。
type eidCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func (c *eidCache) first(eid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]time.Time{}
	}
	now := time.Now()
	if len(c.seen) > 10000 {
		for k, t := range c.seen {
			if now.Sub(t) > 10*time.Minute {
				delete(c.seen, k)
			}
		}
	}
	if _, dup := c.seen[eid]; dup {
		return false
	}
	c.seen[eid] = now
	return true
}

// knownOps 是这个版本认识的同步操作；其余的带编号时原样转发。
var knownOps = map[string]bool{"add": true, "delete": true, "clear": true, "edit": true, "meta": true, "fav": true, "tags": true, "todo": true, "comment": true, "sync": true}

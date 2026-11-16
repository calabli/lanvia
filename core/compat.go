package core

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Forward compatibility: so that future features are not dropped when passing through devices running this version.
//
// Previously, every new feature (comments, lists, tags) lost all fields the old version did not know about when content was relayed by an older device.
// From this version on:
//   - Fields in the content (Item) that are not recognized are kept verbatim in Extra, and are carried along when storing, relaying and syncing history;
//   - Sync operations that are not recognized (Envelope.Op) are relayed verbatim to other devices when they carry a number (EID), deduplicated by that number to prevent loops.
//
// So future features only need to keep the state of each entry in a new field of Item, or to sync it with a new operation carrying an EID,
// and they will pass through devices running this version.

const (
	maxExtraBytes = 16 << 10 // unknown fields kept per entry are at most this large
	maxExtraKeys  = 32
)

// itemKnown holds the JSON field names of Item itself.
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

type itemNoMethods Item // drop MarshalJSON / UnmarshalJSON to avoid recursion

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
			it.Extra = nil // do not keep the oversized ones, so that they cannot fill it up
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
	// {"id":...} + {"unknown field":...} → {"id":...,"unknown field":...}
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1])
	buf.WriteByte(',')
	buf.Write(extra[1:])
	return buf.Bytes(), nil
}

// adoptExtra fills in the unknown fields when this machine's copy of the entry has none but the peer's copy does (for example they were dropped while relayed by an old version and were then synced back from elsewhere).
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

// eidCache remembers the IDs of recently relayed sync messages, so that a message coming back around is not relayed again.
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

// knownOps are the sync operations this version recognizes; the rest are relayed verbatim when they carry an ID.
var knownOps = map[string]bool{"add": true, "delete": true, "clear": true, "edit": true, "meta": true, "fav": true, "tags": true, "todo": true, "comment": true, "sync": true}

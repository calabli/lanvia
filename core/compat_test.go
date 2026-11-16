package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fields added by later versions: they are kept while parsing and are still there after saving, restarting, taking a sync snapshot, or pushing to the web page
func TestItemKeepsUnknownFields(t *testing.T) {
	in := `{"id":"a000000000000001","kind":"text","text":"hi","from":"x","origin":"o","ts":5,"reactions":{"👍":["Mac"]},"pinnedAt":123}`
	var it Item
	if err := json.Unmarshal([]byte(in), &it); err != nil {
		t.Fatal(err)
	}
	if it.Text != "hi" || len(it.Extra) != 2 || string(it.Extra["pinnedAt"]) != "123" {
		t.Fatalf("parsed: %+v extra=%v", it, it.Extra)
	}
	out, _ := json.Marshal(&it)
	var back map[string]any
	json.Unmarshal(out, &back)
	if back["text"] != "hi" || back["pinnedAt"] != float64(123) || back["reactions"] == nil {
		t.Fatalf("marshal lost fields: %s", out)
	}

	dir := t.TempDir()
	s := openStore(dir)
	it.TS = nowMs()
	s.Add(&it)
	s.Flush()
	r := openStore(dir)
	got := r.Get(it.ID)
	if got == nil || string(got.Extra["pinnedAt"]) != "123" {
		t.Fatalf("after reload: %+v", got)
	}
	snap, _ := json.Marshal(r.Snapshot(0))
	if !strings.Contains(string(snap), `"reactions":{"👍":["Mac"]}`) {
		t.Fatalf("snapshot lost fields: %s", snap)
	}
	// Copy-on-write changes such as favoriting do not drop unknown fields
	r.applyFav(it.ID, true, "", nowMs())
	if string(r.Get(it.ID).Extra["pinnedAt"]) != "123" {
		t.Fatal("copy-on-write lost Extra")
	}
}

func TestItemExtraLimits(t *testing.T) {
	big := `{"id":"a000000000000001","kind":"text","text":"hi","from":"x","origin":"o","ts":5,"blob":"` + strings.Repeat("x", maxExtraBytes) + `"}`
	var it Item
	if err := json.Unmarshal([]byte(big), &it); err != nil || it.Extra != nil || it.Text != "hi" {
		t.Fatalf("oversized extra should be dropped: err=%v extra=%d", err, len(it.Extra))
	}
	// With no unknown fields the output is exactly the same as before
	plain := Item{ID: "a000000000000001", Kind: "text", Text: "hi", From: "x", Origin: "o", TS: 5}
	b, _ := json.Marshal(plain)
	if string(b) != `{"id":"a000000000000001","kind":"text","text":"hi","from":"x","origin":"o","ts":5}` {
		t.Fatalf("plain item JSON changed: %s", b)
	}
}

// This local copy has no unknown fields (e.g. they were lost when an older version forwarded it) but the synced one does: adopt them
func TestAdoptExtra(t *testing.T) {
	s := openStore(t.TempDir())
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: "x", From: "x", Origin: "o", TS: nowMs()})
	ex := map[string]json.RawMessage{"future": json.RawMessage(`1`)}
	if s.adoptExtra("a000000000000001", ex) == nil || s.adoptExtra("a000000000000001", map[string]json.RawMessage{"other": json.RawMessage(`2`)}) != nil {
		t.Fatal("adoptExtra should fill once and keep the first")
	}
}

func TestEIDCache(t *testing.T) {
	var c eidCache
	if !c.first("e1") || c.first("e1") || !c.first("e2") {
		t.Fatal("dedupe")
	}
}

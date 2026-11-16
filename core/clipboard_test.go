package core

import (
	"sync/atomic"
	"testing"
	"time"
)

func testApp(t *testing.T, mut func(*Config)) *App {
	t.Helper()
	c := Config{ID: "a1a1a1a1a1a1a1a1", Name: "MyMac", Code: "K7QFM2XD9P", Port: 1, Settings: Settings{HistoryDays: 30}}
	if mut != nil {
		mut(&c)
	}
	return newApp(t.TempDir(), &c, nil)
}

func TestBlockedApps(t *testing.T) {
	a := testApp(t, func(c *Config) { c.BlockApps = []string{"1Password", "com.apple.keychainaccess", "CHROME.EXE"} })
	cases := []struct {
		id, name string
		want     bool
	}{
		{"com.1password.1password", "1Password", true},        // by name
		{"com.apple.keychainaccess", "Keychain Access", true}, // by ID
		{"chrome.exe", "chrome", true},                        // case-insensitive
		{"com.google.Chrome", "Google Chrome", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := a.noteApp(c.id, c.name); got != c.want {
			t.Errorf("noteApp(%q, %q) = %v", c.id, c.name, got)
		}
	}
	if len(a.RecentApps()) != 4 { // empty ones are not recorded
		t.Errorf("recent apps: %v", a.RecentApps())
	}
}

func TestPaused(t *testing.T) {
	now := time.Now().UnixMilli()
	for _, c := range []struct {
		until int64
		want  bool
	}{{0, false}, {-1, true}, {now + 60_000, true}, {now - 1, false}} {
		a := testApp(t, func(cfg *Config) { cfg.PausedUntil = c.until })
		if a.Paused() != c.want {
			t.Errorf("PausedUntil=%d: Paused() = %v", c.until, a.Paused())
		}
	}
}

func TestIncomingHook(t *testing.T) {
	a := testApp(t, nil)
	var n atomic.Int32
	a.OnIncoming(func(Item) { n.Add(1) })
	// From the local clipboard / the local dashboard: no notification
	a.newText("from this mac", "MyMac", false)
	// From a phone via this machine, or forwarded by another computer: notify
	a.newText("from phone", "iPhone", true)
	a.ingest(&Item{ID: "b2b2b2b2b2b2b2b2", Kind: "text", Text: "from peer", From: "Windows PC", Origin: "x", TS: nowMs()}, "peer1", false)
	// Merged in from history sync: no notification
	a.merge(storeFile{Items: []*Item{{ID: "c3c3c3c3c3c3c3c3", Kind: "text", Text: "old", From: "iPad", Origin: "x", TS: nowMs() - 1000}}}, "peer1")
	time.Sleep(100 * time.Millisecond) // the callback runs in a goroutine
	if got := n.Load(); got != 2 {
		t.Fatalf("incoming notifications = %d, want 2", got)
	}
}

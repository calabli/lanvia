// Package core is the Lanvia node: clipboard watching, LAN discovery and syncing, the HTTP API and the built-in web pages.
// The command line version (cmd/lanvia) and the desktop client (cmd/lanvia-desktop) share it.
package core

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
)

const Version = "0.14.2"

//go:embed web
var webFS embed.FS

type Options struct {
	DataDir string   // Lanvia/ under the system configuration directory when empty
	Port    int      // the port from the configuration file when 0 (default 8686)
	Name    string   // device name
	Join    string   // the pairing code of another device
	NoWatch bool     // do not watch the local clipboard
	Peers   []string // manually specified peers as host:port
	// neither send nor listen for LAN broadcasts, only connect to manually added devices
	NoDiscovery bool
}

func DefaultDataDir() string { return defaultDataDir() }

// Open reads the configuration and prepares the node, but does not listen on the port yet.
func Open(o Options) (*App, error) {
	if o.DataDir == "" {
		o.DataDir = defaultDataDir()
	}
	cfg, err := loadConfig(o.DataDir)
	if err != nil {
		return nil, fmt.Errorf(L("读取配置失败: %w", "Reading configuration failed: %w"), err)
	}
	if o.Port != 0 {
		cfg.Port = o.Port
	}
	if o.Name != "" {
		cfg.Name = o.Name
	}
	if o.Join != "" {
		c := normalizeCode(o.Join)
		if len(c) != codeLen {
			return nil, fmt.Errorf(L("配对码应为 %d 位", "The pairing code has %d characters"), codeLen)
		}
		cfg.Code = c
	}
	if o.NoWatch {
		cfg.Watch = false
	}
	if err := cfg.save(o.DataDir); err != nil {
		return nil, err
	}
	// the --peer flag of the command line only applies to this run, the peers added in the settings are saved
	manual := append([]string(nil), cfg.Peers...)
	for _, p := range o.Peers {
		if a, err := normalizeAddr(p); err == nil && !contains(manual, a) {
			manual = append(manual, a)
		}
	}
	a := newApp(o.DataDir, cfg, manual)
	a.peers.noDiscovery = o.NoDiscovery
	return a, nil
}

// ErrPortInUse usually means that another Lanvia is already running (the command line version or the desktop version).
var ErrPortInUse = errors.New("port in use")

// The message of portError is shown in the current language and it can be checked with errors.Is(err, ErrPortInUse).
type portError struct {
	port int
	err  error
}

func (e *portError) Error() string {
	return fmt.Sprintf("%s (%d): %v", L("端口已被占用", "Port already in use"), e.port, e.err)
}
func (e *portError) Is(target error) bool { return target == ErrPortInUse }
func (e *portError) Unwrap() error        { return e.err }

// Start listens on the port and starts the background tasks, returning immediately on success.
func (a *App) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", a.Port()))
	if err != nil {
		return &portError{a.Port(), err}
	}
	web, _ := fs.Sub(webFS, "web")
	go a.peers.start()
	go a.clip.run()
	go a.presenceLoop()
	go a.pruneLoop()
	go a.fetchLoop()
	go a.fetchLoop() // two in parallel, so a large file does not block the small ones
	go http.Serve(ln, a.routes(web))
	return nil
}

// Close saves the history that has not been written to disk yet.
func (a *App) Close() { a.store.Flush() }

func (a *App) Port() int          { return a.cfgSnapshot().Port }
func (a *App) Name() string       { return a.cfgSnapshot().Name }
func (a *App) Code() string       { return formatCode(a.cfgSnapshot().Code) }
func (a *App) Settings() Settings { return a.cfgSnapshot().Settings }
func (a *App) ClipboardOK() bool  { return a.clip.ok }
func (a *App) LocalURL() string   { return fmt.Sprintf("http://localhost:%d", a.Port()) }

// LANURLs returns the addresses at which this machine can be reached on the LAN, the most likely usable one first.
func (a *App) LANURLs() []string {
	var out []string
	for _, l := range lanAddrs() {
		out = append(out, "http://"+net.JoinHostPort(l.IP.String(), strconv.Itoa(a.Port())))
	}
	return out
}

// PairURL is the address for joining by scanning the QR code; the pairing code is put after the # and is never sent to the server.
func (a *App) PairURL() string {
	urls := a.LANURLs()
	if len(urls) == 0 {
		return ""
	}
	return urls[0] + "/#k=" + a.cfgSnapshot().Code
}

// SetWatch switches between the active (copy sends) and the passive (send manually) mode.
func (a *App) SetWatch(on bool) {
	if on {
		a.clip.resync()
	}
	a.updateCfg(func(c *Config) { c.Watch = on })
}
func (a *App) SetAutoApply(on bool) { a.updateCfg(func(c *Config) { c.AutoApply = on }) }

// PausedUntil returns until when syncing is paused: 0 means not paused, -1 means until it is resumed manually.
func (a *App) PausedUntil() int64 {
	if !a.Paused() {
		return 0
	}
	return a.cfgSnapshot().PausedUntil
}

// Recent returns the n most recent items.
func (a *App) Recent(n int) []Item {
	var out []Item
	for _, it := range a.store.List() {
		if len(out) == n {
			break
		}
		out = append(out, *it)
	}
	return out
}

// CopyToClipboard writes a text to the local system clipboard (without creating a new item).
func (a *App) CopyToClipboard(id string) error {
	it := a.store.Get(id)
	if it == nil {
		return errors.New("内容不存在")
	}
	if it.Kind == "file" {
		return errors.New("只能复制文本")
	}
	return a.clip.Apply(it.PlainText()) // a checklist is copied as a list of "- [ ] item" entries
}

// OnChange registers a callback that is called when the history or the settings change (in its own goroutine).
func (a *App) OnChange(fn func()) {
	a.mu.Lock()
	a.onChange = fn
	a.mu.Unlock()
}

// OnIncoming registers a callback for the items that arrive live from the other devices (called in its own goroutine).
func (a *App) OnIncoming(fn func(Item)) {
	a.mu.Lock()
	a.onIncoming = fn
	a.mu.Unlock()
}

// SetDesktopHook lets the desktop client handle the window actions sent by the web page (hide, main, preview).
// id is the item the action applies to (used for the preview) and may be empty.
func (a *App) SetDesktopHook(fn func(action, id string) bool) {
	a.mu.Lock()
	a.desktopHook = fn
	a.mu.Unlock()
}

func (a *App) changed() {
	a.mu.RLock()
	fn := a.onChange
	a.mu.RUnlock()
	if fn != nil {
		go fn()
	}
}

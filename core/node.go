// Package core 是 Lanvia 节点：剪贴板监听、局域网发现与同步、HTTP API 和内置网页。
// 命令行版（cmd/lanvia）和桌面客户端（cmd/lanvia-desktop）共用它。
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

const Version = "0.7.0"

//go:embed web
var webFS embed.FS

type Options struct {
	DataDir string   // 为空时使用系统配置目录下的 Lanvia/
	Port    int      // 为 0 时使用配置文件里的端口（默认 8686）
	Name    string   // 设备名
	Join    string   // 另一台设备的配对码
	NoWatch bool     // 不监听本机剪贴板
	Peers   []string // 手动指定的对端 host:port
	// 不发送也不收听局域网广播，只连接手动添加的设备
	NoDiscovery bool
}

func DefaultDataDir() string { return defaultDataDir() }

// Open 读取配置并准备好节点，但还不监听端口。
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
	// 命令行的 --peer 只在本次运行有效，设置里添加的会保存
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

// ErrPortInUse 通常意味着已经有一个 Lanvia 在运行（命令行版或桌面版）。
var ErrPortInUse = errors.New("port in use")

// portError 的说明文字按当前语言显示，同时可以用 errors.Is(err, ErrPortInUse) 判断。
type portError struct {
	port int
	err  error
}

func (e *portError) Error() string {
	return fmt.Sprintf("%s (%d): %v", L("端口已被占用", "Port already in use"), e.port, e.err)
}
func (e *portError) Is(target error) bool { return target == ErrPortInUse }
func (e *portError) Unwrap() error        { return e.err }

// Start 监听端口并启动后台任务，成功后立即返回。
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
	go a.fetchLoop() // 两个并发，大文件不至于堵住小文件
	go http.Serve(ln, a.routes(web))
	return nil
}

// Close 把未写盘的历史保存下来。
func (a *App) Close() { a.store.Flush() }

func (a *App) Port() int          { return a.cfgSnapshot().Port }
func (a *App) Name() string       { return a.cfgSnapshot().Name }
func (a *App) Code() string       { return formatCode(a.cfgSnapshot().Code) }
func (a *App) Settings() Settings { return a.cfgSnapshot().Settings }
func (a *App) ClipboardOK() bool  { return a.clip.ok }
func (a *App) LocalURL() string   { return fmt.Sprintf("http://localhost:%d", a.Port()) }

// LANURLs 返回本机在局域网里的访问地址，最可能可用的排在前面。
func (a *App) LANURLs() []string {
	var out []string
	for _, l := range lanAddrs() {
		out = append(out, "http://"+net.JoinHostPort(l.IP.String(), strconv.Itoa(a.Port())))
	}
	return out
}

// PairURL 是扫码加入用的地址，配对码放在 # 后面，不会发给服务器。
func (a *App) PairURL() string {
	urls := a.LANURLs()
	if len(urls) == 0 {
		return ""
	}
	return urls[0] + "/#k=" + a.cfgSnapshot().Code
}

// SetWatch 切换主动（复制即发送）/ 被动（手动发送）模式。
func (a *App) SetWatch(on bool) {
	if on {
		a.clip.resync()
	}
	a.updateCfg(func(c *Config) { c.Watch = on })
}
func (a *App) SetAutoApply(on bool) { a.updateCfg(func(c *Config) { c.AutoApply = on }) }

// PausedUntil 返回暂停到什么时候：0 没有暂停，-1 直到手动恢复。
func (a *App) PausedUntil() int64 {
	if !a.Paused() {
		return 0
	}
	return a.cfgSnapshot().PausedUntil
}

// Recent 返回最近的 n 条内容。
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

// CopyToClipboard 把一条文本写入本机系统剪贴板（不会产生新记录）。
func (a *App) CopyToClipboard(id string) error {
	it := a.store.Get(id)
	if it == nil {
		return errors.New("内容不存在")
	}
	if it.Kind != "text" {
		return errors.New("只能复制文本")
	}
	return a.clip.Apply(it.Text)
}

// OnChange 注册一个回调，在历史或设置变化时调用（在独立的 goroutine 中）。
func (a *App) OnChange(fn func()) {
	a.mu.Lock()
	a.onChange = fn
	a.mu.Unlock()
}

// OnIncoming 注册其他设备实时发来新内容时的回调（在独立的 goroutine 中调用）。
func (a *App) OnIncoming(fn func(Item)) {
	a.mu.Lock()
	a.onIncoming = fn
	a.mu.Unlock()
}

// SetDesktopHook 让桌面客户端处理网页发来的窗口操作（如 hide、main、preview）。
// id 是操作针对的内容（预览时使用），可能为空。
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

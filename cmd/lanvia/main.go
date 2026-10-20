// Lanvia 命令行版：在终端里运行一个节点，适合服务器、Linux 或不需要界面的场景。
// 桌面客户端见 cmd/lanvia-desktop。
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"lanvia/core"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// langFromArgs 在解析参数之前先找出 --lang，这样连 --help 的说明也能用对应的语言。
func langFromArgs(args []string) string {
	for i, a := range args {
		a = strings.TrimLeft(a, "-")
		if v, ok := strings.CutPrefix(a, "lang="); ok {
			return v
		}
		if a == "lang" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func main() {
	lang := langFromArgs(os.Args[1:])
	if lang == "" {
		lang = core.SystemLanguage()
	}
	core.SetLanguage(lang)
	L := core.L

	var o core.Options
	flag.StringVar(&lang, "lang", lang, L("界面语言：zh 或 en（默认跟随系统）", "Language: zh or en (defaults to the system language)"))
	flag.StringVar(&o.DataDir, "data", core.DefaultDataDir(), L("数据目录", "Data directory"))
	flag.IntVar(&o.Port, "port", 0, L("HTTP 端口（默认 8686）", "HTTP port (default 8686)"))
	flag.StringVar(&o.Name, "name", "", L("设备名称", "Device name"))
	flag.StringVar(&o.Join, "join", "", L("使用另一台设备的配对码，加入它的空间", "Join another device's space with its pairing code"))
	flag.BoolVar(&o.NoWatch, "no-watch", false, L("启动时不监听本机剪贴板", "Don't watch this computer's clipboard"))
	flag.BoolVar(&o.NoDiscovery, "no-discovery", false, L("不使用局域网广播，只连接手动添加的设备", "Don't use LAN broadcast; only connect to manually added devices"))
	flag.Var((*listFlag)(&o.Peers), "peer", L("手动指定对端地址 host:port，可重复（广播被路由器屏蔽时使用）", "Peer address host:port, repeatable (use when the router blocks broadcast)"))
	flag.Parse()
	log.SetFlags(log.Ltime)

	a, err := core.Open(o)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Start(); err != nil {
		log.Fatalf(L("%v（可用 --port 换一个端口，或先退出桌面版 Lanvia）", "%v (use --port to pick another port, or quit the Lanvia desktop app first)"), err)
	}
	a.PrintBanner()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	a.Close()
}

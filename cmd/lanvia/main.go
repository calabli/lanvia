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

func main() {
	var o core.Options
	flag.StringVar(&o.DataDir, "data", core.DefaultDataDir(), "数据目录")
	flag.IntVar(&o.Port, "port", 0, "HTTP 端口（默认 8686）")
	flag.StringVar(&o.Name, "name", "", "设备名称")
	flag.StringVar(&o.Join, "join", "", "使用另一台设备的配对码，加入它的空间")
	flag.BoolVar(&o.NoWatch, "no-watch", false, "启动时不监听本机剪贴板")
	flag.Var((*listFlag)(&o.Peers), "peer", "手动指定对端地址 host:port，可重复（广播被路由器屏蔽时使用）")
	flag.Parse()
	log.SetFlags(log.Ltime)

	a, err := core.Open(o)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Start(); err != nil {
		log.Fatalf("%v（可用 --port 换一个端口，或先退出桌面版 Lanvia）", err)
	}
	a.PrintBanner()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	a.Close()
}

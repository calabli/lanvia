// Lanvia：局域网内多设备之间即时共享剪贴板与文件。
//
// 每台电脑运行一个 Lanvia 节点，节点之间通过 UDP 广播互相发现、签名 HTTP 互相转发；
// 手机和平板无需安装，扫码用浏览器打开即可。
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"rsc.io/qr"
)

const version = "0.1.0"

//go:embed web
var webFS embed.FS

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	var (
		dir     = flag.String("data", defaultDataDir(), "数据目录")
		port    = flag.Int("port", 0, "HTTP 端口（默认 8686）")
		name    = flag.String("name", "", "设备名称")
		join    = flag.String("join", "", "使用另一台设备的配对码，加入它的空间")
		noWatch = flag.Bool("no-watch", false, "启动时不监听本机剪贴板")
		peers   listFlag
	)
	flag.Var(&peers, "peer", "手动指定对端地址 host:port，可重复（广播被路由器屏蔽时使用）")
	flag.Parse()
	log.SetFlags(log.Ltime)

	cfg, err := loadConfig(*dir)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if *name != "" {
		cfg.Name = *name
	}
	if *join != "" {
		if c := normalizeCode(*join); len(c) == codeLen {
			cfg.Code = c
		} else {
			log.Fatalf("配对码应为 %d 位", codeLen)
		}
	}
	if *noWatch {
		cfg.Watch = false
	}
	cfg.save(*dir)

	a := newApp(*dir, cfg, peers)
	web, _ := fs.Sub(webFS, "web")

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		log.Fatalf("端口 %d 不可用: %v（可用 --port 换一个）", cfg.Port, err)
	}
	banner(a)

	go a.peers.start()
	go a.clip.run()
	go a.presenceLoop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		a.store.Flush()
		os.Exit(0)
	}()

	log.Fatal(http.Serve(ln, a.routes(web)))
}

func banner(a *App) {
	c := a.cfgSnapshot()
	addrs := lanAddrs()
	fmt.Printf("\n  \x1b[1mLanvia %s\x1b[0m · %s\n\n", version, c.Name)
	fmt.Printf("  配对码      \x1b[1;36m%s\x1b[0m\n", formatCode(c.Code))
	fmt.Printf("  本机仪表盘  http://localhost:%d\n", c.Port)
	for i, l := range addrs {
		label := "            "
		if i == 0 {
			label = "  局域网地址"
		}
		fmt.Printf("%s  http://%s:%d\n", label, l.IP, c.Port)
	}
	if !a.clip.ok {
		fmt.Println("\n  \x1b[33m未找到剪贴板工具，本机剪贴板同步不可用（Linux 请安装 xclip 或 wl-clipboard）\x1b[0m")
	}
	if len(addrs) > 0 {
		fmt.Println("\n  用手机相机扫码即可加入：")
		printQR(fmt.Sprintf("http://%s:%d/#k=%s", addrs[0].IP, c.Port, c.Code))
	}
	fmt.Println()
}

// printQR 用半高方块字符在终端里画二维码，强制白底黑码，深色终端也能扫。
func printQR(text string) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return
	}
	const quiet = 2
	black := func(x, y int) bool {
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return false
		}
		return code.Black(x, y)
	}
	color := func(b bool) int {
		if b {
			return 16
		}
		return 231
	}
	var sb strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		sb.WriteString("  ")
		for x := -quiet; x < code.Size+quiet; x++ {
			fmt.Fprintf(&sb, "\x1b[38;5;%dm\x1b[48;5;%dm▀", color(black(x, y)), color(black(x, y+1)))
		}
		sb.WriteString("\x1b[0m\n")
	}
	fmt.Print(sb.String())
}

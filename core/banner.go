package core

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// PrintBanner 在终端打印配对码、访问地址和扫码用的二维码。
func (a *App) PrintBanner() {
	c := a.cfgSnapshot()
	urls := a.LANURLs()
	fmt.Printf("\n  \x1b[1mLanvia %s\x1b[0m · %s\n\n", Version, c.Name)
	fmt.Printf("  配对码      \x1b[1;36m%s\x1b[0m\n", formatCode(c.Code))
	fmt.Printf("  本机仪表盘  %s\n", a.LocalURL())
	for i, u := range urls {
		label := "            "
		if i == 0 {
			label = "  局域网地址"
		}
		fmt.Printf("%s  %s\n", label, u)
	}
	if !a.clip.ok {
		fmt.Println("\n  \x1b[33m未找到剪贴板工具，本机剪贴板同步不可用（Linux 请安装 xclip 或 wl-clipboard）\x1b[0m")
	}
	if u := a.PairURL(); u != "" {
		fmt.Println("\n  用手机相机扫码即可加入：")
		printQR(u)
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

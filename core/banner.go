package core

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// PrintBanner prints the pairing code, the access URLs and the QR code used for scanning in the terminal.
func (a *App) PrintBanner() {
	c := a.cfgSnapshot()
	urls := a.LANURLs()
	fmt.Printf("\n  \x1b[1mLanvia %s\x1b[0m · %s\n\n", Version, c.Name)
	lbl := func(zh, en string) string { // label alignment: pad with spaces by display width
		if english.Load() {
			return fmt.Sprintf("%-14s", en)
		}
		return zh
	}
	fmt.Printf("  %s\x1b[1;36m%s\x1b[0m\n", lbl("配对码      ", "Pairing code"), formatCode(c.Code))
	fmt.Printf("  %s%s\n", lbl("本机仪表盘  ", "Dashboard"), a.LocalURL())
	for i, u := range urls {
		label := strings.Repeat(" ", 12)
		if english.Load() {
			label = strings.Repeat(" ", 14)
		}
		if i == 0 {
			label = lbl("局域网地址  ", "LAN address")
		}
		fmt.Printf("  %s%s\n", label, u)
	}
	if !a.clip.ok {
		fmt.Println("\n  \x1b[33m" + L("未找到剪贴板工具，本机剪贴板同步不可用（Linux 请安装 xclip 或 wl-clipboard）",
			"No clipboard tool found, clipboard sync is unavailable (on Linux install xclip or wl-clipboard)") + "\x1b[0m")
	}
	if u := a.PairURL(); u != "" {
		fmt.Println("\n  " + L("用手机相机扫码即可加入：", "Scan with your phone camera to join:"))
		printQR(u)
	}
	fmt.Println()
}

// printQR draws a QR code in the terminal with half-height block characters, forcing a white background and a dark code so it stays scannable on dark terminals.
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

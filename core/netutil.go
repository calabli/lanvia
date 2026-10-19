package core

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"sort"
	"strings"
)

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type lanAddr struct {
	IP    net.IP
	Bcast net.IP
	Iface string
}

// lanAddrs 列出可用于局域网的 IPv4 地址，常见的物理网卡排在前面。
func lanAddrs() []lanAddr {
	var out []lanAddr
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			mask := net.IP(ipn.Mask).To4()
			if mask == nil {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip4[i] | ^mask[i]
			}
			out = append(out, lanAddr{IP: ip4, Bcast: b, Iface: ifc.Name})
		}
	}
	score := func(a lanAddr) int {
		s := 0
		if a.IP.IsPrivate() {
			s += 10
		}
		n := strings.ToLower(a.Iface)
		for _, p := range []string{"en", "eth", "wlan", "wi-fi", "wlp", "enp", "以太网", "无线"} {
			if strings.HasPrefix(n, p) {
				s += 5
			}
		}
		for _, p := range []string{"utun", "docker", "br-", "veth", "vmnet", "vbox", "bridge", "tailscale", "zt"} {
			if strings.HasPrefix(n, p) {
				s -= 8
			}
		}
		if a.IP[0] == 198 && a.IP[1] == 18 { // 代理软件的虚拟网段
			s -= 8
		}
		return s
	}
	sort.SliceStable(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	return out
}

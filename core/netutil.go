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

// 虚拟网卡：VPN/代理隧道、容器、虚拟机。它们的地址手机连不上，不应出现在二维码和地址列表里。
var (
	virtualPrefixes = []string{"utun", "tun", "tap", "zt", "br-", "veth", "docker", "vmnet", "vboxnet", "bridge", "awdl", "llw"}
	virtualContains = []string{"vethernet", "vmware", "virtualbox", "hyper-v", "wsl", "tailscale", "zerotier", "docker", "loopback", "pseudo"}
	proxyNet        = &net.IPNet{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)} // 代理软件 fake-ip 网段
)

func isVirtual(ifc net.Interface, ip net.IP) bool {
	if ifc.Flags&net.FlagPointToPoint != 0 || proxyNet.Contains(ip) {
		return true
	}
	n := strings.ToLower(ifc.Name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	for _, c := range virtualContains {
		if strings.Contains(n, c) {
			return true
		}
	}
	return false
}

// lanAddrs 列出可用于局域网的 IPv4 地址，排除虚拟网卡，私有地址和常见物理网卡排在前面。
// 如果过滤后一个都不剩（比如只有 VPN），退回到全部地址，至少还能手动尝试。
func lanAddrs() []lanAddr {
	var real, all []lanAddr
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
			l := lanAddr{IP: ip4, Bcast: b, Iface: ifc.Name}
			all = append(all, l)
			if !isVirtual(ifc, ip4) {
				real = append(real, l)
			}
		}
	}
	out := real
	if len(out) == 0 {
		out = all
	}
	score := func(a lanAddr) int {
		s := 0
		if a.IP.IsPrivate() {
			s += 10
		}
		n := strings.ToLower(a.Iface)
		for _, p := range []string{"en", "eth", "wlan", "wi-fi", "wlp", "以太网", "无线"} {
			if strings.HasPrefix(n, p) {
				s += 5
				break
			}
		}
		return s
	}
	sort.SliceStable(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	return out
}

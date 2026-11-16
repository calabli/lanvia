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

// Virtual network interfaces: VPN/proxy tunnels, containers, virtual machines. Phones cannot reach their addresses, so they should not show up in the QR code or in the address list.
var (
	virtualPrefixes = []string{"utun", "tun", "tap", "zt", "br-", "veth", "docker", "vmnet", "vboxnet", "bridge", "awdl", "llw"}
	virtualContains = []string{"vethernet", "vmware", "virtualbox", "hyper-v", "wsl", "tailscale", "zerotier", "docker", "loopback", "pseudo"}
	proxyNet        = &net.IPNet{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)} // fake-ip range used by proxy software
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

// lanAddrs lists the IPv4 addresses usable on the LAN, excluding virtual interfaces, with private addresses and common physical interfaces sorted first.
// If nothing is left after the filtering (for example only a VPN), it falls back to all addresses, so that they can still be tried manually.
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

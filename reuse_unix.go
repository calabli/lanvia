//go:build darwin || linux || freebsd

package main

import "syscall"

// 允许同一台机器上多个实例同时收听广播端口（方便调试，也避免端口被占时直接失败）。
func reuseControl(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	})
}

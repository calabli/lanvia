//go:build darwin || linux || freebsd

package core

import "syscall"

// Allow several instances on the same machine to listen on the broadcast port at once (handy for debugging, and it avoids failing outright when the port is taken).
func reuseControl(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	})
}

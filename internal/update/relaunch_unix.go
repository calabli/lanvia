//go:build !windows

package update

import "os/exec"

// Relaunch reopens path after the current program exits. The desktop app is a single
// instance, so the new version has to wait for the old one; a background shell opens it shortly after.
func Relaunch(path string) error {
	return exec.Command("sh", "-c", `sleep 1.5; open -n "$0"`, path).Start()
}

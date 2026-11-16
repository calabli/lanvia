package update

import (
	"os/exec"
	"syscall"
)

// Relaunch restarts path after the current program exits (single instance: the new version has to wait for the old one).
func Relaunch(path string) error {
	cmd := exec.Command("cmd", "/C", "ping -n 3 127.0.0.1 >NUL & start \"\" \""+path+"\"")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd.Start()
}

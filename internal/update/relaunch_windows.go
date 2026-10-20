package update

import (
	"os/exec"
	"syscall"
)

// Relaunch 在当前程序退出后重新启动 path（单实例：新版要等旧版退出）。
func Relaunch(path string) error {
	cmd := exec.Command("cmd", "/C", "ping -n 3 127.0.0.1 >NUL & start \"\" \""+path+"\"")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd.Start()
}

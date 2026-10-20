//go:build !windows

package update

import "os/exec"

// Relaunch 在当前程序退出后重新打开 path。桌面版是单实例，新版必须等旧版退出后才能启动，
// 所以交给一个后台 shell 稍等片刻再打开。
func Relaunch(path string) error {
	return exec.Command("sh", "-c", `sleep 1.5; open -n "$0"`, path).Start()
}

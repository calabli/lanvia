package main

import (
	"os"
	"os/exec"
	"strings"
)

// pbpaste/pbcopy 需要 UTF-8 的 locale，否则中文和 emoji 会乱码。
func pbEnv() []string { return append(os.Environ(), "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8") }

func clipboardSupported() bool { _, err := exec.LookPath("pbpaste"); return err == nil }

func readClipboard() (string, error) {
	cmd := exec.Command("pbpaste")
	cmd.Env = pbEnv()
	out, err := cmd.Output()
	return string(out), err
}

func writeClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Env = pbEnv()
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

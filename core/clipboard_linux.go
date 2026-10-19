package core

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

type clipTool struct{ read, write []string }

func linuxClipTool() *clipTool {
	cands := []clipTool{
		{[]string{"xclip", "-selection", "clipboard", "-o"}, []string{"xclip", "-selection", "clipboard"}},
		{[]string{"xsel", "--clipboard", "--output"}, []string{"xsel", "--clipboard", "--input"}},
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cands = append([]clipTool{{[]string{"wl-paste", "--no-newline"}, []string{"wl-copy"}}}, cands...)
	}
	for _, c := range cands {
		if _, err := exec.LookPath(c.read[0]); err == nil {
			return &c
		}
	}
	return nil
}

var linuxTool = linuxClipTool()

func clipboardSupported() bool { return linuxTool != nil }

func readClipboard() (string, error) {
	if linuxTool == nil {
		return "", errors.New("no clipboard tool")
	}
	out, err := exec.Command(linuxTool.read[0], linuxTool.read[1:]...).Output()
	return string(out), err
}

func writeClipboard(s string) error {
	if linuxTool == nil {
		return errors.New("no clipboard tool")
	}
	cmd := exec.Command(linuxTool.write[0], linuxTool.write[1:]...)
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func skipClipboardChange() bool { return false }

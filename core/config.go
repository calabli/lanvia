package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Settings 是可在仪表盘里实时切换的选项。
type Settings struct {
	AutoApply bool `json:"autoApply"` // 收到其他设备的文本时写入本机剪贴板
	Watch     bool `json:"watch"`     // 监听本机剪贴板并自动分享
}

type Config struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"` // 配对码（规范化后，不含分隔符）
	Port int    `json:"port"`
	Settings
}

const defaultPort = 8686

func defaultDataDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "Lanvia")
}

func loadConfig(dir string) (*Config, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	c := &Config{}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		c.Settings = Settings{AutoApply: true, Watch: true}
	default:
		return nil, err
	}
	if c.ID == "" {
		c.ID = randHex(8)
	}
	if c.Name == "" {
		c.Name = hostName()
	}
	if normalizeCode(c.Code) == "" {
		c.Code = newCode()
	}
	c.Code = normalizeCode(c.Code)
	if c.Port == 0 {
		c.Port = defaultPort
	}
	return c, c.save(dir)
}

func (c *Config) save(dir string) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := filepath.Join(dir, "config.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "config.json"))
}

// hostName 返回一个对人友好的设备名。
func hostName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return s
			}
		}
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "Lanvia-" + runtime.GOOS
	}
	return strings.TrimSuffix(h, ".local")
}

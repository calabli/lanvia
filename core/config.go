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
	Watch     bool `json:"watch"`     // 主动模式：复制即发送；false 为被动模式，手动发送
	// 历史保留天数，0 表示永久。只对本机生效：过期内容会被删除，也不会再从其他设备同步回来。
	HistoryDays int `json:"historyDays"`
	// 自动下载其他设备发来的文件（MB 上限）：0 关闭，-1 不限。发送方关机后本机仍保有一份。
	AutoFetchMB int `json:"autoFetchMB"`
	// 文件占用上限（MB），0 不限。超出时只删其他设备文件的副本，本机的原件不删。
	StorageMB int `json:"storageMB"`
	// 只允许加密连接：拒绝不支持加密的旧版本节点，也防止中间人去掉加密
	StrictEnc bool `json:"strictEnc"`
	// 界面语言：空为跟随系统，"zh" 或 "en"。本机的网页窗口和托盘菜单共用
	Lang string `json:"lang"`
}

const (
	defaultHistoryDays = 30
	defaultAutoFetchMB = 100
	defaultStorageMB   = 2048
)

type Config struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"` // 配对码（规范化后，不含分隔符）
	Port int    `json:"port"`
	// 手动添加的设备地址（host:port），自动发现失效时使用
	Peers []string `json:"peers,omitempty"`
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
		// 旧版本的配置里没有这一项：默认不再永久保存
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) == nil {
			if _, ok := raw["historyDays"]; !ok {
				c.HistoryDays = defaultHistoryDays
			}
			if _, ok := raw["autoFetchMB"]; !ok {
				c.AutoFetchMB = defaultAutoFetchMB
			}
			if _, ok := raw["storageMB"]; !ok {
				c.StorageMB = defaultStorageMB
			}
		}
	case errors.Is(err, fs.ErrNotExist):
		c.Settings = Settings{AutoApply: true, Watch: true, HistoryDays: defaultHistoryDays, AutoFetchMB: defaultAutoFetchMB, StorageMB: defaultStorageMB}
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

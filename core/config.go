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

// Settings are the options that can be switched live from the dashboard.
type Settings struct {
	AutoApply bool `json:"autoApply"` // write text received from other devices into the local clipboard
	Watch     bool `json:"watch"`     // active mode: copying sends right away; false is passive mode, where sending is manual
	// number of days to keep the history, 0 means forever. Only applies to this machine: expired content is deleted and is not synced back from other devices.
	HistoryDays int `json:"historyDays"`
	// automatically download the files sent by other devices (limit in MB): 0 disables it, -1 means unlimited. A copy is kept on this machine even after the sender shuts down.
	AutoFetchMB int `json:"autoFetchMB"`
	// storage limit in MB, 0 means unlimited. When exceeded, only copies of files from other devices are deleted, never the originals on this machine.
	StorageMB int `json:"storageMB"`
	// allow only encrypted connections: reject old nodes without encryption support and prevent man-in-the-middle from stripping the encryption
	StrictEnc bool `json:"strictEnc"`
	// interface language: empty means follow the system, "zh" or "en". Shared by the web window and the tray menu on this machine
	Lang string `json:"lang"`
	// show a system notification when content arrives from another device (desktop version)
	Notify bool `json:"notify"`
	// pause syncing until this time (in milliseconds); -1 means until it is resumed manually, 0 means not paused
	PausedUntil int64 `json:"pausedUntil"`
	// application blacklist: content copied in these applications is not sent (bundle id / process name / application name, case-insensitive)
	BlockApps []string `json:"blockApps"`
	// disable token access (shortcuts, curl). Both the token and the content are transmitted in plain text
	TokenOff bool `json:"tokenOff"`
	// automatic updates (desktop version): download a new version in the background and, when the Lanvia window is not in use, install it and reopen automatically
	AutoUpdate bool `json:"autoUpdate"`
}

const (
	defaultHistoryDays = 30
	defaultAutoFetchMB = 100
	defaultStorageMB   = 2048
)

type Config struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"` // pairing code (normalized, without separators)
	Port int    `json:"port"`
	// manually added device addresses (host:port), used when auto discovery fails
	Peers []string `json:"peers,omitempty"`
	// regenerated automation token; empty means the one derived from the pairing code is used
	Token string `json:"token,omitempty"`
	// version of the last run, used to show "updated to vX" after an automatic update
	LastVersion string `json:"lastVersion,omitempty"`
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
		// older configurations do not have this entry: no longer keep everything forever by default
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
			if _, ok := raw["notify"]; !ok {
				c.Notify = true
			}
			if _, ok := raw["autoUpdate"]; !ok {
				c.AutoUpdate = true
			}
		}
	case errors.Is(err, fs.ErrNotExist):
		c.Settings = Settings{AutoApply: true, Watch: true, HistoryDays: defaultHistoryDays, AutoFetchMB: defaultAutoFetchMB, StorageMB: defaultStorageMB, Notify: true, AutoUpdate: true}
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

// hostName returns a human friendly device name.
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

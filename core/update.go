package core

// 桌面版的检查更新、自动更新：下载和安装在桌面客户端里做（见 cmd/lanvia-desktop/updater.go），
// 这里只保存状态、提供给设置页的接口，并把变化推给打开着的窗口。命令行版没有自动更新。

// UpdateState 是更新的当前状态。
type UpdateState struct {
	Current     string  `json:"current"`            // 当前版本
	Supported   bool    `json:"supported"`          // 能自动更新（桌面版）
	Latest      string  `json:"latest,omitempty"`   // 可以更新到的版本（没有新版本时为空）
	Notes       string  `json:"notes,omitempty"`    // 新版本的更新说明
	URL         string  `json:"url,omitempty"`      // 发布页面
	Checking    bool    `json:"checking,omitempty"` // 正在检查
	Downloading bool    `json:"downloading,omitempty"`
	Progress    float64 `json:"progress,omitempty"` // 下载进度 0–1
	Ready       bool    `json:"ready,omitempty"`    // 已下载并校验，等待安装
	Installing  bool    `json:"installing,omitempty"`
	Error       string  `json:"error,omitempty"`     // 最近一次失败的原因
	CheckedAt   int64   `json:"checkedAt,omitempty"` // 最近一次检查的时间（毫秒）
}

// SetUpdateState 由桌面客户端在更新状态变化时调用。
func (a *App) SetUpdateState(s UpdateState) {
	s.Current, s.Supported = Version, true
	a.mu.Lock()
	a.upd = &s
	a.mu.Unlock()
	a.hub.Broadcast("updater", s)
}

// GetUpdateState 返回当前的更新状态。
func (a *App) GetUpdateState() UpdateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.upd == nil {
		return UpdateState{Current: Version}
	}
	return *a.upd
}

// NoteVersion 记下这次运行的版本，返回上次运行的版本（第一次运行时为空）。
// 桌面版用它在自动更新后提示“已更新到 vX”。
func (a *App) NoteVersion() string {
	prev := a.cfgSnapshot().LastVersion
	if prev != Version {
		a.updateCfg(func(c *Config) { c.LastVersion = Version })
	}
	return prev
}

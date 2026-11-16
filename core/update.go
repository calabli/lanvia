package core

// Update checking and auto update for the desktop version: the download and the installation happen in
// the desktop client (see cmd/lanvia-desktop/updater.go); here we only keep the state, expose it to the settings page and push the changes to the open windows. The command line version has no auto update.

// UpdateState is the current state of the update.
type UpdateState struct {
	Current     string  `json:"current"`            // current version
	Supported   bool    `json:"supported"`          // auto update is possible (desktop version)
	Latest      string  `json:"latest,omitempty"`   // the version it can update to (empty when there is no new version)
	Notes       string  `json:"notes,omitempty"`    // release notes of the new version
	URL         string  `json:"url,omitempty"`      // release page
	Checking    bool    `json:"checking,omitempty"` // checking in progress
	Downloading bool    `json:"downloading,omitempty"`
	Progress    float64 `json:"progress,omitempty"` // download progress 0–1
	Ready       bool    `json:"ready,omitempty"`    // downloaded and verified, waiting to be installed
	Installing  bool    `json:"installing,omitempty"`
	Error       string  `json:"error,omitempty"`     // reason of the most recent failure
	CheckedAt   int64   `json:"checkedAt,omitempty"` // time of the most recent check (in milliseconds)
}

// SetUpdateState is called by the desktop client when the update state changes.
func (a *App) SetUpdateState(s UpdateState) {
	s.Current, s.Supported = Version, true
	a.mu.Lock()
	a.upd = &s
	a.mu.Unlock()
	a.hub.Broadcast("updater", s)
}

// GetUpdateState returns the current update state.
func (a *App) GetUpdateState() UpdateState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.upd == nil {
		return UpdateState{Current: Version}
	}
	return *a.upd
}

// NoteVersion records the version of this run and returns the version of the previous run
// (empty on the first run). The desktop version uses it to show "updated to vX" after an auto update.
func (a *App) NoteVersion() string {
	prev := a.cfgSnapshot().LastVersion
	if prev != Version {
		a.updateCfg(func(c *Config) { c.LastVersion = Version })
	}
	return prev
}

package winui

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

// appSettings holds the small set of preferences that are owned by the GUI
// itself (Tailscale preferences are edited through LocalAPI instead). They are
// stored next to the log file in %APPDATA%\Trayscale on Windows.
type appSettings struct {
	StartWithWindows bool `json:"startWithWindows"`
	ShowOffline      bool `json:"showOffline"`
	CompactRows      bool `json:"compactRows"`
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Trayscale", "settings.json")
}

func loadSettings() appSettings {
	// Offline devices stay visible by default; the Settings toggle can hide
	// them. The other preferences default to off.
	s := appSettings{ShowOffline: true}
	p := settingsPath()
	if p == "" {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("could not read settings", "err", err)
		}
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		slog.Warn("invalid settings file", "err", err)
	}
	return s
}

func saveSettings(s appSettings) {
	p := settingsPath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		slog.Warn("could not create settings directory", "err", err)
		return
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		slog.Warn("could not save settings", "err", err)
	}
}

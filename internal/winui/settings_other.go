//go:build !windows

package winui

import "os/exec"

// Non-Windows builds keep the settings file and UI, but the Windows-specific
// startup registration is a no-op.
func setRunAtStartup(bool) error { return nil }

func openSystemPath(path string) error {
	return exec.Command("xdg-open", path).Start()
}

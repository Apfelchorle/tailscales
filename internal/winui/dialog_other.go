//go:build !windows

package winui

import (
	"os/exec"
	"strings"
)

// openFileDialog shows a native file-open dialog on non-Windows systems and
// returns the selected path. ok is false if the user cancelled or no picker is
// available. It shells out to the desktop's own file chooser — zenity on
// GNOME, kdialog on KDE — so the dialog matches the rest of the session.
func openFileDialog() (path string, ok bool) {
	for _, c := range [][]string{
		{"zenity", "--file-selection", "--title=Select a file to send"},
		{"kdialog", "--getopenfilename"},
	} {
		bin, err := exec.LookPath(c[0])
		if err != nil {
			continue // picker not installed, try the next one
		}
		out, err := exec.Command(bin, c[1:]...).Output()
		if err != nil {
			return "", false // non-zero exit == user cancelled
		}
		if p := strings.TrimSpace(string(out)); p != "" {
			return p, true
		}
		return "", false
	}
	return "", false
}

//go:build windows

package winui

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows/registry"
)

// setRunAtStartup adds or removes the GUI executable from the current user's
// Run key. The tray app is intentionally not registered: the GUI is the
// primary window people expect to see at login.
func setRunAtStartup(enable bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enable {
		if err := key.DeleteValue("Trayscale"); err == registry.ErrNotExist {
			return nil
		} else {
			return err
		}
	}
	return key.SetStringValue("Trayscale", fmt.Sprintf(`"%s"`, exe))
}

// openSystemPath asks the Windows shell to open a file or folder with its
// default handler (Notepad for .log, Explorer for directories).
func openSystemPath(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

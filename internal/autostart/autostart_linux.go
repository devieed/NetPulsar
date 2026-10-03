//go:build linux

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// Apply writes or removes an XDG autostart entry for the current executable.
func Apply(enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "autostart")
	path := filepath.Join(dir, "netpulsar.desktop")
	if !enabled {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	body := "[Desktop Entry]\nType=Application\nName=NetPulsar\nX-GNOME-Autostart-enabled=true\nExec=" + quoteExec(exe) + "\n"
	return os.WriteFile(path, []byte(body), 0o644)
}

func quoteExec(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

//go:build darwin

package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Apply writes or removes a per-user Launch Agent that starts NetPulsar at login.
func Apply(enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	path := filepath.Join(dir, "com.netpulsar.widget.plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if !enabled {
		_ = exec.Command("launchctl", "bootout", domain, path).Run()
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
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.netpulsar.widget</string>
<key>RunAtLoad</key><true/>
<key>ProgramArguments</key><array><string>` + xmlEscape(exe) + `</string></array>
</dict></plist>
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain, path).Run()
	return exec.Command("launchctl", "bootstrap", domain, path).Run()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

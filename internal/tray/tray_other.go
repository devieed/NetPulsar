//go:build !windows

package tray

// Start is a no-op where there is no tray icon. The window keeps its close button.
func Start(_ []byte, _ string, _ Labels, _ Handlers) error {
	return errUnsupported
}

// SetLabels is a no-op without a tray icon.
func SetLabels(Labels) {}

// Stop is a no-op without a tray icon.
func Stop() {}

// Enabled reports whether a tray icon is showing.
func Enabled() bool { return false }

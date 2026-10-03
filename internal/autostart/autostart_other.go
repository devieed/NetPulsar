//go:build !windows && !linux && !darwin

package autostart

// Apply is a no-op on systems without a desktop login-item API.
func Apply(enabled bool) error {
	_ = enabled
	return nil
}

//go:build !windows && !darwin && !linux

package desktop

// Raise is unused where no native window query exists.
func Raise(title string, topmost bool) {
	_, _ = title, topmost
}

// HideFromTaskbar is unused on this system.
func HideFromTaskbar(title string) { _ = title }

func Query(title string) Info {
	_ = title
	return Info{Scale: 1}
}

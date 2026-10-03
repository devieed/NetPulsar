//go:build !windows

package tray

import "errors"

var errUnsupported = errors.New("tray is not available")

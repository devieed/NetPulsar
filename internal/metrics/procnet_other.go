//go:build !windows

package metrics

func procNetSnapshot() map[int32]netCum {
	return nil
}

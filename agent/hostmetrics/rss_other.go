//go:build !linux

package hostmetrics

// readRSS is unavailable off Linux without cgo or extra dependencies; the
// field ships as 0 and the UI treats it as absent.
func readRSS() uint64 { return 0 }

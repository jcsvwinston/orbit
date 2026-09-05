//go:build !linux

package hostmetrics

// readRSS is unavailable off Linux without cgo or extra dependencies; the
// field ships as 0 and the UI treats it as absent.
// readRSS is 0 on every platform but Linux, where /proc/self/statm is
// read: there is no portable resident-set accessor in the standard
// library, and Go's runtime metrics report heap, not RSS. A dashboard
// reads 0 as "not reported", which it is (F15).
func readRSS() uint64 { return 0 }

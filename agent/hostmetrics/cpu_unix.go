//go:build unix

package hostmetrics

import (
	"syscall"
	"time"
)

// cpuTime returns the process's cumulative user+system CPU time.
func cpuTime() (time.Duration, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	toDur := func(tv syscall.Timeval) time.Duration {
		return time.Duration(tv.Sec)*time.Second + time.Duration(tv.Usec)*time.Microsecond
	}
	return toDur(ru.Utime) + toDur(ru.Stime), true
}

//go:build !unix

package hostmetrics

import "time"

func cpuTime() (time.Duration, bool) { return 0, false }

//go:build !windows

package treesitter_test

import "time"

var clockEpoch = time.Now()

// clockMicros returns a time in microseconds since the test binary started.
func clockMicros() float64 {
	return float64(time.Since(clockEpoch).Nanoseconds()) / 1e3
}

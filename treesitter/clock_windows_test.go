package treesitter_test

import (
	"syscall"
	"unsafe"
)

// The benchmark reads QueryPerformanceCounter, because time.Now resolves only
// about 0.5 ms on the machines it runs on, and the typing cost is about 10 µs.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	qpcProc  = kernel32.NewProc("QueryPerformanceCounter")
	qpfProc  = kernel32.NewProc("QueryPerformanceFrequency")
	qpcFreq  = queryFrequency()
)

func queryFrequency() int64 {
	var f int64
	qpfProc.Call(uintptr(unsafe.Pointer(&f)))
	return f
}

// clockMicros returns a high-resolution time in microseconds.
func clockMicros() float64 {
	var c int64
	qpcProc.Call(uintptr(unsafe.Pointer(&c)))
	return float64(c) * 1e6 / float64(qpcFreq)
}

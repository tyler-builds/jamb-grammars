package treesitter_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	grammars "github.com/tyler-builds/jamb-grammars"
	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/python"
	"github.com/tyler-builds/jamb-grammars/treesitter"
	"github.com/tyler-builds/jamb-grammars/typescript"
	"github.com/tyler-builds/jamb/codeedit/highlight"
)

// TestPipelineReport repeats the typing measurement of the prior work through
// the Session API. For each real file, it types three words of ten letters near
// the middle, one key every interval, and reports the cost of each Edit call on
// the typing thread, and the time from each key until its colors are published.
// It runs only when JAMB_HIGHLIGHT_INPUTS names the directory of the input files
// (go4k.go, go110k.go, color.ts, markdown.ts, libdom.ts, typing.py,
// pydecimal.py), and is skipped otherwise.
func TestPipelineReport(t *testing.T) {
	dir := os.Getenv("JAMB_HIGHLIGHT_INPUTS")
	if dir == "" {
		t.Skip("set JAMB_HIGHLIGHT_INPUTS to the directory of the benchmark inputs")
	}
	inputs := []struct {
		file string
		lang *grammars.Grammar
	}{
		{"go4k.go", golang.Grammar}, {"go110k.go", golang.Grammar},
		{"color.ts", typescript.TypeScript}, {"markdown.ts", typescript.TypeScript}, {"libdom.ts", typescript.TypeScript},
		{"typing.py", python.Grammar}, {"pydecimal.py", python.Grammar},
	}
	for _, in := range inputs {
		src, err := os.ReadFile(filepath.Join(dir, in.file))
		if err != nil {
			t.Fatal(err)
		}
		for _, interval := range []float64{80, 30} {
			report := pipeline(t, in.lang, src, interval)
			fmt.Printf("%-14s %3.0fms/key  %s\n", in.file, interval, report)
		}
	}
}

// pipeline types the benchmark's keys into a session on src and returns a report
// line. It also prints the heap used by the session.
func pipeline(t *testing.T, lang *grammars.Grammar, src []byte, interval float64) string {
	t.Helper()
	var mu sync.Mutex
	var pendingAt []float64 // key times, by version
	var latency []float64   // time from each key until its colors are published
	var sess atomic.Pointer[treesitter.Session]
	first := make(chan struct{})
	var firstOnce sync.Once
	notify := func() {
		firstOnce.Do(func() { close(first) })
		s := sess.Load()
		if s == nil {
			return // the first publish, before NewSession returns
		}
		now := clockMicros()
		v := s.Synced()
		mu.Lock()
		defer mu.Unlock()
		for len(latency) < len(pendingAt) && v >= uint64(len(latency)+1) {
			latency = append(latency, now-pendingAt[len(latency)])
		}
	}

	before := heapInUse()
	s, err := treesitter.NewSession(lang, src, treesitter.Options{Notify: notify})
	if err != nil {
		t.Fatal(err)
	}
	sess.Store(s)
	defer s.Close()
	select {
	case <-first:
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for the first spans")
	}
	heap := heapInUse() - before

	rng := rand.New(rand.NewSource(2))
	var typing []float64
	start := clockMicros()
	version := 0
	for w := range 3 {
		pos := lineStart(src, len(src)/2+rng.Intn(len(src)/20)) + w
		lo, hi := window(src, pos, 40)
		if err := s.SetViewport(lo, hi); err != nil {
			t.Fatal(err)
		}
		for k, c := range []byte("abcdefghij") {
			due := start + float64(version)*interval*1000
			for clockMicros() < due {
				time.Sleep(200 * time.Microsecond)
			}
			version++
			t0 := clockMicros()
			mu.Lock()
			pendingAt = append(pendingAt, t0)
			mu.Unlock()
			if err := s.Edit(highlight.EditRecord{Start: pos + k, OldEnd: pos + k, Text: []byte{c}}); err != nil {
				t.Fatal(err)
			}
			typing = append(typing, clockMicros()-t0)
		}
	}
	waitPublished(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(latency) == len(pendingAt)
	})
	mu.Lock()
	lat := slices.Clone(latency)
	mu.Unlock()
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("typing thread %s; colors current %s; heap %s",
		stats(typing), stats(lat), kib(heap))
}

// waitPublished waits until done reports true, failing the test after 60 seconds.
func waitPublished(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the worker")
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// stats formats the p50, p95 and max of v in milliseconds.
func stats(v []float64) string {
	if len(v) == 0 {
		return "n/a"
	}
	s := slices.Clone(v)
	sort.Float64s(s)
	return fmt.Sprintf("%.2f/%.2f/%.2f ms", s[len(s)/2]/1000, s[len(s)*95/100]/1000, s[len(s)-1]/1000)
}

// heapInUse returns the bytes of in-use heap after a collection.
func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

func kib(n uint64) string {
	return fmt.Sprintf("%d KiB", n/1024)
}

// lineStart returns the offset of the start of the line containing at.
func lineStart(src []byte, at int) int { return bytes.LastIndexByte(src[:at], '\n') + 1 }

// window returns the byte range of n lines around at, as the prior benchmark
// used it.
func window(src []byte, at, n int) (int, int) {
	lo := lineStart(src, at)
	for i := 0; i < n && lo > 0; i++ {
		lo = lineStart(src, lo-1)
	}
	hi := at
	for i := 0; i < n; i++ {
		j := bytes.IndexByte(src[hi:], '\n')
		if j < 0 {
			hi = len(src)
			break
		}
		hi += j + 1
	}
	return lo, hi
}

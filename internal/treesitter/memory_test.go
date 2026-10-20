package treesitter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/internal/treesitter"
)

// TestInstanceMemory reports the linear memory of an empty instance and of
// instances holding documents of the benchmark's sizes. It runs when
// JAMB_HIGHLIGHT_INPUTS names the benchmark inputs directory, and is skipped
// otherwise.
func TestInstanceMemory(t *testing.T) {
	dir := os.Getenv("JAMB_HIGHLIGHT_INPUTS")
	if dir == "" {
		t.Skip("set JAMB_HIGHLIGHT_INPUTS to the directory of the benchmark inputs")
	}
	mod, err := treesitter.Compile(golang.Grammar.Wasm())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := mod.Instantiate()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("empty instance: %d KiB linear memory", empty.MemorySize()/1024)
	empty.Close()
	for _, file := range []string{"go4k.go", "go110k.go"} {
		src, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		inst, err := mod.Instantiate()
		if err != nil {
			t.Fatal(err)
		}
		if err := inst.Init(src); err != nil {
			t.Fatal(err)
		}
		if err := inst.Parse(true); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s (%d KiB text): %d KiB linear memory after a full parse", file, len(src)/1024, inst.MemorySize()/1024)
		inst.Close()
	}
}

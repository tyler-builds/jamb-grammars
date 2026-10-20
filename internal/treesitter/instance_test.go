package treesitter_test

import (
	"math/rand"
	"os"
	"slices"
	"testing"

	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/internal/treesitter"
)

// TestMain keeps the tests' compiled-module cache in a temporary directory, so
// that they do not write to the user's cache.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jamb-grammars-test-cache")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"LOCALAPPDATA", "XDG_CACHE_HOME", "HOME"} {
		os.Setenv(k, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// editAlphabet is the text that random edits insert. It includes brackets,
// quotes and newlines, so that many edits leave the parser in error recovery.
const editAlphabet = "xyz _(){}[]\n\"'=;:.,+-*/<>!&|1\t#"

// randomEdit returns an insertion or deletion of text, as offsets and the new
// bytes.
func randomEdit(rng *rand.Rand, text []byte) (start, oldEnd int, ins []byte) {
	if len(text) == 0 || rng.Intn(2) == 0 {
		at := rng.Intn(len(text) + 1)
		ins = make([]byte, 1+rng.Intn(6))
		for i := range ins {
			ins[i] = editAlphabet[rng.Intn(len(editAlphabet))]
		}
		return at, at, ins
	}
	at := rng.Intn(len(text))
	return at, at + min(1+rng.Intn(8), len(text)-at), nil
}

// TestIncrementalTreeMatchesFreshParse applies random edits to a real Go source
// file and checks, after each edit, that the incrementally parsed tree equals a
// fresh parse of the same text. An edit that leaves syntax errors is undone, so
// the text stays valid and every step is compared. Incremental parsing of text
// with errors can differ from a fresh parse; the highlight session handles that
// case by parsing from scratch when idle, and is tested separately.
func TestIncrementalTreeMatchesFreshParse(t *testing.T) {
	src, err := os.ReadFile("../../treesitter/session.go")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := treesitter.Compile(golang.Grammar.Wasm())
	if err != nil {
		t.Fatal(err)
	}
	inst, err := mod.Instantiate()
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	if err := inst.Init(src); err != nil {
		t.Fatal(err)
	}
	if err := inst.Parse(true); err != nil {
		t.Fatal(err)
	}

	rng := rand.New(rand.NewSource(11))
	text := slices.Clone(src)
	// edit applies one change to the instance and to text, and reparses.
	edit := func(start, oldEnd int, ins []byte) {
		t.Helper()
		if err := inst.Edit(start, oldEnd, ins); err != nil {
			t.Fatal(err)
		}
		text = append(append(slices.Clone(text[:start]), ins...), text[oldEnd:]...)
		if err := inst.Parse(false); err != nil {
			t.Fatal(err)
		}
	}
	reverted := 0
	for step := range 400 {
		start, oldEnd, ins := randomEdit(rng, text)
		old := slices.Clone(text[start:oldEnd])
		edit(start, oldEnd, ins)
		hasErr, err := inst.TreeHasError()
		if err != nil {
			t.Fatal(err)
		}
		if hasErr {
			edit(start, start+len(ins), old)
			reverted++
		}
		inc, err := inst.TreeHash(false)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := inst.TreeHash(true)
		if err != nil {
			t.Fatal(err)
		}
		if inc != fresh {
			t.Fatalf("step %d (edit [%d, %d) with %q): incremental tree differs from a fresh parse of %d bytes", step, start, oldEnd, ins, len(text))
		}
	}
	t.Logf("%d of 400 edits left syntax errors and were undone", reverted)

	// The module's own text must match the expected text: a fresh instance
	// initialized with it must parse to the same tree.
	other, err := mod.Instantiate()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Init(text); err != nil {
		t.Fatal(err)
	}
	if err := other.Parse(true); err != nil {
		t.Fatal(err)
	}
	want, _ := other.TreeHash(false)
	got, _ := inst.TreeHash(true)
	if got != want {
		t.Fatal("module text differs from the expected text")
	}
}

package treesitter_test

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	grammars "github.com/tyler-builds/jamb-grammars"
	"github.com/tyler-builds/jamb-grammars/bash"
	"github.com/tyler-builds/jamb-grammars/c"
	"github.com/tyler-builds/jamb-grammars/cpp"
	"github.com/tyler-builds/jamb-grammars/csharp"
	"github.com/tyler-builds/jamb-grammars/css"
	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/html"
	"github.com/tyler-builds/jamb-grammars/java"
	"github.com/tyler-builds/jamb-grammars/javascript"
	"github.com/tyler-builds/jamb-grammars/json"
	"github.com/tyler-builds/jamb-grammars/markdown"
	"github.com/tyler-builds/jamb-grammars/python"
	"github.com/tyler-builds/jamb-grammars/rust"
	"github.com/tyler-builds/jamb-grammars/toml"
	"github.com/tyler-builds/jamb-grammars/treesitter"
	"github.com/tyler-builds/jamb-grammars/typescript"
	"github.com/tyler-builds/jamb-grammars/yaml"
	"github.com/tyler-builds/jamb/codeedit"
	"github.com/tyler-builds/jamb/codeedit/highlight"
)

// A Session is a highlighter for jamb's code editor.
var _ codeedit.Highlighter = (*treesitter.Session)(nil)

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

// open starts a session on text with the whole text in view, and waits for its
// first spans.
func open(t *testing.T, lang *grammars.Grammar, text []byte) *treesitter.Session {
	t.Helper()
	notify := make(chan struct{}, 1)
	s, err := treesitter.NewSession(lang, text, treesitter.Options{Notify: func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetViewport(0, len(text)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notify:
	case <-time.After(30 * time.Second):
		t.Fatal("no spans published")
	}
	return s
}

// wantCapture is a highlighted text and the capture it should carry.
type wantCapture struct{ text, capture string }

func TestSpansMatchExpectedCaptures(t *testing.T) {
	tests := []struct {
		name string
		lang *grammars.Grammar
		text string
		want []wantCapture
	}{
		{"go", golang.Grammar,
			"package main\n\n// Hi is a comment.\nfunc main() {\n\tx := \"s\" + 1\n\tprintln(x, true)\n}\n",
			[]wantCapture{
				{"package", "keyword"},
				{"// Hi is a comment.", "comment"},
				{"func", "keyword"},
				{"\"s\"", "string"},
				{"1", "number"},
				{"true", "constant.builtin"},
			}},
		{"typescript", typescript.TypeScript,
			"const n: number = 42; // note\nfunction greet(name: string) { return \"hi \" + name; }\n",
			[]wantCapture{
				{"const", "keyword"},
				{"number", "type.builtin"},
				{"42", "number"},
				{"// note", "comment"},
				{"function", "keyword"},
				{"greet", "function"},
				{"\"hi \"", "string"},
			}},
		{"python", python.Grammar,
			"def add(a, b):\n    # sum\n    return a + 1 if True else None\n",
			[]wantCapture{
				{"def", "keyword"},
				{"# sum", "comment"},
				{"return", "keyword"},
				{"1", "number"},
				{"None", "constant.builtin"},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := []byte(tt.text)
			s := open(t, tt.lang, text)
			spans, _ := s.Spans()
			names := s.CaptureNames()
			for _, w := range tt.want {
				found := false
				for _, sp := range spans {
					if string(text[sp.Start:sp.End]) == w.text && names[sp.Capture] == w.capture {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no %q capture on %q", w.capture, w.text)
				}
			}
		})
	}
}

// TestLanguagesCompile checks that every language's module and query load, and
// that each highlights a short sample without error.
func TestLanguagesCompile(t *testing.T) {
	langs := []*grammars.Grammar{
		golang.Grammar, javascript.Grammar, typescript.TypeScript, typescript.TSX,
		python.Grammar, json.Grammar, css.Grammar, html.Grammar, rust.Grammar,
		c.Grammar, cpp.Grammar, csharp.Grammar, java.Grammar, bash.Grammar,
		yaml.Grammar, toml.Grammar, markdown.Markdown, markdown.MarkdownInline,
	}
	for _, lang := range langs {
		t.Run(lang.Name(), func(t *testing.T) {
			s := open(t, lang, []byte("x\n"))
			if err := s.Err(); err != nil {
				t.Fatal(err)
			}
			if len(s.CaptureNames()) == 0 {
				t.Fatal("no capture names")
			}
		})
	}
}

func TestBadQueryIsAnError(t *testing.T) {
	lang := grammars.New("go", golang.Grammar.Wasm(), []byte("(no_such_node) @x"))
	if _, err := treesitter.NewSession(lang, []byte("package p\n"), treesitter.Options{}); err == nil {
		t.Fatal("NewSession accepted a query that does not match the grammar")
	}
	if _, err := treesitter.NewSession(grammars.New("none", nil, nil), []byte("x"), treesitter.Options{}); err == nil {
		t.Fatal("NewSession accepted a language without a module")
	}
	if _, err := treesitter.NewSession(nil, []byte("x"), treesitter.Options{}); err == nil {
		t.Fatal("NewSession accepted a nil language")
	}
}

func TestSizeLimit(t *testing.T) {
	text := bytes.Repeat([]byte("a"), 101)
	if _, err := treesitter.NewSession(golang.Grammar, text, treesitter.Options{MaxBytes: 100}); !errors.Is(err, treesitter.ErrTooLarge) {
		t.Fatalf("101 bytes with MaxBytes 100: err = %v, want ErrTooLarge", err)
	}
	big := make([]byte, treesitter.DefaultMaxBytes+1)
	if _, err := treesitter.NewSession(golang.Grammar, big, treesitter.Options{}); !errors.Is(err, treesitter.ErrTooLarge) {
		t.Fatalf("over DefaultMaxBytes: err = %v, want ErrTooLarge", err)
	}
	s := open(t, golang.Grammar, text[:100])
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestEditValidationAndClose(t *testing.T) {
	text := []byte("package p\n")
	s := open(t, golang.Grammar, text)
	if err := s.Edit(highlight.EditRecord{Start: 5, OldEnd: 50}); err == nil {
		t.Fatal("Edit accepted a range past the end of the document")
	}
	if err := s.Edit(highlight.EditRecord{Start: 3, OldEnd: 1}); err == nil {
		t.Fatal("Edit accepted a reversed range")
	}
	if err := s.Edit(highlight.EditRecord{Start: 0, OldEnd: 0, Text: []byte("// c\n")}); err != nil {
		t.Fatal(err)
	}
	// Only the valid edit counts: the published spans come to reflect one edit.
	for deadline := time.Now().Add(5 * time.Second); s.Synced() < 1 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if n := s.Synced(); n != 1 {
		t.Fatalf("the spans reflect %d edits, want 1: rejected edits were counted", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
	if err := s.Edit(highlight.EditRecord{}); !errors.Is(err, treesitter.ErrClosed) {
		t.Fatalf("Edit after Close: err = %v, want ErrClosed", err)
	}
	if err := s.SetViewport(0, 1); !errors.Is(err, treesitter.ErrClosed) {
		t.Fatalf("SetViewport after Close: err = %v, want ErrClosed", err)
	}
}

// TestConcurrentUse runs a typing goroutine, a viewport goroutine and readers at
// once. Run it with -race.
func TestConcurrentUse(t *testing.T) {
	text := bytes.Repeat([]byte("func f() { return 1 }\n"), 200)
	var notifies atomic.Int64
	s, err := treesitter.NewSession(golang.Grammar, text, treesitter.Options{Notify: func() { notifies.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { // the typing thread
		defer wg.Done()
		rng := rand.New(rand.NewSource(3))
		for range 400 {
			at := rng.Intn(len(text))
			if err := s.Edit(highlight.EditRecord{Start: at, OldEnd: at, Text: []byte("x")}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() { // scrolling
		defer wg.Done()
		for i := range 200 {
			if err := s.SetViewport(i*10, i*10+500); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() { // painting
		defer wg.Done()
		for range 2000 {
			s.Spans()
			s.Err()
		}
	}()
	wg.Wait()
	deadline := time.Now().Add(30 * time.Second)
	for notifies.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if notifies.Load() == 0 {
		t.Fatal("no notification")
	}
}

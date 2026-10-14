package grammars_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

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
	"github.com/tyler-builds/jamb-grammars/typescript"
	"github.com/tyler-builds/jamb-grammars/yaml"
)

// all lists every grammar of the repository.
var all = []*grammars.Grammar{
	bash.Grammar, c.Grammar, cpp.Grammar, csharp.Grammar, css.Grammar, golang.Grammar,
	html.Grammar, java.Grammar, javascript.Grammar, json.Grammar, markdown.Markdown,
	markdown.MarkdownInline, python.Grammar, rust.Grammar, toml.Grammar, typescript.TypeScript,
	typescript.TSX, yaml.Grammar,
}

// shimExports are the functions that shim/shim.c exports at ABI 1, which the editor
// calls. shimImports are the host functions it imports, besides WASI.
var (
	shimExports = []string{
		"buf_alloc", "buf_free", "doc_init", "doc_set_query", "doc_query_error_offset",
		"doc_capture_count", "doc_capture_name", "doc_edit", "doc_parse", "doc_spans",
		"doc_tree_has_error", "doc_tree_hash",
	}
	shimImports = []string{"jamb.regex_match"}
)

// Every grammar is a WebAssembly module that exports the shim's functions and imports
// only WASI and the editor's host functions, and has a highlight query.
func TestEveryGrammarHasTheShimInterface(t *testing.T) {
	if grammars.ABI != 1 {
		t.Fatalf("ABI is %d; update shimExports and shimImports for it", grammars.ABI)
	}
	names := map[string]bool{}
	for _, g := range all {
		if names[g.Name()] {
			t.Errorf("two grammars are named %q", g.Name())
		}
		names[g.Name()] = true
		if g.ABI() != grammars.ABI {
			t.Errorf("%s: ABI %d, want %d", g.Name(), g.ABI(), grammars.ABI)
		}
		if len(bytes.TrimSpace(g.Query())) == 0 {
			t.Errorf("%s: empty highlight query", g.Name())
		}
		exports, imports, err := moduleInterface(g.Wasm())
		if err != nil {
			t.Errorf("%s: %v", g.Name(), err)
			continue
		}
		for _, e := range shimExports {
			if !slices.Contains(exports, e) {
				t.Errorf("%s: the module does not export %s", g.Name(), e)
			}
		}
		for _, i := range imports {
			if !slices.Contains(shimImports, i) && !bytes.HasPrefix([]byte(i), []byte("wasi_snapshot_preview1.")) {
				t.Errorf("%s: the module imports %s, which the editor does not provide", g.Name(), i)
			}
		}
	}
	if len(all) != 18 {
		t.Errorf("%d grammars listed, want 18", len(all))
	}
}

// moduleInterface returns the names of the functions a WebAssembly module exports,
// and the module.name of each function it imports.
func moduleInterface(b []byte) (exports, imports []string, err error) {
	if len(b) < 8 || !bytes.Equal(b[:4], []byte("\x00asm")) || !bytes.Equal(b[4:8], []byte{1, 0, 0, 0}) {
		return nil, nil, errors.New("not a WebAssembly 1.0 module")
	}
	r := &reader{b: b[8:]}
	for len(r.b) > 0 {
		id := r.byte()
		size := r.uint()
		if r.err != nil || uint64(len(r.b)) < size {
			return nil, nil, errors.New("truncated section")
		}
		sec := &reader{b: r.b[:size]}
		r.b = r.b[size:]
		switch id {
		case 2: // imports
			for n := sec.uint(); n > 0 && sec.err == nil; n-- {
				mod, name, kind := sec.name(), sec.name(), sec.byte()
				switch kind {
				case 0: // function: a type index
					sec.uint()
					imports = append(imports, mod+"."+name)
				case 1: // table: a reference type and limits
					sec.byte()
					sec.limits()
				case 2: // memory: limits
					sec.limits()
				case 3: // global: a value type and mutability
					sec.byte()
					sec.byte()
				default:
					return nil, nil, errors.New("unknown import kind")
				}
			}
		case 7: // exports
			for n := sec.uint(); n > 0 && sec.err == nil; n-- {
				name, kind := sec.name(), sec.byte()
				sec.uint()
				if kind == 0 {
					exports = append(exports, name)
				}
			}
		}
		if sec.err != nil {
			return nil, nil, sec.err
		}
	}
	return exports, imports, r.err
}

// reader reads the encodings of a WebAssembly module.
type reader struct {
	b   []byte
	err error
}

func (r *reader) byte() byte {
	if len(r.b) == 0 {
		r.err = errors.New("unexpected end of module")
		return 0
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c
}

// uint reads an unsigned LEB128 number.
func (r *reader) uint() uint64 {
	var v uint64
	for shift := 0; shift < 64; shift += 7 {
		c := r.byte()
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v
		}
	}
	r.err = errors.New("number too long")
	return 0
}

func (r *reader) name() string {
	n := r.uint()
	if uint64(len(r.b)) < n {
		r.err = errors.New("truncated name")
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}

func (r *reader) limits() {
	if r.byte()&1 != 0 {
		r.uint()
	}
	r.uint()
}

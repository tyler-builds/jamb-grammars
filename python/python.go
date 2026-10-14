// Package python provides the Python language for jamb's code editor: the
// tree-sitter Python grammar and its highlight query, built to WebAssembly.
package python

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed python.wasm
var wasm []byte

//go:embed python.highlights.scm
var query []byte

// Grammar is Python.
var Grammar = grammars.New("python", wasm, query)

// Package c provides the C language for jamb's code editor: the tree-sitter C
// grammar and its highlight query, built to WebAssembly.
package c

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed c.wasm
var wasm []byte

//go:embed c.highlights.scm
var query []byte

// Grammar is C.
var Grammar = grammars.New("c", wasm, query)

// Package javascript provides the JavaScript language for jamb's code editor: the
// tree-sitter JavaScript grammar, with JSX, and its highlight query, built to
// WebAssembly.
package javascript

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed javascript.wasm
var wasm []byte

//go:embed javascript.highlights.scm
var query []byte

// Grammar is JavaScript.
var Grammar = grammars.New("javascript", wasm, query)

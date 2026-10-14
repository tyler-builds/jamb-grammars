// Package golang provides the Go language for jamb's code editor: the tree-sitter
// Go grammar and its highlight query, built to WebAssembly.
package golang

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed go.wasm
var wasm []byte

//go:embed go.highlights.scm
var query []byte

// Grammar is Go.
var Grammar = grammars.New("go", wasm, query)

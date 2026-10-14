// Package json provides the JSON language for jamb's code editor: the tree-sitter
// JSON grammar and its highlight query, built to WebAssembly.
package json

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed json.wasm
var wasm []byte

//go:embed json.highlights.scm
var query []byte

// Grammar is JSON.
var Grammar = grammars.New("json", wasm, query)

// Package bash provides the Bash language for jamb's code editor: the tree-sitter
// Bash grammar and its highlight query, built to WebAssembly.
package bash

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed bash.wasm
var wasm []byte

//go:embed bash.highlights.scm
var query []byte

// Grammar is Bash.
var Grammar = grammars.New("bash", wasm, query)

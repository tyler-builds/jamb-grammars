// Package yaml provides the YAML language for jamb's code editor: the tree-sitter
// YAML grammar and its highlight query, built to WebAssembly.
package yaml

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed yaml.wasm
var wasm []byte

//go:embed yaml.highlights.scm
var query []byte

// Grammar is YAML.
var Grammar = grammars.New("yaml", wasm, query)

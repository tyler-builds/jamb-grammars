// Package toml provides the TOML language for jamb's code editor: the tree-sitter
// TOML grammar and its highlight query, built to WebAssembly.
package toml

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed toml.wasm
var wasm []byte

//go:embed toml.highlights.scm
var query []byte

// Grammar is TOML.
var Grammar = grammars.New("toml", wasm, query)

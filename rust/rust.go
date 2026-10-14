// Package rust provides the Rust language for jamb's code editor: the tree-sitter
// Rust grammar and its highlight query, built to WebAssembly.
package rust

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed rust.wasm
var wasm []byte

//go:embed rust.highlights.scm
var query []byte

// Grammar is Rust.
var Grammar = grammars.New("rust", wasm, query)

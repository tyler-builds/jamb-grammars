// Package java provides the Java language for jamb's code editor: the tree-sitter
// Java grammar and its highlight query, built to WebAssembly.
package java

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed java.wasm
var wasm []byte

//go:embed java.highlights.scm
var query []byte

// Grammar is Java.
var Grammar = grammars.New("java", wasm, query)

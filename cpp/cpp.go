// Package cpp provides the C++ language for jamb's code editor: the tree-sitter C++
// grammar and its highlight query, built to WebAssembly. The query merges the C
// queries that C++ inherits.
package cpp

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed cpp.wasm
var wasm []byte

//go:embed cpp.highlights.scm
var query []byte

// Grammar is C++.
var Grammar = grammars.New("cpp", wasm, query)

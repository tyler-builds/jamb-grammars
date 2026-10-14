// Package css provides the CSS language for jamb's code editor: the tree-sitter CSS
// grammar and its highlight query, built to WebAssembly.
package css

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed css.wasm
var wasm []byte

//go:embed css.highlights.scm
var query []byte

// Grammar is CSS.
var Grammar = grammars.New("css", wasm, query)

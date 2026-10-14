// Package html provides the HTML language for jamb's code editor: the tree-sitter
// HTML grammar and its highlight query, built to WebAssembly. Embedded
// JavaScript and CSS are not highlighted by this language; their injections are
// not yet supported.
package html

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed html.wasm
var wasm []byte

//go:embed html.highlights.scm
var query []byte

// Grammar is HTML.
var Grammar = grammars.New("html", wasm, query)

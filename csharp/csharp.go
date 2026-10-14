// Package csharp provides the C# language for jamb's code editor: the tree-sitter
// C# grammar and its highlight query, built to WebAssembly.
package csharp

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed csharp.wasm
var wasm []byte

//go:embed csharp.highlights.scm
var query []byte

// Grammar is C#.
var Grammar = grammars.New("csharp", wasm, query)

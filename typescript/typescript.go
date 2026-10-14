// Package typescript provides the TypeScript and TSX languages for jamb's code
// editor. Each is a tree-sitter grammar built to WebAssembly, with a highlight
// query that merges the JavaScript queries it inherits.
package typescript

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed typescript.wasm
var typescriptWasm []byte

//go:embed typescript.highlights.scm
var typescriptQuery []byte

//go:embed tsx.wasm
var tsxWasm []byte

//go:embed tsx.highlights.scm
var tsxQuery []byte

// TypeScript is the TypeScript language, without JSX.
var TypeScript = grammars.New("typescript", typescriptWasm, typescriptQuery)

// TSX is TypeScript with JSX.
var TSX = grammars.New("tsx", tsxWasm, tsxQuery)

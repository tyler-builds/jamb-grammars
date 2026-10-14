// Package markdown provides the Markdown languages for jamb's code editor: the
// tree-sitter block grammar (Markdown) and the inline grammar (MarkdownInline),
// each built to WebAssembly with its highlight query. Injections between the
// two grammars are not yet supported, so inline text is highlighted only by
// MarkdownInline.
package markdown

import (
	_ "embed"

	grammars "github.com/tyler-builds/jamb-grammars"
)

//go:embed markdown.wasm
var markdownWasm []byte

//go:embed markdown.highlights.scm
var markdownQuery []byte

//go:embed markdown_inline.wasm
var inlineWasm []byte

//go:embed markdown_inline.highlights.scm
var inlineQuery []byte

// Markdown is the Markdown block grammar.
var Markdown = grammars.New("markdown", markdownWasm, markdownQuery)

// MarkdownInline is the Markdown inline grammar, for text within blocks.
var MarkdownInline = grammars.New("markdown_inline", inlineWasm, inlineQuery)

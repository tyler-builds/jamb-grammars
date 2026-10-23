# jamb-grammars

Tree-sitter grammars compiled to WebAssembly, and a tree-sitter highlighter for the code editor
of [jamb](https://github.com/tyler-builds/jamb) (`github.com/tyler-builds/jamb/codeedit`).

It requires jamb v0.4.0 or later, the release that moved this highlighter out of jamb.

jamb's editor has a built-in highlighter, `@pierre/highlights`, that covers 76 languages and needs
no grammars:

```go
codeedit.Editor(doc).Language("go")
```

This repository is for apps that want highlighting from a real syntax tree, with the accuracy of a
parse: one package per tree-sitter grammar, and the `treesitter` package, a highlighter that runs
those grammars and plugs into the editor with `Highlighter`. The grammars live apart from jamb so
that jamb stays small: an app that does not import them downloads none of them, and an app that
does links only the grammars it imports.

## The tree-sitter highlighter

`treesitter.NewSession` opens a document with a grammar, and the `*treesitter.Session` it returns
satisfies `codeedit.Highlighter`. The session parses in a worker goroutine: the editor sends it
each change and the visible rows, and paints with the spans it has, so a keystroke never waits for
a parse.

The editor neither starts nor closes a highlighter that the app gives it. The app makes the
session from the document's text, closes it when the editor goes away, and makes a new one when
the editor shows another document. `Options.Notify` runs on the session's worker each time new
spans are ready; post to the UI thread there and render again, or the new colors wait for the
next frame that paints. Give each editor its own session, because every editor sends the changes
it sees to its highlighter. In a jamb component:

```go
import (
	"github.com/tyler-builds/jamb-grammars/golang"
	"github.com/tyler-builds/jamb-grammars/treesitter"
	"github.com/tyler-builds/jamb/codeedit"
	"github.com/tyler-builds/jamb/ui"
)

func codeView(doc *codeedit.Document) ui.Element {
	return ui.Component(func(ctx *ui.Context) ui.Element {
		hl := ui.Local(ctx, "highlighter", &highlighter{}).Peek()
		if hl.s == nil && hl.err == nil {
			hl.s, hl.err = treesitter.NewSession(golang.Grammar, []byte(doc.Text()), treesitter.Options{
				// Notify runs on the session's worker: render again on the UI thread.
				Notify: func() { ctx.Post(ctx.Invalidate) },
			})
			if hl.err == nil {
				ctx.OnDispose(func() { hl.s.Close() })
			}
		}
		ed := codeedit.Editor(doc)
		if hl.s != nil { // nil after an error, such as ErrTooLarge: plain text
			ed.Highlighter(hl.s)
		}
		return ed
	})
}

// highlighter is the session of one editor, or why it could not start.
type highlighter struct {
	s   *treesitter.Session
	err error
}
```

A document over `Options.MaxBytes` (5 MB by default) gets `treesitter.ErrTooLarge`. The session
compiles each module once per process and keeps the compiled code in `jamb-grammars/wasm` under
the user's cache directory (`os.UserCacheDir`), or in memory when that directory cannot be used.
When the highlighter was part of jamb it shared jamb's cache, `jamb/wasm`; it now has its own, so
the first run after the move compiles the modules again.

## Grammars

| Package | Grammars | Module size |
|---|---|---|
| `bash` | `Grammar` | 1.47 MB |
| `c` | `Grammar` | 0.74 MB |
| `cpp` | `Grammar` (merges C's highlight query) | 3.43 MB |
| `csharp` | `Grammar` | 5.23 MB |
| `css` | `Grammar` | 0.28 MB |
| `golang` | `Grammar` | 0.34 MB |
| `html` | `Grammar` | 0.16 MB |
| `java` | `Grammar` | 0.53 MB |
| `javascript` | `Grammar` | 0.53 MB |
| `json` | `Grammar` | 0.14 MB |
| `markdown` | `Markdown`, `MarkdownInline` | 0.54 MB each |
| `python` | `Grammar` | 0.57 MB |
| `rust` | `Grammar` | 1.20 MB |
| `toml` | `Grammar` | 0.16 MB |
| `typescript` | `TypeScript`, `TSX` (both merge JavaScript's highlight query) | 1.48 MB, 1.51 MB |
| `yaml` | `Grammar` | 0.32 MB |

The size is what the grammar adds to an app's binary. Each module holds the tree-sitter runtime,
one grammar with its scanner, and the shim (about 144 KB of each module is the runtime and the
shim).

## The module interface and its version

A module is the tree-sitter runtime, a grammar, and `shim/shim.c`, which is what the highlighter
calls: it exports `doc_init`, `doc_edit`, `doc_parse`, `doc_spans` and the other functions listed
in `grammars_test.go`, and imports one host function, `jamb.regex_match`, for the `#match?`
predicate. `grammars.ABI` is the version of that interface. The modules and the `treesitter`
package that calls them are in this one module, so they always match; a change to the shim that
the highlighter must follow raises `ABI` and changes `treesitter` with it.

## Building the modules

The modules are committed, so building them is needed only when a pin, the shim or the build
flags change:

```
go generate .
```

This runs `internal/grammarbuild`, which needs git, zig 0.14 or later, and network access to
GitHub. It clones the tree-sitter runtime and each grammar at the commits pinned in
`internal/grammarbuild/main.go` into a temporary directory, compiles each module with zig
(`-target wasm32-wasi -Os -flto -mbulk-memory`, reactor model), and writes each module and its
merged highlight query into its package. Pass `-zig PATH` if zig is not on the PATH, and
`-only golang,python` to build some packages.

`go test ./...` checks every module: that it is a WebAssembly module, that it exports the shim's
functions and imports only WASI and the editor's host function, and that it has a highlight
query. The tests of `treesitter` highlight real text with these grammars and check the spans
against fresh parses, and wire a session to a headless editor.

## License

The code of this repository is licensed under the Apache License, Version 2.0 (see LICENSE). The
tree-sitter runtime and the grammars that the modules embed are licensed under the MIT License;
see NOTICE for their copyright notices. The `treesitter` package runs the modules with
[wazero](https://github.com/tetratelabs/wazero), which is licensed under the Apache License,
Version 2.0.

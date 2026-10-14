# jamb-grammars

The tree-sitter grammars of jamb's code editor
([github.com/tyler-builds/jamb/codeedit](https://github.com/tyler-builds/jamb)), compiled to
WebAssembly. They live in their own repository so that jamb stays small: an app that does not
use the editor downloads none of them, and an app that does links only the grammars it imports.

```go
import (
	"github.com/tyler-builds/jamb/codeedit"
	"github.com/tyler-builds/jamb-grammars/golang"
)

editor := codeedit.New(codeedit.NewDocument(source)).Language(golang.Grammar)
```

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

## The editor interface and its version

A module is the tree-sitter runtime, a grammar, and `shim/shim.c`, which is what the editor calls:
it exports `doc_init`, `doc_edit`, `doc_parse`, `doc_spans` and the other functions listed in
`grammars_test.go`, and imports one host function, `jamb.regex_match`, for the `#match?`
predicate. `grammars.ABI` is the version of that interface. jamb's `codeedit/highlight` checks
it and refuses a grammar built for an interface it does not support, with an error that names
both versions. A change to the shim that the editor must follow raises `ABI`, and needs a jamb
release that supports it.

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
query. jamb's own tests highlight real files with these grammars.

## License

The code of this repository is licensed under the Apache License, Version 2.0 (see LICENSE). The
tree-sitter runtime and the grammars that the modules embed are licensed under the MIT License;
see NOTICE for their copyright notices.

// Package treesitter runs the tree-sitter language modules of this repository in
// WebAssembly with wazero. A module is the tree-sitter runtime, one grammar and
// the shim in shim/shim.c, built by internal/grammarbuild; grammars.ABI is the
// version of the shim's interface that this package calls. The package compiles
// each module once per process, keeps the compiled code in a per-user cache
// directory, and creates an Instance for each document. An Instance holds one
// document: its text, parser, tree and highlight query. Instances are not safe
// for concurrent use, so each one has a single owner that calls it from one
// goroutine.
package treesitter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// cacheSubdir is the directory, under os.UserCacheDir, that holds the compiled
// modules. It is this module's own, apart from jamb's cache.
var cacheSubdir = filepath.Join("jamb-grammars", "wasm")

// mu guards the process's runtime and its compiled modules, and every call that
// instantiates or closes a module. Calls into a running instance do not take it.
var (
	mu       sync.Mutex
	rt       wazero.Runtime
	compiled = map[blobKey]*Module{}
)

// blobKey identifies an embedded module by the address and length of its bytes.
// Embedded blobs do not move, so each one compiles once.
type blobKey struct {
	data *byte
	n    int
}

// runtimeLocked returns the process's runtime, creating it on first use. The
// caller holds mu.
func runtimeLocked() (wazero.Runtime, error) {
	if rt == nil {
		r, err := newRuntime(defaultCache())
		if err != nil {
			return nil, err
		}
		rt = r
	}
	return rt, nil
}

// newRuntime returns a runtime with the WASI functions and the host functions
// that the modules import. A nil cache leaves the runtime to compile every
// module from source.
func newRuntime(cache wazero.CompilationCache) (wazero.Runtime, error) {
	cfg := wazero.NewRuntimeConfig()
	if cache != nil {
		cfg = cfg.WithCompilationCache(cache)
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, cfg)
	if _, err := r.NewHostModuleBuilder("jamb").NewFunctionBuilder().WithFunc(regexMatch).Export("regex_match").Instantiate(ctx); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("treesitter: host functions: %w", err)
	}
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return r, nil
}

// defaultCache returns the process's compilation cache: cacheSubdir under the
// user's cache directory, or memory when that directory is not available.
func defaultCache() wazero.CompilationCache {
	base, err := os.UserCacheDir()
	if err != nil {
		return wazero.NewCompilationCache()
	}
	return openCache(filepath.Join(base, cacheSubdir))
}

// openCache returns a compilation cache in dir, creating dir if needed. It
// falls back to an in-memory cache when dir cannot be used, so an unwritable
// cache never prevents highlighting. Nothing is written outside dir.
func openCache(dir string) wazero.CompilationCache {
	if err := os.MkdirAll(dir, 0o755); err == nil {
		if c, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
			return c
		}
	}
	return wazero.NewCompilationCache()
}

// Module is a compiled language module. Its instances share the compiled code.
type Module struct {
	compiled wazero.CompiledModule
}

// Compile returns the compiled form of wasm, compiling it on first use. wasm
// must be a module built as described in the package comment, and must not be
// changed afterwards. The result is cached per process, keyed by the address of
// wasm's bytes.
func Compile(wasm []byte) (*Module, error) {
	if len(wasm) == 0 {
		return nil, errors.New("treesitter: empty module")
	}
	key := blobKey{&wasm[0], len(wasm)}
	mu.Lock()
	defer mu.Unlock()
	if m := compiled[key]; m != nil {
		return m, nil
	}
	r, err := runtimeLocked()
	if err != nil {
		return nil, err
	}
	cm, err := r.CompileModule(context.Background(), wasm)
	if err != nil {
		return nil, fmt.Errorf("treesitter: compile module: %w", err)
	}
	m := &Module{cm}
	compiled[key] = m
	return m, nil
}

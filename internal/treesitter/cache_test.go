package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tyler-builds/jamb-grammars/golang"
)

func TestOpenCacheWritesOnlyUnderItsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	r, err := newRuntime(openCache(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	wasm := golang.Grammar.Wasm()
	if _, err := r.CompileModule(context.Background(), wasm); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("compiling a module wrote nothing to the cache directory")
	}
}

func TestOpenCacheFallsBackToMemory(t *testing.T) {
	// A regular file where the cache directory should be makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := openCache(filepath.Join(blocker, "cache"))
	if c == nil {
		t.Fatal("openCache returned nil")
	}
	if _, err := os.Stat(filepath.Join(blocker, "cache")); err == nil {
		t.Fatal("openCache created a directory under a file")
	}
	r, err := newRuntime(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	wasm := golang.Grammar.Wasm()
	if _, err := r.CompileModule(context.Background(), wasm); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultCacheIsUnderUserCacheDir(t *testing.T) {
	base := t.TempDir()
	// os.UserCacheDir reads these on each platform; point all of them at the
	// temporary directory so that the test does not touch the real cache.
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv("HOME", base)
	if defaultCache() == nil {
		t.Fatal("defaultCache returned nil")
	}
	dir := filepath.Join(base, cacheSubdir)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("defaultCache did not create %s: %v", dir, err)
	}
}

// Command grammarbuild clones the pinned tree-sitter runtime and grammars,
// compiles each language module with zig to WebAssembly (wasm32-wasi, reactor
// model), and writes the modules and their highlight queries into the language
// packages of this repository. go generate runs it from the repository root:
//
//	go generate .
//
// It needs git, zig 0.14 or later, and network access to GitHub. The outputs
// are committed, so the build runs only when a pin changes. Every module is the
// tree-sitter runtime, the shim, one grammar and its scanner, built with the
// same flags: -Os -flto -mbulk-memory.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repo is a git repository pinned to one commit. Tag records the release the
// commit was resolved from.
type repo struct {
	url, commit, tag string
}

// grammars are the pinned repositories, keyed by the short name that modules
// refer to.
var grammars = map[string]repo{
	"tree-sitter":    {"https://github.com/tree-sitter/tree-sitter", "10eb5ac119eec9cb4267a8e035066473983f5f3a", "v0.27.1"},
	"go":             {"https://github.com/tree-sitter/tree-sitter-go", "1547678a9da59885853f5f5cc8a99cc203fa2e2c", "v0.25.0"},
	"javascript":     {"https://github.com/tree-sitter/tree-sitter-javascript", "44c892e0be055ac465d5eeddae6d3e194424e7de", "v0.25.0"},
	"typescript":     {"https://github.com/tree-sitter/tree-sitter-typescript", "f975a621f4e7f532fe322e13c4f79495e0a7b2e7", "v0.23.2"},
	"python":         {"https://github.com/tree-sitter/tree-sitter-python", "293fdc02038ee2bf0e2e206711b69c90ac0d413f", "v0.25.0"},
	"json":           {"https://github.com/tree-sitter/tree-sitter-json", "ee35a6ebefcef0c5c416c0d1ccec7370cfca5a24", "v0.24.8"},
	"css":            {"https://github.com/tree-sitter/tree-sitter-css", "dda5cfc5722c429eaba1c910ca32c2c0c5bb1a3f", "v0.25.0"},
	"html":           {"https://github.com/tree-sitter/tree-sitter-html", "5a5ca8551a179998360b4a4ca2c0f366a35acc03", "v0.23.2"},
	"rust":           {"https://github.com/tree-sitter/tree-sitter-rust", "77a3747266f4d621d0757825e6b11edcbf991ca5", "v0.24.2"},
	"c":              {"https://github.com/tree-sitter/tree-sitter-c", "b780e47fc780ddc8da13afa35a3f4ed5c157823d", "v0.24.2"},
	"cpp":            {"https://github.com/tree-sitter/tree-sitter-cpp", "f41e1a044c8a84ea9fa8577fdd2eab92ec96de02", "v0.23.4"},
	"c-sharp":        {"https://github.com/tree-sitter/tree-sitter-c-sharp", "cac6d5fb595f5811a076336682d5d595ac1c9e85", "v0.23.5"},
	"java":           {"https://github.com/tree-sitter/tree-sitter-java", "94703d5a6bed02b98e438d7cad1136c01a60ba2c", "v0.23.5"},
	"bash":           {"https://github.com/tree-sitter/tree-sitter-bash", "a06c2e4415e9bc0346c6b86d401879ffb44058f7", "v0.25.1"},
	"yaml":           {"https://github.com/tree-sitter-grammars/tree-sitter-yaml", "7708026449bed86239b1cd5bce6e3c34dbca6415", "v0.7.2"},
	"toml":           {"https://github.com/tree-sitter-grammars/tree-sitter-toml", "64b56832c2cffe41758f28e05c756a3a98d16f41", "v0.7.0"},
	"tree-sitter-md": {"https://github.com/tree-sitter-grammars/tree-sitter-markdown", "f969cd3ae3f9fbd4e43205431d0ae286014c05b5", "v0.5.3"},
}

// module is one language module: a tree-sitter grammar with its scanner, the
// runtime and the shim, and the highlight query that goes with it. The module
// is written to <pkg>/<file>.wasm and its query to
// <pkg>/<file>.highlights.scm.
type module struct {
	pkg, file string
	fn        string   // the grammar's language function, such as tree_sitter_go
	repo      string   // key into grammars
	src       string   // the grammar's src directory, relative to the repo root
	queries   []string // highlight query files, concatenated in order; "repo:path" names another pinned repository, otherwise the module's own
}

// modules are the v1 language modules. A package with two modules (typescript
// and markdown) exposes both languages.
var modules = []module{
	{"golang", "go", "tree_sitter_go", "go", "src", []string{"queries/highlights.scm"}},
	{"javascript", "javascript", "tree_sitter_javascript", "javascript", "src", []string{"queries/highlights.scm"}},
	{"typescript", "typescript", "tree_sitter_typescript", "typescript", "typescript/src", []string{"javascript:queries/highlights.scm", "typescript:queries/highlights.scm"}},
	{"typescript", "tsx", "tree_sitter_tsx", "typescript", "tsx/src", []string{"javascript:queries/highlights.scm", "javascript:queries/highlights-jsx.scm", "typescript:queries/highlights.scm"}},
	{"python", "python", "tree_sitter_python", "python", "src", []string{"queries/highlights.scm"}},
	{"json", "json", "tree_sitter_json", "json", "src", []string{"queries/highlights.scm"}},
	{"css", "css", "tree_sitter_css", "css", "src", []string{"queries/highlights.scm"}},
	{"html", "html", "tree_sitter_html", "html", "src", []string{"queries/highlights.scm"}},
	{"rust", "rust", "tree_sitter_rust", "rust", "src", []string{"queries/highlights.scm"}},
	{"c", "c", "tree_sitter_c", "c", "src", []string{"queries/highlights.scm"}},
	{"cpp", "cpp", "tree_sitter_cpp", "cpp", "src", []string{"c:queries/highlights.scm", "cpp:queries/highlights.scm"}},
	{"csharp", "csharp", "tree_sitter_c_sharp", "c-sharp", "src", []string{"queries/highlights.scm"}},
	{"java", "java", "tree_sitter_java", "java", "src", []string{"queries/highlights.scm"}},
	{"bash", "bash", "tree_sitter_bash", "bash", "src", []string{"queries/highlights.scm"}},
	{"yaml", "yaml", "tree_sitter_yaml", "yaml", "src", []string{"queries/highlights.scm"}},
	{"toml", "toml", "tree_sitter_toml", "toml", "src", []string{"queries/highlights.scm"}},
	{"markdown", "markdown", "tree_sitter_markdown", "tree-sitter-md", "tree-sitter-markdown/src", []string{"tree-sitter-markdown/queries/highlights.scm"}},
	{"markdown", "markdown_inline", "tree_sitter_markdown_inline", "tree-sitter-md", "tree-sitter-markdown-inline/src", []string{"tree-sitter-markdown-inline/queries/highlights.scm"}},
}

// flags for the build. The defaults assume the working directory is the repository root.
var (
	workDir  = flag.String("work", filepath.Join(os.TempDir(), "jamb-grammars"), "directory for the clones and object files")
	zigPath  = flag.String("zig", "zig", "path to the zig executable")
	langDir  = flag.String("lang", ".", "directory of the language packages")
	shimPath = flag.String("shim", filepath.Join("shim", "shim.c"), "path to the shim source")
	only     = flag.String("only", "", "comma-separated package names to build; empty builds all")
)

const cflags = "-target wasm32-wasi -Os -flto -mbulk-memory"

func main() {
	log.SetFlags(0)
	flag.Parse()
	want := map[string]bool{}
	for _, p := range strings.Split(*only, ",") {
		if p != "" {
			want[p] = true
		}
	}
	rt, err := fetch(*workDir, "tree-sitter", grammars["tree-sitter"])
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range modules {
		if len(want) > 0 && !want[m.pkg] {
			continue
		}
		if err := build(rt, m); err != nil {
			log.Fatalf("%s/%s: %v", m.pkg, m.file, err)
		}
	}
}

// fetch clones the repository into work and checks out the pinned commit. A
// commit that does not match the pin is an error, so a moved tag cannot change
// the build.
func fetch(work, name string, r repo) (string, error) {
	dir := filepath.Join(work, "src", name)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if _, err := git(dir, "init", "-q"); err != nil {
			return "", err
		}
		if _, err := git(dir, "remote", "add", "origin", r.url); err != nil {
			return "", err
		}
	}
	if _, err := git(dir, "fetch", "-q", "--depth", "1", "origin", r.commit); err != nil {
		return "", fmt.Errorf("fetch %s %s: %w", name, r.commit, err)
	}
	if _, err := git(dir, "-c", "advice.detachedHead=false", "checkout", "-q", "--detach", r.commit); err != nil {
		return "", err
	}
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(head) != r.commit {
		return "", fmt.Errorf("%s: checked out %s, want %s", name, strings.TrimSpace(head), r.commit)
	}
	return dir, nil
}

// unit is one C source file of a module, compiled to the object file out.
type unit struct {
	out, in string
	flags   []string
}

// build compiles one module and writes it and its query into the package.
func build(rt string, m module) error {
	g := grammars[m.repo]
	dir, err := fetch(*workDir, m.repo, g)
	if err != nil {
		return err
	}
	obj := filepath.Join(*workDir, "obj", m.pkg+"_"+m.file)
	if err := os.MkdirAll(obj, 0o755); err != nil {
		return err
	}
	src := filepath.Join(dir, m.src)
	units := []unit{
		{"runtime.o", filepath.Join(rt, "lib", "src", "lib.c"), []string{"-I", filepath.Join(rt, "lib", "include"), "-I", filepath.Join(rt, "lib", "src")}},
		{"shim.o", *shimPath, []string{"-I", filepath.Join(rt, "lib", "include"), "-DTS_LANG_FN=" + m.fn}},
		{"parser.o", filepath.Join(src, "parser.c"), []string{"-I", src}},
	}
	if _, err := os.Stat(filepath.Join(src, "scanner.c")); err == nil {
		units = append(units, unit{"scanner.o", filepath.Join(src, "scanner.c"), []string{"-I", src}})
	}
	var objs []string
	for _, u := range units {
		o := filepath.Join(obj, u.out)
		args := append([]string{"cc"}, strings.Fields(cflags)...)
		args = append(args, "-c")
		args = append(args, u.flags...)
		args = append(args, u.in, "-o", o)
		if err := zig(args...); err != nil {
			return err
		}
		objs = append(objs, o)
	}
	wasm := filepath.Join(*langDir, m.pkg, m.file+".wasm")
	link := append([]string{"cc"}, strings.Fields(cflags)...)
	// The stack is 1 MiB instead of the linker's default, which makes every
	// instance reserve 16 MiB of memory before it parses anything.
	link = append(link, "-mexec-model=reactor", "-Wl,-z,stack-size=1048576")
	link = append(link, objs...)
	link = append(link, "-o", wasm)
	if err := os.MkdirAll(filepath.Dir(wasm), 0o755); err != nil {
		return err
	}
	if err := zig(link...); err != nil {
		return err
	}
	var q bytes.Buffer
	for _, f := range m.queries {
		key, path, ok := strings.Cut(f, ":")
		if !ok {
			key, path = m.repo, f
		}
		qdir, err := fetch(*workDir, key, grammars[key])
		if err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(qdir, path))
		if err != nil {
			return err
		}
		q.Write(b)
		if !bytes.HasSuffix(b, []byte("\n")) {
			q.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(*langDir, m.pkg, m.file+".highlights.scm"), q.Bytes(), 0o644); err != nil {
		return err
	}
	st, err := os.Stat(wasm)
	if err != nil {
		return err
	}
	log.Printf("%s/%s: %d bytes wasm, %d bytes query (%s %s)", m.pkg, m.file, st.Size(), q.Len(), m.repo, g.commit[:12])
	return nil
}

// zig runs the zig executable with args and returns its combined output in
// the error.
func zig(args ...string) error {
	out, err := exec.Command(*zigPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zig %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// git runs git in dir and returns its standard output.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

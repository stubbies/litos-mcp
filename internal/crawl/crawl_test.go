package crawl_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbies/litos-mcp/internal/crawl"
)

func TestIsAllowedExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".go", true},
		{".GO", true},
		{".tsx", true},
		{".png", false},
		{"", false},
		{".md", false},
	}
	for _, tc := range tests {
		if got := crawl.IsAllowedExtension(tc.ext); got != tc.want {
			t.Errorf("IsAllowedExtension(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}

func TestIsDefaultSkippedDir(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"node_modules/pkg", true},
		{"vendor/lib", true},
		{".git/objects", true},
		{"src/pkg", false},
		{"build/out", true},
	}
	for _, tc := range tests {
		if got := crawl.IsDefaultSkippedDir(tc.path); got != tc.want {
			t.Errorf("IsDefaultSkippedDir(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIgnoreRules_GitignoreAndExclude(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(root, ".git", "info", "exclude"), "secret.go\n")

	rules, err := crawl.NewIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := rules.EnterDir(root); err != nil {
		t.Fatal(err)
	}

	if !rulesSkipFile(t, rules, root, "debug.log") {
		t.Fatal("expected *.log to be ignored")
	}
	if !rulesSkipFile(t, rules, root, "secret.go") {
		t.Fatal("expected secret.go from exclude to be ignored")
	}
	if rulesSkipFile(t, rules, root, "main.go") {
		t.Fatal("expected main.go to be allowed by ignore rules")
	}
}

func TestIgnoreRules_NestedGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gitignore"), "*.gen.go\n")
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, ".gitignore"), "!keep.gen.go\n")

	rules, err := crawl.NewIgnoreRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := rules.EnterDir(root); err != nil {
		t.Fatal(err)
	}
	if err := rules.EnterDir(sub); err != nil {
		t.Fatal(err)
	}

	if !rulesSkipFile(t, rules, root, "root.gen.go") {
		t.Fatal("root.gen.go should be ignored by root *.gen.go")
	}
	if !rulesSkipFile(t, rules, root, "pkg/other.gen.go") {
		t.Fatal("expected nested *.gen.go to be ignored")
	}
	if rulesSkipFile(t, rules, root, "pkg/keep.gen.go") {
		t.Fatal("expected nested negation to allow keep.gen.go")
	}
}

func TestCrawl_SkipDefaultsAndAllowlist(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "image.png"), "binary")
	writeFile(t, filepath.Join(root, "readme.md"), "# docs")
	writeFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")

	writeFile(t, filepath.Join(root, "ignored.go"), "package ignored\n")
	writeFile(t, filepath.Join(root, "node_modules", "dep", "index.js"), "module.exports = {}\n")
	writeFile(t, filepath.Join(root, "vendor", "lib.go"), "package vendor\n")

	files, err := crawl.Crawl(context.Background(), root, crawl.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	paths := filePaths(files)
	want := []string{"main.go"}
	if !slices.Equal(paths, want) {
		t.Fatalf("Crawl() paths = %v, want %v", paths, want)
	}
}

func TestCrawl_MixedExtensions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "b.ts"), "export {}\n")
	writeFile(t, filepath.Join(root, "c.py"), "pass\n")
	writeFile(t, filepath.Join(root, "d.bin"), "\x00")

	files, err := crawl.Crawl(context.Background(), root, crawl.Options{})
	if err != nil {
		t.Fatal(err)
	}

	paths := filePaths(files)
	want := []string{"a.go", "b.ts", "c.py"}
	if !slices.Equal(paths, want) {
		t.Fatalf("Crawl() paths = %v, want %v", paths, want)
	}
}

func TestCrawl_RecordsMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.go")
	content := []byte("package sample\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	files, err := crawl.Crawl(context.Background(), root, crawl.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	if files[0].Path != "sample.go" {
		t.Fatalf("Path = %q, want sample.go", files[0].Path)
	}
	if files[0].Size != int64(len(content)) {
		t.Fatalf("Size = %d, want %d", files[0].Size, len(content))
	}
	if files[0].MtimeNs != info.ModTime().UnixNano() {
		t.Fatalf("MtimeNs = %d, want %d", files[0].MtimeNs, info.ModTime().UnixNano())
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func filePaths(files []crawl.File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func rulesSkipFile(t *testing.T, rules *crawl.IgnoreRules, root, rel string) bool {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	return rules.SkipFile(rel, abs)
}

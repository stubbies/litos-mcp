package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/store"
)

func TestNormalizeTagPath_relative(t *testing.T) {
	root := t.TempDir()
	rel, err := normalizeTagPath(root, "src/foo.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "src/foo.go" {
		t.Fatalf("got %q want src/foo.go", rel)
	}
}

func TestNormalizeTagPath_absolute(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "pkg", "bar.go")
	rel, err := normalizeTagPath(root, abs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "pkg/bar.go" {
		t.Fatalf("got %q want pkg/bar.go", rel)
	}
}

func TestNormalizeTagPath_outsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "escape.go")
	_, err := normalizeTagPath(root, outside)
	if err == nil {
		t.Fatal("expected error for path outside repo root")
	}
}

func TestBatchPaths(t *testing.T) {
	paths := make([]string, 450)
	for i := range paths {
		paths[i] = filepath.Join("f", string(rune('a'+i%26))+".go")
	}
	batches := batchPaths(paths)
	if len(batches) != 3 {
		t.Fatalf("got %d batches want 3", len(batches))
	}
	if len(batches[0]) != 200 || len(batches[1]) != 200 || len(batches[2]) != 50 {
		t.Fatalf("batch sizes: %d, %d, %d", len(batches[0]), len(batches[1]), len(batches[2]))
	}
}

func TestDeriveEndLines_nextSymbol(t *testing.T) {
	symbols := []store.SymbolRecord{
		{Name: "A", FilePath: "a.go", StartLine: 1, EndLine: 0},
		{Name: "B", FilePath: "a.go", StartLine: 10, EndLine: 0},
		{Name: "C", FilePath: "a.go", StartLine: 20, EndLine: 0},
	}
	out := deriveEndLines(symbols, map[string]int{"a.go": 30})
	if out[0].EndLine != 9 {
		t.Fatalf("A end_line=%d want 9", out[0].EndLine)
	}
	if out[1].EndLine != 19 {
		t.Fatalf("B end_line=%d want 19", out[1].EndLine)
	}
	if out[2].EndLine != 30 {
		t.Fatalf("C end_line=%d want 30", out[2].EndLine)
	}
}

func TestDeriveEndLines_preservesCtagsEnd(t *testing.T) {
	symbols := []store.SymbolRecord{
		{Name: "A", FilePath: "a.go", StartLine: 1, EndLine: 5},
		{Name: "B", FilePath: "a.go", StartLine: 10, EndLine: 0},
	}
	out := deriveEndLines(symbols, map[string]int{"a.go": 20})
	if out[0].EndLine != 5 {
		t.Fatalf("A end_line=%d want 5 (ctags provided)", out[0].EndLine)
	}
	if out[1].EndLine != 20 {
		t.Fatalf("B end_line=%d want 20", out[1].EndLine)
	}
}

func TestDeriveEndLines_singleLineFallback(t *testing.T) {
	symbols := []store.SymbolRecord{
		{Name: "Only", FilePath: "solo.go", StartLine: 7, EndLine: 0},
	}
	out := deriveEndLines(symbols, nil)
	if out[0].EndLine != 7 {
		t.Fatalf("end_line=%d want 7 (start_line fallback)", out[0].EndLine)
	}
}

func TestNewExtractor(t *testing.T) {
	ext := NewExtractor()
	if ext.Name() != "regex" && ext.Name() != "ctags" {
		t.Fatalf("unexpected extractor name: %q", ext.Name())
	}
}

func TestParseCtagsJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	data := []byte(`{"_type":"tag","name":"Foo","path":"src/a.go","line":10,"kind":"function","end":15,"scope":"pkg"}
{"_type":"ptag","name":"JSON_OUTPUT_VERSION","path":".","pattern":"/^0\\.0$/"}
{"_type":"tag","name":"Bar","path":"` + filepath.Join(root, "src", "b.go") + `","line":3,"K":"function"}
`)

	symbols, err := parseCtagsJSON(root, data)
	if err != nil {
		t.Fatalf("parseCtagsJSON: %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("got %d symbols want 2", len(symbols))
	}
	if symbols[0].Name != "Foo" || symbols[0].FilePath != "src/a.go" || symbols[0].EndLine != 15 {
		t.Fatalf("unexpected first symbol: %+v", symbols[0])
	}
	if symbols[1].Name != "Bar" || symbols[1].FilePath != "src/b.go" {
		t.Fatalf("unexpected second symbol: %+v", symbols[1])
	}
}

func TestCtagsExtractor_batching(t *testing.T) {
	if !CtagsAvailable() {
		t.Skip("ctags not installed")
	}

	root := t.TempDir()
	paths := make([]string, 205)
	for i := range paths {
		name := filepath.Join("batch", "f"+string(rune('a'+i%26))+".go")
		paths[i] = filepath.ToSlash(name)
		abs := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "package batch\n\nfunc Symbol" + string(rune('A'+i%26)) + "() {}\n"
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ext := NewCtagsExtractor(CtagsCommand())
	symbols, err := ext.Extract(context.Background(), root, paths)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) < 205 {
		t.Fatalf("got %d symbols want at least 205", len(symbols))
	}
	for _, sym := range symbols {
		if sym.EndLine < sym.StartLine {
			t.Fatalf("invalid range for %s: %d-%d", sym.Name, sym.StartLine, sym.EndLine)
		}
	}
}

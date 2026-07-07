package read_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSymbol_DelegatesToReadLines(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "billing.go"), strings.Join([]string{
		"package billing",
		"",
		"func ProcessPayment() {",
		"	return nil",
		"}",
	}, "\n"))

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	sym := store.SymbolRecord{
		FilePath:  "src/billing.go",
		Kind:      "function",
		Name:      "ProcessPayment",
		StartLine: 3,
		EndLine:   5,
	}

	got, err := r.ReadSymbol(sym)
	if err != nil {
		t.Fatal(err)
	}

	want := "3\tfunc ProcessPayment() {\n4\t\treturn nil\n5\t}"
	if got != want {
		t.Fatalf("ReadSymbol() = %q, want %q", got, want)
	}
}

func TestReadSymbol_ByteRangeExcludesSibling(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"package billing",
		"",
		"func ProcessPayment() {",
		"	return nil",
		"}",
		"",
		"func RefundPayment() {",
		"	return nil",
		"}",
	}, "\n")
	writeFile(t, filepath.Join(root, "src", "billing.go"), content)

	startByte := strings.Index(content, "func ProcessPayment")
	endByte := strings.Index(content, "\n\nfunc RefundPayment")
	if startByte < 0 || endByte < 0 || endByte <= startByte {
		t.Fatal("failed to locate function byte boundaries in fixture content")
	}

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	sym := store.SymbolRecord{
		FilePath:  "src/billing.go",
		Kind:      "function",
		Name:      "ProcessPayment",
		StartLine: 3,
		EndLine:   7,
		StartByte: startByte,
		EndByte:   endByte,
	}

	got, err := r.ReadSymbol(sym)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "RefundPayment") {
		t.Fatalf("ReadSymbol() leaked sibling symbol: %q", got)
	}
	if !strings.Contains(got, "func ProcessPayment() {") {
		t.Fatalf("ReadSymbol() = %q, want ProcessPayment body", got)
	}
}

func TestReadSymbol_StaleBytesFallbackToLines(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"package billing",
		"",
		"func ProcessPayment() {",
		"	return nil",
		"}",
	}, "\n")
	writeFile(t, filepath.Join(root, "src", "billing.go"), content)

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	sym := store.SymbolRecord{
		FilePath:  "src/billing.go",
		Kind:      "function",
		Name:      "ProcessPayment",
		StartLine: 3,
		EndLine:   5,
		StartByte: len(content) + 100,
		EndByte:   len(content) + 200,
	}

	got, err := r.ReadSymbol(sym)
	if err != nil {
		t.Fatal(err)
	}
	want := "3\tfunc ProcessPayment() {\n4\t\treturn nil\n5\t}"
	if got != want {
		t.Fatalf("ReadSymbol() = %q, want line fallback %q", got, want)
	}
}

func TestReadByteRange_LineFormat(t *testing.T) {
	root := t.TempDir()
	content := "alpha\nbeta\n"
	writeFile(t, filepath.Join(root, "a.go"), content)

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.ReadByteRange("a.go", 0, len(content))
	if err != nil {
		t.Fatal(err)
	}
	want := "1\talpha\n2\tbeta"
	if got != want {
		t.Fatalf("ReadByteRange() = %q, want %q", got, want)
	}
}

func TestReadByteRange_InvalidRange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "line\n")

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		start, end int
		wantErr    error
	}{
		{-1, 1, read.ErrInvalidRange},
		{1, 1, read.ErrInvalidRange},
		{2, 1, read.ErrInvalidRange},
	}
	for _, tc := range tests {
		_, err := r.ReadByteRange("a.go", tc.start, tc.end)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("ReadByteRange(%d,%d) error = %v, want %v", tc.start, tc.end, err, tc.wantErr)
		}
	}
}

func TestReadLines_InclusiveRange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "billing.go"), strings.Join([]string{
		"package billing",
		"",
		"func ProcessPayment() {",
		"	return nil",
		"}",
	}, "\n"))

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.ReadLines("src/billing.go", 3, 5)
	if err != nil {
		t.Fatal(err)
	}

	want := "3\tfunc ProcessPayment() {\n4\t\treturn nil\n5\t}"
	if got != want {
		t.Fatalf("ReadLines() = %q, want %q", got, want)
	}
}

func TestReadLines_SingleLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "line1\nline2\nline3\n")

	got, err := read.ReadLines(root, "a.go", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2\tline2" {
		t.Fatalf("ReadLines() = %q, want %q", got, "2\tline2")
	}
}

func TestReadLines_LineFormat(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "alpha\nbeta\n")

	got, err := read.ReadLines(root, "a.go", 1, 2)
	if err != nil {
		t.Fatal(err)
	}

	lineRe := regexp.MustCompile(`^\d+\t`)
	for _, line := range strings.Split(got, "\n") {
		if !lineRe.MatchString(line) {
			t.Fatalf("line %q missing number-tab prefix", line)
		}
	}
}

func TestReadLines_MissingFile(t *testing.T) {
	root := t.TempDir()

	_, err := read.ReadLines(root, "missing.go", 1, 1)
	if !errors.Is(err, read.ErrFileNotFound) {
		t.Fatalf("ReadLines() error = %v, want ErrFileNotFound", err)
	}
}

func TestRead_TraversalRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.go"), "secret\n")
	writeFile(t, filepath.Join(root, "ok.go"), "ok\n")

	tests := []string{
		"../" + filepath.Base(outside) + "/secret.go",
		"../../" + strings.TrimPrefix(outside, string(filepath.Separator)),
	}
	// Absolute path outside root (platform-specific).
	if absOutside, err := filepath.Abs(filepath.Join(outside, "secret.go")); err == nil {
		tests = append(tests, absOutside)
	}

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, filePath := range tests {
		_, err := r.ReadLines(filePath, 1, 1)
		if err == nil {
			t.Fatalf("ReadLines(%q) succeeded, want error", filePath)
		}
		if !errors.Is(err, read.ErrPathOutsideRoot) && !errors.Is(err, read.ErrFileNotFound) {
			t.Fatalf("ReadLines(%q) error = %v, want ErrPathOutsideRoot or ErrFileNotFound", filePath, err)
		}
	}
}

func TestRead_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.go"), "top secret\n")

	linkPath := filepath.Join(root, "link.go")
	if err := os.Symlink(filepath.Join(outside, "secret.go"), linkPath); err != nil {
		t.Fatal(err)
	}

	r, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.ReadLines("link.go", 1, 1)
	if !errors.Is(err, read.ErrPathOutsideRoot) {
		t.Fatalf("ReadLines() error = %v, want ErrPathOutsideRoot", err)
	}
}

func TestReadLines_InvalidRange(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "line\n")

	tests := []struct {
		start, end int
	}{
		{0, 1},
		{1, 0},
		{5, 2},
	}

	for _, tc := range tests {
		_, err := read.ReadLines(root, "a.go", tc.start, tc.end)
		if !errors.Is(err, read.ErrInvalidRange) && !errors.Is(err, read.ErrSpanTooLarge) {
			t.Fatalf("ReadLines(%d,%d) error = %v, want ErrInvalidRange or ErrSpanTooLarge", tc.start, tc.end, err)
		}
	}
}

func TestReadLines_SpanTooLarge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "line\n")

	_, err := read.ReadLines(root, "a.go", 1, read.MaxLineSpan+1)
	if !errors.Is(err, read.ErrSpanTooLarge) {
		t.Fatalf("ReadLines() error = %v, want ErrSpanTooLarge", err)
	}
}

func TestReadLines_BeyondEOF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "one\ntwo\n")

	got, err := read.ReadLines(root, "a.go", 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2\ttwo" {
		t.Fatalf("ReadLines() = %q, want %q", got, "2\ttwo")
	}
}

func TestReadLines_StartBeyondEOF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "one\n")

	got, err := read.ReadLines(root, "a.go", 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("ReadLines() = %q, want empty", got)
	}
}

func TestReadLines_LeadingSlashPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "main.go"), "package main\n")

	got, err := read.ReadLines(root, "/pkg/main.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1\tpackage main" {
		t.Fatalf("ReadLines() = %q, want %q", got, "1\tpackage main")
	}
}

func TestReadLines_DirectoryRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := read.ReadLines(root, "pkg", 1, 1)
	if !errors.Is(err, read.ErrNotAFile) {
		t.Fatalf("ReadLines() error = %v, want ErrNotAFile", err)
	}
}

func TestReadLines_BrokenSymlink(t *testing.T) {
	root := t.TempDir()
	linkPath := filepath.Join(root, "broken.go")
	if err := os.Symlink(filepath.Join(root, "missing.go"), linkPath); err != nil {
		t.Fatal(err)
	}

	_, err := read.ReadLines(root, "broken.go", 1, 1)
	if !errors.Is(err, read.ErrFileNotFound) {
		t.Fatalf("ReadLines() error = %v, want ErrFileNotFound", err)
	}
}

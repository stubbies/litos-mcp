package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestInit_SummaryOutput(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	bin := buildBinary(t)
	cmd := exec.Command(bin, "init", "--root", root)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	line := strings.TrimSpace(lastLine(string(out)))
	re := regexp.MustCompile(`^files=(\d+) symbols=(\d+) indexer=(ctags|regex) elapsed_ms=(\d+) db_bytes=(\d+)$`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("summary line %q does not match expected format", line)
	}

	files, _ := strconv.Atoi(m[1])
	symbols, _ := strconv.Atoi(m[2])
	if files != 1 {
		t.Fatalf("files = %d, want 1", files)
	}
	if symbols < 1 {
		t.Fatalf("symbols = %d, want at least 1", symbols)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "litos-mcp")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/litos-mcp")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func writeGoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

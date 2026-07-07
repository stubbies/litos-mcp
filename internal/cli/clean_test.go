package cli_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stubbies/litos-mcp/internal/store"
)

func TestClean_RemovesCache(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	bin := buildBinary(t)

	initCmd := exec.Command(bin, "init", "--root", root)
	initCmd.Dir = root
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	if !store.Exists(root) {
		t.Fatal("cache missing after init")
	}

	cleanCmd := exec.Command(bin, "clean", "--root", root)
	cleanCmd.Dir = root
	out, err := cleanCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clean failed: %v\n%s", err, out)
	}
	assertCacheAbsent(t, root)
	if !strings.Contains(string(out), "removed "+store.CacheDBName) {
		t.Fatalf("expected removed message in output: %q", out)
	}
}

func TestClean_MissingCacheNoError(t *testing.T) {
	root := t.TempDir()
	bin := buildBinary(t)

	cmd := exec.Command(bin, "clean", "--root", root)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clean failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cache already absent") {
		t.Fatalf("expected cache already absent in output: %q", out)
	}
}

func TestClean_Reindex(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	bin := buildBinary(t)

	initCmd := exec.Command(bin, "init", "--root", root)
	initCmd.Dir = root
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	cleanCmd := exec.Command(bin, "clean", "--reindex", "--root", root)
	cleanCmd.Dir = root
	out, err := cleanCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clean --reindex failed: %v\n%s", err, out)
	}
	if !store.Exists(root) {
		t.Fatal("cache missing after clean --reindex")
	}

	line := strings.TrimSpace(lastLine(string(out)))
	if !strings.HasPrefix(line, "files=") {
		t.Fatalf("expected init summary line, got %q", line)
	}

	st, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	n, err := st.SymbolCount()
	if err != nil {
		t.Fatalf("SymbolCount: %v", err)
	}
	if n < 1 {
		t.Fatalf("symbols = %d, want at least 1", n)
	}
}

func TestClean_ReindexWithoutPriorCache(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	bin := buildBinary(t)

	cmd := exec.Command(bin, "clean", "--reindex", "--root", root)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clean --reindex failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cache absent; rebuilding") {
		t.Fatalf("expected cache absent; rebuilding in output: %q", out)
	}
	if strings.Contains(string(out), "cache already absent") {
		t.Fatalf("did not expect cache already absent in output: %q", out)
	}
	if !store.Exists(root) {
		t.Fatal("cache missing after clean --reindex")
	}

	st, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	n, err := st.SymbolCount()
	if err != nil {
		t.Fatalf("SymbolCount: %v", err)
	}
	if n < 1 {
		t.Fatalf("symbols = %d, want at least 1", n)
	}
}

func assertCacheAbsent(t *testing.T, root string) {
	t.Helper()
	for _, path := range store.CachePaths(root) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s absent, stat err = %v", path, err)
		}
	}
}

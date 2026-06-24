package repo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/repo"
)

func TestResolveRoot_FromGitRoot(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveRoot(sub, "")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestResolveRoot_FlagOverridesCWD(t *testing.T) {
	cwd := t.TempDir()
	flagRoot := t.TempDir()

	got, err := repo.ResolveRoot(cwd, flagRoot)
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(flagRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestResolveRoot_NoGitFallsBackToCWD(t *testing.T) {
	cwd := t.TempDir()
	sub := filepath.Join(cwd, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveRoot(sub, "")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestResolveRoot_FlagWalksUpToGitRoot(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "apps", "api")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveRoot(t.TempDir(), nested)
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestResolveRoot_GitFileWorktree(t *testing.T) {
	root := t.TempDir()
	gitFile := filepath.Join(root, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /path/to/actual/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveRoot(sub, "")
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveRoot() = %q, want %q", got, want)
	}
}

func TestRoot_UsesWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "cmd")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(sub)
	got, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestRoot_UsesClaudeProjectDir(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(sub)
	t.Setenv("CLAUDE_PROJECT_DIR", root)

	got, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestRoot_ClaudeProjectDirEmptyFallsBackToCWD(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(sub)
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	got, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

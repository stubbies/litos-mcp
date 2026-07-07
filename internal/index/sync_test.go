package index_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/store"
)

func TestSyncCoordinator_HydrateDetectsTouchedFile(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeGoFile(t, root, "pkg/other.go", "package pkg\n\nfunc Other() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	beforeSymbols, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}

	mainPath := filepath.Join(root, "main.go")
	info, err := os.Stat(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	future := info.ModTime().Add(time.Second)
	if err := os.Chtimes(mainPath, future, future); err != nil {
		t.Fatal(err)
	}

	elapsed, err := coord.Hydrate(ctx)
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if elapsed < 0 {
		t.Fatalf("unexpected negative hydration duration: %v", elapsed)
	}

	afterSymbols, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}
	if afterSymbols != beforeSymbols {
		t.Fatalf("symbol count changed on hydrate of one stale file: before=%d after=%d", beforeSymbols, afterSymbols)
	}

	meta, ok, err := st.GetFileMeta("main.go")
	if err != nil || !ok {
		t.Fatalf("GetFileMeta main.go: meta=%+v ok=%v err=%v", meta, ok, err)
	}
	info, err = os.Stat(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.MtimeNs != info.ModTime().UnixNano() || meta.Size != info.Size() {
		t.Fatalf("main.go meta not refreshed: meta=%+v disk mtime=%d size=%d", meta, info.ModTime().UnixNano(), info.Size())
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.HydrationMs < 0 {
		t.Fatalf("status hydration_ms = %d, want >= 0", status.HydrationMs)
	}
	if status.LastSyncAt == "" {
		t.Fatal("expected last_sync_at to be set after Hydrate")
	}
}

func TestSyncCoordinator_HydrateSkipsUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "stable.go", "package main\n\nfunc Stable() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	before, _, err := st.GetFileMeta("stable.go")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := coord.Hydrate(ctx); err != nil {
		t.Fatalf("first Hydrate: %v", err)
	}

	after, ok, err := st.GetFileMeta("stable.go")
	if err != nil || !ok {
		t.Fatalf("GetFileMeta: meta=%+v ok=%v err=%v", after, ok, err)
	}
	if after.MtimeNs != before.MtimeNs || after.Size != before.Size {
		t.Fatalf("unchanged file metadata mutated: before=%+v after=%+v", before, after)
	}
}

func TestSyncCoordinator_HydrateRemovesDeletedFile(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "keep.go", "package main\n\nfunc Keep() {}\n")
	writeGoFile(t, root, "drop.go", "package main\n\nfunc Drop() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "drop.go")); err != nil {
		t.Fatal(err)
	}

	if _, err := coord.Hydrate(ctx); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}

	n, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("FileCount = %d, want 1 after delete hydration", n)
	}

	hits, err := st.Search("Drop", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleted symbol still searchable: %+v", hits)
	}
}

func TestSyncCoordinator_HydrateDiscoversNewFileWhileOffline(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "existing.go", "package main\n\nfunc Existing() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	writeGoFile(t, root, "new.go", "package main\n\nfunc NewFunc() {}\n")

	if _, err := coord.Hydrate(ctx); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}

	n, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("FileCount = %d, want 2 after boot crawl found new file", n)
	}

	hits, err := st.Search("NewFunc", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("search NewFunc: got %d hits", len(hits))
	}
}

func TestSyncCoordinator_SyncFile(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "sync.go", "package main\n\nfunc Synced() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if err := coord.SyncFile(ctx, "sync.go"); err != nil {
		t.Fatalf("SyncFile: %v", err)
	}

	hits, err := st.Search("Synced", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("search Synced: got %d hits", len(hits))
	}

	if err := os.Remove(filepath.Join(root, "sync.go")); err != nil {
		t.Fatal(err)
	}
	if err := coord.SyncFile(ctx, "sync.go"); err != nil {
		t.Fatalf("SyncFile after delete: %v", err)
	}
	hits, err = st.Search("Synced", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("symbol still present after SyncFile remove: %+v", hits)
	}
}

func TestSyncCoordinator_SyncFileSkipsUnsupportedRegexExtension(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "lib.rs", "fn indexed() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if err := coord.SyncFile(ctx, "lib.rs"); err != nil {
		t.Fatalf("SyncFile: %v", err)
	}

	n, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("FileCount = %d, want 0 for regex-unsupported extension via SyncFile", n)
	}
}

func TestSyncCoordinator_ReconcileFullColdStart(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "cold.go", "package main\n\nfunc Cold() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	result, err := coord.ReconcileFull(ctx)
	if err != nil {
		t.Fatalf("ReconcileFull: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Fatalf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
	if result.SymbolsIndexed < 1 {
		t.Fatalf("SymbolsIndexed = %d, want at least 1", result.SymbolsIndexed)
	}

	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.ReconcileNeeded {
		t.Fatal("reconcile_needed should be cleared after ReconcileFull")
	}
	if status.Indexer != "regex" {
		t.Fatalf("Indexer = %q, want regex", status.Indexer)
	}
}

func TestSyncCoordinator_HydrateColdStartUsesReconcileFull(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "empty.go", "package main\n\nfunc Empty() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := coord.Hydrate(ctx); err != nil {
		t.Fatalf("Hydrate on empty index: %v", err)
	}

	n, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("FileCount = %d, want 1 after cold Hydrate", n)
	}
}

func TestSyncCoordinator_StartWatcherDebouncesRapidWrites(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "debounce.go", "package main\n\nfunc First() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext, index.WithDebounce(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	if err := coord.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	path := filepath.Join(root, "debounce.go")
	for i := 0; i < 3; i++ {
		content := fmt.Sprintf("package main\n\nfunc Debounced%d() {}\n", i)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	hits, err := st.Search("Debounced2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("search Debounced2: got %d hits, want final write indexed", len(hits))
	}

	hits, err = st.Search("First", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("stale symbol still searchable after debounced writes: %+v", hits)
	}
}

func TestSyncCoordinator_StartWatcherDeleteRemovesSymbols(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "watched.go", "package main\n\nfunc Watched() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext, index.WithDebounce(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	if err := coord.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "watched.go")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	hits, err := st.Search("Watched", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("deleted symbol still searchable: %+v", hits)
	}
}

func TestSyncCoordinator_WatcherOverflowSetsReconcileNeeded(t *testing.T) {
	if !index.IsWatcherOverflow(fsnotify.ErrEventOverflow) {
		t.Fatal("IsWatcherOverflow should recognize ErrEventOverflow")
	}

	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())
	coord.SetReconcileNeeded()

	ctx := context.Background()
	status, err := coord.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.ReconcileNeeded {
		t.Fatal("expected reconcile_needed after watcher overflow flag")
	}
}

func TestSyncCoordinator_EnsureFreshTriggersFullReconcile(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())
	coord.SetReconcileNeeded()

	ctx := context.Background()
	coord.EnsureFresh(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := coord.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !status.ReconcileNeeded && status.Files > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("EnsureFresh did not complete full reconcile within timeout")
}

func TestSyncCoordinator_EnsureFreshHydratesStaleFile(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "main.go", "package main\n\nfunc Original() {}\n")

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if _, err := index.Reindex(ctx, root, st, ext); err != nil {
		t.Fatal(err)
	}

	writeGoFile(t, root, "main.go", "package main\n\nfunc Updated() {}\n")

	coord.EnsureFresh(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hits, err := st.Search("Updated", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 1 && hits[0].Symbol == "Updated" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("EnsureFresh did not sync stale file within timeout")
}

func TestSyncCoordinator_SyncFileUpdatesCallSites(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "caller.go", `package main

func Work() {
	ProcessPayment()
}
`)

	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ext := index.NewRegexExtractor()
	coord := index.NewSyncCoordinator(root, st, ext)
	ctx := context.Background()

	if err := coord.SyncFile(ctx, "caller.go"); err != nil {
		t.Fatalf("SyncFile: %v", err)
	}

	hits, err := st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("ProcessPayment callers after sync: got %d", len(hits))
	}
	if hits[0].Line != 4 {
		t.Fatalf("call line = %d, want 4", hits[0].Line)
	}

	writeGoFile(t, root, "caller.go", `package main

func Work() {
	RefundPayment()
}
`)
	if err := coord.SyncFile(ctx, "caller.go"); err != nil {
		t.Fatalf("SyncFile after edit: %v", err)
	}

	hits, err = st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("stale ProcessPayment call still indexed: %+v", hits)
	}

	hits, err = st.FindCallers("RefundPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("RefundPayment callers after edit: got %d", len(hits))
	}
}

func TestStore_SetMetaGetMeta(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.SetMeta("last_sync_at", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.GetMeta("last_sync_at")
	if err != nil || !ok || v != "2026-01-01T00:00:00Z" {
		t.Fatalf("GetMeta = %q ok=%v err=%v", v, ok, err)
	}

	if err := st.SetMeta("last_sync_at", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	v, ok, err = st.GetMeta("last_sync_at")
	if err != nil || !ok || v != "2026-06-01T00:00:00Z" {
		t.Fatalf("GetMeta after update = %q ok=%v err=%v", v, ok, err)
	}

	_, ok, err = st.GetMeta("missing")
	if err != nil || ok {
		t.Fatalf("GetMeta missing: ok=%v err=%v", ok, err)
	}
}

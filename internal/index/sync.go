package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stubbies/litos-mcp/internal/crawl"
	"github.com/stubbies/litos-mcp/internal/store"
)

const (
	metaKeyLastSyncAt      = "last_sync_at"
	metaKeyIndexer         = "indexer"
	metaKeyLastHydrationMs = "last_hydration_ms"
	defaultDebounce        = 300 * time.Millisecond
)

// SyncStatus summarizes coordinator state for observability.
type SyncStatus struct {
	ReconcileNeeded  bool   `json:"reconcile_needed"`
	Files            int    `json:"files"`
	Symbols          int    `json:"symbols"`
	LastSyncAt       string `json:"last_sync_at,omitempty"`
	Indexer          string `json:"indexer,omitempty"`
	BoundaryIndexer  string `json:"boundary_indexer,omitempty"`
	HydrationMs      int64  `json:"hydration_ms,omitempty"`
}

// SyncOption configures optional SyncCoordinator behavior.
type SyncOption func(*SyncCoordinator)

// WithDebounce sets the per-path fsnotify debounce interval (for tests).
func WithDebounce(d time.Duration) SyncOption {
	return func(c *SyncCoordinator) {
		c.debounceDur = d
	}
}

// SyncCoordinator serializes index sync operations shared by serve, fsnotify, and MCP tools.
type SyncCoordinator struct {
	mu              sync.Mutex
	repoRoot        string
	store           *store.Store
	ext             Extractor
	reconcileNeeded bool
	debounceDur     time.Duration
	ensureSyncing   atomic.Bool

	watcherMu sync.Mutex
	pending   map[string]*pathPending
}

type pathPending struct {
	remove bool
	timer  *time.Timer
}

// NewSyncCoordinator creates a coordinator for repoRoot backed by st and ext.
func NewSyncCoordinator(repoRoot string, st *store.Store, ext Extractor, opts ...SyncOption) *SyncCoordinator {
	c := &SyncCoordinator{
		repoRoot: repoRoot,
		store:    st,
		ext:      ext,
		pending:  make(map[string]*pathPending),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *SyncCoordinator) debounceInterval() time.Duration {
	if c.debounceDur > 0 {
		return c.debounceDur
	}
	return defaultDebounce
}

// SyncPaths extracts and upserts symbols for paths using metadata from metaByPath.
func SyncPaths(ctx context.Context, repoRoot string, st *store.Store, ext Extractor, paths []string, metaByPath map[string]store.FileMeta) error {
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)

	extracted, err := ext.Extract(ctx, repoRoot, paths)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	byFile := groupSymbolsByFile(extracted)
	for _, path := range paths {
		meta := metaByPath[path]
		if err := st.UpsertFile(meta, byFile[path]); err != nil {
			return fmt.Errorf("upsert %s: %w", path, err)
		}
	}
	return nil
}

// Hydrate performs a boot stat pass on indexed paths and a single crawl for new/deleted paths.
func (c *SyncCoordinator) Hydrate(ctx context.Context) (time.Duration, error) {
	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.hydrateStatPass(ctx); err != nil {
		return 0, err
	}

	fileCount, err := c.store.FileCount()
	if err != nil {
		return 0, fmt.Errorf("count files after stat pass: %w", err)
	}
	if fileCount == 0 {
		if _, err := c.reconcileFullLocked(ctx); err != nil {
			return 0, err
		}
		elapsed := time.Since(start)
		if err := c.persistSyncMeta(elapsed.Milliseconds()); err != nil {
			return 0, err
		}
		return elapsed, nil
	}

	if err := c.hydrateBootCrawl(ctx); err != nil {
		return 0, err
	}

	elapsed := time.Since(start)
	if err := c.persistSyncMeta(elapsed.Milliseconds()); err != nil {
		return 0, err
	}
	return elapsed, nil
}

func (c *SyncCoordinator) hydrateStatPass(ctx context.Context) error {
	indexed, err := c.store.ListFiles()
	if err != nil {
		return fmt.Errorf("list indexed files: %w", err)
	}

	var stalePaths []string
	staleMeta := make(map[string]store.FileMeta)
	for _, meta := range indexed {
		absPath := filepath.Join(c.repoRoot, filepath.FromSlash(meta.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				if err := c.store.RemoveFile(meta.Path); err != nil {
					return fmt.Errorf("remove missing file %s: %w", meta.Path, err)
				}
				continue
			}
			return fmt.Errorf("stat %s: %w", meta.Path, err)
		}
		mtimeNs := info.ModTime().UnixNano()
		size := info.Size()
		if meta.MtimeNs != mtimeNs || meta.Size != size {
			stalePaths = append(stalePaths, meta.Path)
			staleMeta[meta.Path] = store.FileMeta{
				Path:    meta.Path,
				MtimeNs: mtimeNs,
				Size:    size,
			}
		}
	}

	return SyncPaths(ctx, c.repoRoot, c.store, c.ext, stalePaths, staleMeta)
}

func (c *SyncCoordinator) hydrateBootCrawl(ctx context.Context) error {
	crawled, err := crawl.Crawl(ctx, c.repoRoot, crawl.Options{})
	if err != nil {
		return fmt.Errorf("boot crawl: %w", err)
	}
	crawled = filterCrawledForExtractor(crawled, c.ext)

	crawledByPath := make(map[string]crawl.File, len(crawled))
	for _, f := range crawled {
		crawledByPath[f.Path] = f
	}

	indexed, err := c.store.ListFiles()
	if err != nil {
		return fmt.Errorf("list indexed files: %w", err)
	}
	indexedByPath := make(map[string]store.FileMeta, len(indexed))
	for _, meta := range indexed {
		indexedByPath[meta.Path] = meta
	}

	for _, f := range crawled {
		if _, ok := indexedByPath[f.Path]; ok {
			continue
		}
		if err := c.syncFileLocked(ctx, f.Path); err != nil {
			return fmt.Errorf("sync new file %s: %w", f.Path, err)
		}
	}

	for _, meta := range indexed {
		if _, ok := crawledByPath[meta.Path]; !ok {
			if err := c.store.RemoveFile(meta.Path); err != nil {
				return fmt.Errorf("remove absent crawl path %s: %w", meta.Path, err)
			}
		}
	}
	return nil
}

// SyncFile stats path, extracts symbols, and upserts (or removes if the file is gone).
func (c *SyncCoordinator) SyncFile(ctx context.Context, relPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syncFileLocked(ctx, relPath)
}

// RemoveFile drops path from the index without re-extracting.
func (c *SyncCoordinator) RemoveFile(ctx context.Context, relPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.removeFileLocked(relPath)
}

func (c *SyncCoordinator) removeFileLocked(relPath string) error {
	relPath = filepath.ToSlash(relPath)
	if err := c.store.RemoveFile(relPath); err != nil {
		return fmt.Errorf("remove %s: %w", relPath, err)
	}
	return c.persistSyncMeta(-1)
}

// StartWatcher watches repoRoot with fsnotify, debouncing per-path sync work.
// Write/create events sync the file; remove/rename events drop it from the index.
// Watcher errors (including overflow) flag reconcileNeeded for a later full reconcile.
func (c *SyncCoordinator) StartWatcher(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}

	rules, err := crawl.NewIgnoreRules(c.repoRoot)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("ignore rules: %w", err)
	}

	if err := c.addWatchTree(watcher, rules, c.repoRoot); err != nil {
		watcher.Close()
		return err
	}

	go c.runWatcher(ctx, watcher, rules)
	return nil
}

func (c *SyncCoordinator) addWatchTree(watcher *fsnotify.Watcher, rules *crawl.IgnoreRules, absDir string) error {
	relDir, err := filepath.Rel(c.repoRoot, absDir)
	if err != nil {
		return fmt.Errorf("rel dir %s: %w", absDir, err)
	}
	relDir = filepath.ToSlash(relDir)

	if relDir != "." && rules.SkipDir(relDir, absDir) {
		return nil
	}

	if err := rules.EnterDir(absDir); err != nil {
		return err
	}
	defer rules.LeaveDir()

	if err := watcher.Add(absDir); err != nil {
		return fmt.Errorf("watch %s: %w", absDir, err)
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return fmt.Errorf("readdir %s: %w", absDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := c.addWatchTree(watcher, rules, filepath.Join(absDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (c *SyncCoordinator) runWatcher(ctx context.Context, watcher *fsnotify.Watcher, rules *crawl.IgnoreRules) {
	defer watcher.Close()
	defer c.clearPendingTimers()

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "litos-mcp: fsnotify error: %v\n", err)
				c.SetReconcileNeeded()
			}
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			c.handleWatchEvent(ctx, watcher, rules, event)
		}
	}
}

func (c *SyncCoordinator) handleWatchEvent(_ context.Context, watcher *fsnotify.Watcher, rules *crawl.IgnoreRules, event fsnotify.Event) {
	relPath, err := filepath.Rel(c.repoRoot, event.Name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "litos-mcp: fsnotify rel path %s: %v\n", event.Name, err)
		c.SetReconcileNeeded()
		return
	}
	relPath = filepath.ToSlash(relPath)

	if event.Has(fsnotify.Create) {
		info, statErr := os.Stat(event.Name)
		if statErr == nil && info.IsDir() {
			if err := c.addWatchTree(watcher, rules, event.Name); err != nil {
				fmt.Fprintf(os.Stderr, "litos-mcp: fsnotify add watch %s: %v\n", event.Name, err)
				c.SetReconcileNeeded()
			}
			if rules.SkipDir(relPath, event.Name) {
				return
			}
		}
	}

	if event.Has(fsnotify.Chmod) {
		return
	}

	info, statErr := os.Stat(event.Name)
	isDir := statErr == nil && info.IsDir()
	if isDir {
		if rules.SkipDir(relPath, event.Name) {
			return
		}
	} else if rules.SkipFile(relPath, event.Name) {
		return
	}

	switch {
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		c.schedulePathSync(relPath, true)
	case event.Has(fsnotify.Write) || event.Has(fsnotify.Create):
		if !isDir {
			c.schedulePathSync(relPath, false)
		}
	}
}

func (c *SyncCoordinator) schedulePathSync(relPath string, remove bool) {
	interval := c.debounceInterval()

	c.watcherMu.Lock()
	defer c.watcherMu.Unlock()

	p, ok := c.pending[relPath]
	if !ok {
		p = &pathPending{}
		c.pending[relPath] = p
	}
	p.remove = remove
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(interval, func() {
		c.flushPathSync(relPath)
	})
}

func (c *SyncCoordinator) flushPathSync(relPath string) {
	c.watcherMu.Lock()
	p, ok := c.pending[relPath]
	if ok {
		delete(c.pending, relPath)
	}
	remove := ok && p.remove
	c.watcherMu.Unlock()

	ctx := context.Background()
	if remove {
		if err := c.RemoveFile(ctx, relPath); err != nil {
			fmt.Fprintf(os.Stderr, "litos-mcp: fsnotify remove %s: %v\n", relPath, err)
		}
		return
	}
	if err := c.SyncFile(ctx, relPath); err != nil {
		fmt.Fprintf(os.Stderr, "litos-mcp: fsnotify sync %s: %v\n", relPath, err)
	}
}

func (c *SyncCoordinator) clearPendingTimers() {
	c.watcherMu.Lock()
	defer c.watcherMu.Unlock()
	for path, p := range c.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(c.pending, path)
	}
}

// ReconcileNeeded reports whether a full reconcile was flagged (e.g. fsnotify overflow).
func (c *SyncCoordinator) ReconcileNeeded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconcileNeeded
}

// IsWatcherOverflow reports whether err is fsnotify.ErrEventOverflow.
func IsWatcherOverflow(err error) bool {
	return errors.Is(err, fsnotify.ErrEventOverflow)
}

func (c *SyncCoordinator) syncFileLocked(ctx context.Context, relPath string) error {
	relPath = filepath.ToSlash(relPath)
	if c.ext.Name() == "regex" && !regexSupportedPath(relPath) {
		return nil
	}

	absPath := filepath.Join(c.repoRoot, filepath.FromSlash(relPath))
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.store.RemoveFile(relPath)
		}
		return fmt.Errorf("stat %s: %w", relPath, err)
	}

	meta := store.FileMeta{
		Path:    relPath,
		MtimeNs: info.ModTime().UnixNano(),
		Size:    info.Size(),
	}
	if err := SyncPaths(ctx, c.repoRoot, c.store, c.ext, []string{relPath}, map[string]store.FileMeta{relPath: meta}); err != nil {
		return err
	}
	return c.persistSyncMeta(-1)
}

// ReconcileFull runs a full crawl → extract → store pass and clears reconcileNeeded.
func (c *SyncCoordinator) ReconcileFull(ctx context.Context) (*ReindexResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconcileFullLocked(ctx)
}

func (c *SyncCoordinator) reconcileFullLocked(ctx context.Context) (*ReindexResult, error) {
	result, err := Reindex(ctx, c.repoRoot, c.store, c.ext)
	if err != nil {
		return nil, err
	}
	c.reconcileNeeded = false
	if err := c.persistSyncMeta(-1); err != nil {
		return nil, err
	}
	return result, nil
}

// SetReconcileNeeded flags the index for a full reconcile on the next EnsureFresh call.
func (c *SyncCoordinator) SetReconcileNeeded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconcileNeeded = true
}

// EnsureFresh kicks off a non-blocking background sync when the index may be stale.
// Search and other hot paths call this before querying FTS; they proceed on the current index.
// If reconcileNeeded is set, a full reconcile runs; otherwise only indexed paths with stat mismatches are synced.
func (c *SyncCoordinator) EnsureFresh(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	c.mu.Lock()
	needFull := c.reconcileNeeded
	c.mu.Unlock()

	if needFull {
		c.spawnEnsureSync(func(ctx context.Context) error {
			_, err := c.ReconcileFull(ctx)
			return err
		})
		return
	}

	stale, err := c.hasStalePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "litos-mcp: ensure fresh stat check: %v\n", err)
		return
	}
	if !stale {
		return
	}

	c.spawnEnsureSync(func(ctx context.Context) error {
		return c.quickHydrate(ctx)
	})
}

func (c *SyncCoordinator) spawnEnsureSync(fn func(context.Context) error) {
	if !c.ensureSyncing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.ensureSyncing.Store(false)
		if err := fn(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "litos-mcp: ensure fresh: %v\n", err)
		}
	}()
}

func (c *SyncCoordinator) hasStalePaths() (bool, error) {
	indexed, err := c.store.ListFiles()
	if err != nil {
		return false, fmt.Errorf("list indexed files: %w", err)
	}
	for _, meta := range indexed {
		absPath := filepath.Join(c.repoRoot, filepath.FromSlash(meta.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, fmt.Errorf("stat %s: %w", meta.Path, err)
		}
		mtimeNs := info.ModTime().UnixNano()
		size := info.Size()
		if meta.MtimeNs != mtimeNs || meta.Size != size {
			return true, nil
		}
	}
	return false, nil
}

func (c *SyncCoordinator) quickHydrate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.hydrateStatPass(ctx); err != nil {
		return err
	}
	return c.persistSyncMeta(-1)
}

// Status returns current index sync observability fields.
func (c *SyncCoordinator) Status(ctx context.Context) (*SyncStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	reconcileNeeded := c.reconcileNeeded
	c.mu.Unlock()

	files, err := c.store.FileCount()
	if err != nil {
		return nil, fmt.Errorf("count files: %w", err)
	}
	symbols, err := c.store.SymbolCount()
	if err != nil {
		return nil, fmt.Errorf("count symbols: %w", err)
	}

	status := &SyncStatus{
		ReconcileNeeded: reconcileNeeded,
		Files:           files,
		Symbols:         symbols,
		Indexer:         c.ext.Name(),
		BoundaryIndexer: BoundaryIndexer(),
	}

	if v, ok, err := c.store.GetMeta(metaKeyLastSyncAt); err != nil {
		return nil, err
	} else if ok {
		status.LastSyncAt = v
	}
	if v, ok, err := c.store.GetMeta(metaKeyLastHydrationMs); err != nil {
		return nil, err
	} else if ok {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse hydration_ms meta: %w", err)
		}
		status.HydrationMs = ms
	}
	return status, nil
}

func (c *SyncCoordinator) persistSyncMeta(hydrationMs int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if err := c.store.SetMeta(metaKeyLastSyncAt, now); err != nil {
		return err
	}
	if err := c.store.SetMeta(metaKeyIndexer, c.ext.Name()); err != nil {
		return err
	}
	if hydrationMs >= 0 {
		if err := c.store.SetMeta(metaKeyLastHydrationMs, strconv.FormatInt(hydrationMs, 10)); err != nil {
			return err
		}
	}
	return nil
}

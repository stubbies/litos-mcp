package crawl

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

// File describes a crawled source file candidate relative to the repo root.
type File struct {
	Path    string // repo-relative path with forward slashes
	MtimeNs int64
	Size    int64
}

// Options configures concurrent crawl behavior.
type Options struct {
	// Workers is the bounded worker pool size. Zero uses GOMAXPROCS.
	Workers int
}

type fileJob struct {
	relPath string
	info    fs.DirEntry
}

// Crawl walks repoRoot and returns indexable source files using a concurrent worker pool.
func Crawl(ctx context.Context, repoRoot string, opts Options) ([]File, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("crawl: abs root: %w", err)
	}

	rules, err := NewIgnoreRules(root)
	if err != nil {
		return nil, err
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan fileJob)
	var files []File
	var filesMu sync.Mutex
	var collectErr error
	var collectOnce sync.Once

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				info, err := job.info.Info()
				if err != nil {
					collectOnce.Do(func() {
						collectErr = fmt.Errorf("crawl: stat %s: %w", job.relPath, err)
					})
					continue
				}
				if info.IsDir() {
					continue
				}
				rec := File{
					Path:    filepath.ToSlash(job.relPath),
					MtimeNs: info.ModTime().UnixNano(),
					Size:    info.Size(),
				}
				filesMu.Lock()
				files = append(files, rec)
				filesMu.Unlock()
			}
		}()
	}

	c := &crawler{
		ctx:   ctx,
		root:  root,
		rules: rules,
		jobs:  jobs,
	}

	walkErr := c.walkDir(root)
	close(jobs)
	wg.Wait()

	if walkErr != nil {
		return nil, walkErr
	}
	if collectErr != nil {
		return nil, collectErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

type crawler struct {
	ctx   context.Context
	root  string
	rules *IgnoreRules
	jobs  chan<- fileJob
}

func (c *crawler) walkDir(absDir string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}

	relDir, err := filepath.Rel(c.root, absDir)
	if err != nil {
		return fmt.Errorf("crawl: rel dir %s: %w", absDir, err)
	}
	relDir = filepath.ToSlash(relDir)

	if relDir != "." && c.rules.SkipDir(relDir, absDir) {
		return nil
	}

	if err := c.rules.EnterDir(absDir); err != nil {
		return err
	}
	defer c.rules.LeaveDir()

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return fmt.Errorf("crawl: readdir %s: %w", absDir, err)
	}

	for _, entry := range entries {
		if err := c.ctx.Err(); err != nil {
			return err
		}

		absPath := filepath.Join(absDir, entry.Name())
		relPath, err := filepath.Rel(c.root, absPath)
		if err != nil {
			return fmt.Errorf("crawl: rel path %s: %w", absPath, err)
		}
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			if err := c.walkDir(absPath); err != nil {
				return err
			}
			continue
		}

		if c.rules.SkipFile(relPath, absPath) {
			continue
		}

		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case c.jobs <- fileJob{relPath: relPath, info: entry}:
		}
	}

	return nil
}

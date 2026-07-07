package testutil

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
)

// GoldenMetrics holds frozen expectations for testdata/fixture-repo.
type GoldenMetrics struct {
	FilesIndexed        int `json:"files_indexed"`
	SymbolsIndexedMin   int `json:"symbols_indexed_min"`
	SymbolsInHelpersGo  int `json:"symbols_in_helpers_go"`
	Search              struct {
		ProcessPayment struct {
			FilePath  string `json:"file_path"`
			StartLine int    `json:"start_line"`
			Symbol    string `json:"symbol"`
		} `json:"process_payment"`
		JWTVerificationMinHits int `json:"jwt_verification_min_hits"`
	} `json:"search"`
	ReadSlice struct {
		FilePath  string `json:"file_path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	} `json:"read_slice"`
	Callers struct {
		ProcessPayment struct {
			FilePath          string `json:"file_path"`
			Line              int    `json:"line"`
			Col               int    `json:"col"`
			EnclosingSymbol   string `json:"enclosing_symbol"`
			EnclosingKind     string `json:"enclosing_kind"`
			EnclosingSymbolID string `json:"enclosing_symbol_id"`
		} `json:"process_payment"`
		ProcessPaymentMinHits int `json:"process_payment_min_hits"`
	} `json:"callers"`
	Thresholds struct {
		SearchTokenBudget        int     `json:"search_token_budget"`
		CallersTokenBudget       int     `json:"callers_token_budget"`
		ReadTokenBudget            int     `json:"read_token_budget"`
		ReadVsWholeFileRatioMax    float64 `json:"read_vs_whole_file_ratio_max"`
		IndexSizeBytesMax          int     `json:"index_size_bytes_max"`
		IndexCompressionRatioMax   float64 `json:"index_compression_ratio_max"`
		SymbolDensityMin           float64 `json:"symbol_density_min"`
		SearchLatencyMs            int     `json:"search_latency_ms"`
		ReadLatencyMs              int     `json:"read_latency_ms"`
		InitLatencyMs              int     `json:"init_latency_ms"`
		HydrationMs                int     `json:"hydration_ms"`
		IncrementalLatencyMs       int     `json:"incremental_latency_ms"`
		InitDBBytesTolerancePct    float64 `json:"init_db_bytes_tolerance_pct"`
		TreesitterRefineMs         int     `json:"treesitter_refine_ms"`
	} `json:"thresholds"`
	InitDBBytes int64 `json:"init_db_bytes"`
	Benchmarks struct {
		SearchNsOpMax int64 `json:"search_ns_op_max"`
	} `json:"benchmarks"`
}

// FixtureRepoPath returns the absolute path to testdata/fixture-repo.
func FixtureRepoPath(tb testing.TB) string {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixture-repo")
	abs, err := filepath.Abs(root)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		tb.Fatalf("fixture repo missing at %s: %v", abs, err)
	}
	return abs
}

// LoadGoldenMetrics reads testdata/metrics.json.
func LoadGoldenMetrics(tb testing.TB) GoldenMetrics {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "metrics.json")
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read metrics.json: %v", err)
	}
	var m GoldenMetrics
	if err := json.Unmarshal(data, &m); err != nil {
		tb.Fatalf("parse metrics.json: %v", err)
	}
	return m
}

// CopyFixtureRepo copies the committed fixture into a temp directory for mutation.
func CopyFixtureRepo(tb testing.TB) string {
	tb.Helper()
	src := FixtureRepoPath(tb)
	dst := tb.TempDir()
	if err := copyDir(src, dst); err != nil {
		tb.Fatalf("copy fixture repo: %v", err)
	}
	return dst
}

// InitFixture opens the cache DB and runs a full regex reindex (deterministic in CI).
func InitFixture(tb testing.TB, repoRoot string) (*store.Store, *index.ReindexResult) {
	return InitFixtureWithExtractor(tb, repoRoot, index.NewRegexExtractor())
}

// InitFixtureWithExtractor opens the cache DB and runs a full reindex with ext.
func InitFixtureWithExtractor(tb testing.TB, repoRoot string, ext index.Extractor) (*store.Store, *index.ReindexResult) {
	tb.Helper()
	st, err := store.Open(repoRoot)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { st.Close() })

	result, err := index.Reindex(context.Background(), repoRoot, st, ext)
	if err != nil {
		tb.Fatalf("reindex fixture: %v", err)
	}
	return st, result
}

// NewReader creates a line reader for repoRoot.
func NewReader(tb testing.TB, repoRoot string) *read.Reader {
	tb.Helper()
	r, err := read.New(repoRoot)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}

// TotalSourceBytes sums bytes of indexable source files under repoRoot.
func TotalSourceBytes(tb testing.TB, repoRoot string) int64 {
	tb.Helper()
	var total int64
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		tb.Fatalf("walk fixture source bytes: %v", err)
	}
	return total
}

// TouchFile updates mtime on path to trigger incremental reindex.
func TouchFile(tb testing.TB, path string) {
	tb.Helper()
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		tb.Fatalf("touch %s: %v", path, err)
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

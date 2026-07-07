package eval_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/query"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/testutil"
)

const grepVsLitosRatioMaxFallback = 0.5

// EvalTask is a fixture task loaded from testdata/eval/tasks.json.
type EvalTask struct {
	ID                     string `json:"id"`
	Query                  string `json:"query"`
	ExpectSymbol           string `json:"expect_symbol"`
	ExpectFile             string `json:"expect_file"`
	Callee                 string `json:"callee"`
	ExpectEnclosing        string `json:"expect_enclosing"`
	Dir                    string `json:"dir"`
	ExpectDefsMin          int    `json:"expect_defs_min"`
	ExpectOutgoingContains string `json:"expect_outgoing_contains"`
}

func loadEvalTasks(tb testing.TB) []EvalTask {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "eval", "tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read tasks.json: %v", err)
	}
	var tasks []EvalTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		tb.Fatalf("parse tasks.json: %v", err)
	}
	if len(tasks) == 0 {
		tb.Fatal("tasks.json is empty")
	}
	return tasks
}

func setupEvalFixture(tb testing.TB) (context.Context, string, *store.Store, *index.SyncCoordinator, func()) {
	tb.Helper()
	root := testutil.CopyFixtureRepo(tb)
	st, err := store.Open(root)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := index.Reindex(context.Background(), root, st, index.NewRegexExtractor()); err != nil {
		tb.Fatal(err)
	}
	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())
	cleanup := func() { st.Close() }
	return context.Background(), root, st, coord, cleanup
}

func TestEvalTasks(t *testing.T) {
	tasks := loadEvalTasks(t)
	metrics := testutil.LoadGoldenMetrics(t)
	ctx, root, st, coord, cleanup := setupEvalFixture(t)
	defer cleanup()

	for _, task := range tasks {
		task := task
		t.Run(task.ID, func(t *testing.T) {
			switch {
			case task.Query != "":
				runLocateTask(t, ctx, root, st, coord, metrics, task)
			case task.Callee != "":
				runTraceTask(t, ctx, st, coord, metrics, task)
			case task.Dir != "":
				runMapTask(t, ctx, st, coord, metrics, task)
			default:
				t.Fatalf("task %q has no recognized fields", task.ID)
			}
		})
	}
}

func runLocateTask(
	t *testing.T,
	ctx context.Context,
	repoRoot string,
	st *store.Store,
	coord *index.SyncCoordinator,
	metrics testutil.GoldenMetrics,
	task EvalTask,
) {
	hits, err := query.Search(ctx, st, coord, query.SearchOpts{
		Query: task.Query,
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, hit := range hits {
		if hit.Symbol == task.ExpectSymbol && hit.FilePath == task.ExpectFile {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in %q among hits: %+v", task.ExpectSymbol, task.ExpectFile, hits)
	}

	data, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}
	litosTokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "search_token_budget", metrics.Thresholds.SearchTokenBudget, litosTokens)

	grepOutput := simulateGrep(repoRoot, task.Query)
	grepTokens := testutil.EstimateTokens(grepOutput)
	if grepTokens == 0 {
		t.Fatal("grep simulation produced zero tokens")
	}
	ratio := float64(litosTokens) / float64(grepTokens)
	ratioMax := metrics.Thresholds.GrepVsLitosRatioMax
	if ratioMax <= 0 {
		ratioMax = grepVsLitosRatioMaxFallback
	}
	testutil.AssertMaxFloat(t, "grep_vs_litos_ratio_max", ratioMax, ratio)
}

func runTraceTask(
	t *testing.T,
	ctx context.Context,
	st *store.Store,
	coord *index.SyncCoordinator,
	metrics testutil.GoldenMetrics,
	task EvalTask,
) {
	result, err := query.FindCallers(ctx, st, coord, query.FindCallersOpts{
		Name:  task.Callee,
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, hit := range result.Hits {
		if hit.EnclosingSymbol == task.ExpectEnclosing {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected enclosing %q among caller hits: %+v", task.ExpectEnclosing, result.Hits)
	}

	data, err := json.Marshal(result.Hits)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "callers_token_budget", metrics.Thresholds.CallersTokenBudget, tokens)
}

func runMapTask(
	t *testing.T,
	ctx context.Context,
	st *store.Store,
	coord *index.SyncCoordinator,
	metrics testutil.GoldenMetrics,
	task EvalTask,
) {
	result, err := query.MapDirectory(ctx, st, coord, query.MapDirectoryOpts{
		Dir: task.Dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DefinitionCount < task.ExpectDefsMin {
		t.Fatalf("definition_count = %d, want >= %d", result.DefinitionCount, task.ExpectDefsMin)
	}

	var foundOutgoing bool
	for _, call := range result.OutgoingCalls {
		if call.CalleeName == task.ExpectOutgoingContains {
			foundOutgoing = true
			break
		}
	}
	if !foundOutgoing {
		t.Fatalf("expected outgoing call %q; got %+v", task.ExpectOutgoingContains, result.OutgoingCalls)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "map_token_budget", metrics.Thresholds.MapTokenBudget, tokens)
}

// simulateGrep walks repoRoot and returns ripgrep-style lines for query tokens.
func simulateGrep(repoRoot, query string) string {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return ""
	}

	var b strings.Builder
	_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".litos" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			lower := strings.ToLower(line)
			matchesAll := true
			for _, tok := range tokens {
				if !strings.Contains(lower, tok) {
					matchesAll = false
					break
				}
			}
			if matchesAll {
				b.WriteString(rel)
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(i + 1))
				b.WriteByte(':')
				b.WriteString(strings.TrimSpace(line))
				b.WriteByte('\n')
			}
		}
		return nil
	})
	return b.String()
}

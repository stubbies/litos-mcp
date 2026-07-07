package testutil_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	litosmcp "github.com/stubbies/litos-mcp/internal/mcp"
	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/testutil"
)

func golden(t *testing.T) testutil.GoldenMetrics {
	t.Helper()
	return testutil.LoadGoldenMetrics(t)
}

func freshFixture(t *testing.T) (string, *store.Store, testutil.GoldenMetrics) {
	t.Helper()
	m := golden(t)
	root := testutil.CopyFixtureRepo(t)
	st, _ := testutil.InitFixture(t, root)
	return root, st, m
}

func TestInit_FileCount(t *testing.T) {
	_, st, m := freshFixture(t)
	files, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqualInt(t, "files_indexed", m.FilesIndexed, files)
}

func TestInit_SymbolCount(t *testing.T) {
	_, st, m := freshFixture(t)
	symbols, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMinInt(t, "symbols_indexed", m.SymbolsIndexedMin, symbols)
}

func TestFixtureMapDirectory_Handlers(t *testing.T) {
	_, st, m := freshFixture(t)
	result, err := st.MapDirectory(m.Map.HandlersDir, 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMinInt(t, "handlers_defs", m.Map.DefsMin, result.DefinitionCount)

	var handleCharge bool
	for _, def := range result.Definitions {
		if def.Symbol == m.Map.HandleChargeSymbol {
			handleCharge = true
			break
		}
	}
	if !handleCharge {
		t.Fatalf("HandleCharge not in definitions: %+v", result.Definitions)
	}

	callees := make(map[string]bool)
	for _, call := range result.OutgoingCalls {
		callees[call.CalleeName] = true
	}
	for _, want := range m.Map.OutgoingCallees {
		if !callees[want] {
			t.Fatalf("outgoing calls missing %q; got %+v", want, result.OutgoingCalls)
		}
	}
}

func TestFixtureFindCallers_ProcessPayment(t *testing.T) {
	_, st, m := freshFixture(t)
	hits, err := st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMinInt(t, "process_payment_callers", m.Callers.ProcessPaymentMinHits, len(hits))

	want := m.Callers.ProcessPayment
	var match *store.CallerHit
	for i := range hits {
		h := &hits[i]
		if h.FilePath == want.FilePath && h.EnclosingSymbol == want.EnclosingSymbol {
			match = h
			break
		}
	}
	if match == nil {
		t.Fatalf("no caller hit in %s with enclosing %q; got %+v", want.FilePath, want.EnclosingSymbol, hits)
	}
	if match.Line != want.Line {
		t.Fatalf("line = %d, want %d", match.Line, want.Line)
	}
	if match.Col != want.Col {
		t.Fatalf("col = %d, want %d", match.Col, want.Col)
	}
	if match.EnclosingKind != want.EnclosingKind {
		t.Fatalf("enclosing_kind = %q, want %q", match.EnclosingKind, want.EnclosingKind)
	}
	if match.EnclosingSymbolID != want.EnclosingSymbolID {
		t.Fatalf("enclosing_symbol_id = %q, want %q", match.EnclosingSymbolID, want.EnclosingSymbolID)
	}
}

func TestSearch_ExactHit(t *testing.T) {
	_, st, m := freshFixture(t)
	hits, err := st.Search("ProcessPayment", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("ProcessPayment hits = %d, want 1", len(hits))
	}
	want := m.Search.ProcessPayment
	if hits[0].FilePath != want.FilePath {
		t.Fatalf("file_path = %q, want %q", hits[0].FilePath, want.FilePath)
	}
	if hits[0].Symbol != want.Symbol {
		t.Fatalf("symbol = %q, want %q", hits[0].Symbol, want.Symbol)
	}
	if hits[0].StartLine != want.StartLine {
		t.Fatalf("start_line = %d, want %d", hits[0].StartLine, want.StartLine)
	}
	wantID := store.FormatSymbolID(store.SymbolRecord{
		FilePath:  want.FilePath,
		Kind:      "function",
		Name:      want.Symbol,
		StartLine: want.StartLine,
	})
	if hits[0].SymbolID != wantID {
		t.Fatalf("symbol_id = %q, want %q", hits[0].SymbolID, wantID)
	}
}

func TestSearch_NameMatchExactHit(t *testing.T) {
	_, st, m := freshFixture(t)
	hits, err := st.SearchWithOptions("ProcessPayment", 10, store.SearchOptions{NameMatch: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("ProcessPayment exact hits = %d, want 1", len(hits))
	}
	want := m.Search.ProcessPayment
	if hits[0].FilePath != want.FilePath {
		t.Fatalf("file_path = %q, want %q", hits[0].FilePath, want.FilePath)
	}
	if hits[0].Symbol != want.Symbol {
		t.Fatalf("symbol = %q, want %q", hits[0].Symbol, want.Symbol)
	}
	if hits[0].StartLine != want.StartLine {
		t.Fatalf("start_line = %d, want %d", hits[0].StartLine, want.StartLine)
	}
	wantID := store.FormatSymbolID(store.SymbolRecord{
		FilePath:  want.FilePath,
		Kind:      "function",
		Name:      want.Symbol,
		StartLine: want.StartLine,
	})
	if hits[0].SymbolID != wantID {
		t.Fatalf("symbol_id = %q, want %q", hits[0].SymbolID, wantID)
	}

	rec, err := st.GetSymbolByID(hits[0].SymbolID)
	if err != nil {
		t.Fatalf("GetSymbolByID round-trip: %v", err)
	}
	if rec.Name != want.Symbol {
		t.Fatalf("GetSymbolByID name = %q, want %q", rec.Name, want.Symbol)
	}
}

func TestSearch_MultiToken(t *testing.T) {
	_, st, m := freshFixture(t)
	limit := 10
	hits, err := st.Search("jwt verification", limit)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMinInt(t, "jwt_verification_hits", m.Search.JWTVerificationMinHits, len(hits))
	if len(hits) > limit {
		t.Fatalf("len(hits) = %d, want <= %d", len(hits), limit)
	}
}

func TestInit_Incremental(t *testing.T) {
	root, st, m := freshFixture(t)
	before, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}

	helpersPath := filepath.Join(root, "pkg/utils/helpers.go")
	testutil.TouchFile(t, helpersPath)

	_, err = index.Reindex(context.Background(), root, st, index.NewRegexExtractor())
	if err != nil {
		t.Fatal(err)
	}
	after, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}

	delta := after - before
	testutil.AssertEqualInt(t, "incremental_symbol_delta", 0, delta)
	_ = m
}

func TestRead_SymlinkEscape(t *testing.T) {
	root := testutil.CopyFixtureRepo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	r := testutil.NewReader(t, root)
	text, err := r.ReadLines("link.go", 1, 1)
	if !errors.Is(err, read.ErrPathOutsideRoot) {
		t.Fatalf("ReadLines() error = %v, want ErrPathOutsideRoot (text=%q)", err, text)
	}
	if text != "" {
		t.Fatalf("ReadLines() returned %d bytes, want 0", len(text))
	}
}

func TestRead_TraversalRejected(t *testing.T) {
	root := testutil.CopyFixtureRepo(t)
	r := testutil.NewReader(t, root)
	_, err := r.ReadLines("../"+filepath.Base(root)+"/src/billing/billing.go", 1, 1)
	if err == nil {
		t.Fatal("expected traversal error")
	}
	if !errors.Is(err, read.ErrPathOutsideRoot) && !errors.Is(err, read.ErrFileNotFound) {
		t.Fatalf("error = %v, want ErrPathOutsideRoot or ErrFileNotFound", err)
	}
}

func TestMetrics_CallersTokenBudget(t *testing.T) {
	_, st, m := freshFixture(t)
	hits, err := st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "callers_token_budget", m.Thresholds.CallersTokenBudget, tokens)
}

func TestMetrics_MapTokenBudget(t *testing.T) {
	_, st, m := freshFixture(t)
	result, err := st.MapDirectory(m.Map.HandlersDir, 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "map_token_budget", m.Thresholds.MapTokenBudget, tokens)
}

func TestMetrics_SearchTokenBudget(t *testing.T) {
	_, st, m := freshFixture(t)
	hits, err := st.Search("payment billing jwt", 10)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(string(data))
	testutil.AssertMaxInt(t, "search_token_budget", m.Thresholds.SearchTokenBudget, tokens)
}

func TestMetrics_ReadTokenBudget(t *testing.T) {
	root, _, m := freshFixture(t)
	slice := m.ReadSlice
	text, err := read.ReadLines(root, slice.FilePath, slice.StartLine, slice.EndLine)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(text)
	testutil.AssertMaxInt(t, "read_token_budget", m.Thresholds.ReadTokenBudget, tokens)
}

func TestMetrics_ReadVsWholeFile(t *testing.T) {
	root, _, m := freshFixture(t)
	slice := m.ReadSlice

	sliceText, err := read.ReadLines(root, slice.FilePath, slice.StartLine, slice.EndLine)
	if err != nil {
		t.Fatal(err)
	}
	wholePath := filepath.Join(root, filepath.FromSlash(slice.FilePath))
	wholeBytes, err := os.ReadFile(wholePath)
	if err != nil {
		t.Fatal(err)
	}

	sliceTokens := testutil.EstimateTokens(sliceText)
	wholeTokens := testutil.EstimateTokens(string(wholeBytes))
	if wholeTokens == 0 {
		t.Fatal("whole file token estimate is zero")
	}
	ratio := float64(sliceTokens) / float64(wholeTokens)
	testutil.AssertMaxFloat(t, "read_vs_whole_file_ratio", m.Thresholds.ReadVsWholeFileRatioMax, ratio)
}

func TestMetrics_SearchNoBodyLeak(t *testing.T) {
	_, st, _ := freshFixture(t)
	hits, err := st.Search("ProcessPayment", 10)
	if err != nil {
		t.Fatal(err)
	}
	bodyRe := regexp.MustCompile(`(?m)^\s*(for|if|return|\{|\})\s`)
	data, err := json.Marshal(hits)
	if err != nil {
		t.Fatal(err)
	}
	if bodyRe.Match(data) {
		t.Fatalf("search JSON appears to contain source body lines:\n%s", string(data))
	}
}

func TestMetrics_IndexSizeAbsolute(t *testing.T) {
	root, _, m := freshFixture(t)
	dbPath := filepath.Join(root, store.CacheDBName)
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMaxInt(t, "index_size_bytes", m.Thresholds.IndexSizeBytesMax, int(info.Size()))
}

func TestMetrics_IndexCompressionRatio(t *testing.T) {
	root, _, m := freshFixture(t)
	dbPath := filepath.Join(root, store.CacheDBName)
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes := testutil.TotalSourceBytes(t, root)
	if sourceBytes == 0 {
		t.Fatal("source bytes is zero")
	}
	ratio := float64(info.Size()) / float64(sourceBytes)
	testutil.AssertMaxFloat(t, "index_compression_ratio", m.Thresholds.IndexCompressionRatioMax, ratio)
}

func TestMetrics_SymbolDensity(t *testing.T) {
	_, st, m := freshFixture(t)
	files, err := st.FileCount()
	if err != nil {
		t.Fatal(err)
	}
	symbols, err := st.SymbolCount()
	if err != nil {
		t.Fatal(err)
	}
	density := float64(symbols) / float64(files)
	testutil.AssertMinInt(t, "symbol_density", int(m.Thresholds.SymbolDensityMin), int(density))
}

func TestMetrics_SearchLatency(t *testing.T) {
	_, st, m := freshFixture(t)
	queries := []string{
		"ProcessPayment", "jwt verification", "BillingService", "User",
		"ApiClient", "Migrate", "config", "handlers", "Slugify", "Verify",
	}
	for _, q := range queries {
		start := time.Now()
		if _, err := st.Search(q, 10); err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		testutil.AssertMaxDuration(t, "search_latency_"+q, m.Thresholds.SearchLatencyMs, time.Since(start).Milliseconds())
	}
}

func TestMetrics_ReadLatency(t *testing.T) {
	root, _, m := freshFixture(t)
	start := time.Now()
	_, err := read.ReadLines(root, "src/billing/billing.go", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertMaxDuration(t, "read_latency", m.Thresholds.ReadLatencyMs, time.Since(start).Milliseconds())
}

func TestMetrics_InitLatency(t *testing.T) {
	m := golden(t)
	root := testutil.CopyFixtureRepo(t)
	_ = os.Remove(filepath.Join(root, store.CacheDBName))

	start := time.Now()
	testutil.InitFixture(t, root)
	testutil.AssertMaxDuration(t, "init_latency", m.Thresholds.InitLatencyMs, time.Since(start).Milliseconds())
}

func TestMetrics_HydrationLatency(t *testing.T) {
	root, st, m := freshFixture(t)
	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())

	start := time.Now()
	elapsed, err := coord.Hydrate(context.Background())
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if elapsed < 0 {
		t.Fatalf("unexpected negative hydration duration: %v", elapsed)
	}
	testutil.AssertMaxDuration(t, "hydration_ms", m.Thresholds.HydrationMs, elapsed.Milliseconds())
	// Wall clock should be in the same ballpark (Hydrate is synchronous).
	testutil.AssertMaxDuration(t, "hydration_wall_ms", m.Thresholds.HydrationMs, time.Since(start).Milliseconds())
}

func TestMetrics_IncrementalLatency(t *testing.T) {
	root, st, m := freshFixture(t)
	testutil.TouchFile(t, filepath.Join(root, "pkg/utils/helpers.go"))

	start := time.Now()
	if _, err := index.Reindex(context.Background(), root, st, index.NewRegexExtractor()); err != nil {
		t.Fatal(err)
	}
	testutil.AssertMaxDuration(t, "incremental_latency", m.Thresholds.IncrementalLatencyMs, time.Since(start).Milliseconds())
}

func TestMetrics_ReindexUnderLoad(t *testing.T) {
	root, st, _ := freshFixture(t)
	testutil.TouchFile(t, filepath.Join(root, "src/models/user.go"))

	var wg sync.WaitGroup
	errCh := make(chan error, 11)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := index.Reindex(context.Background(), root, st, index.NewRegexExtractor()); err != nil {
			errCh <- err
		}
	}()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := st.Search("User", 10); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestInit_SummaryOutput(t *testing.T) {
	m := golden(t)
	root := testutil.CopyFixtureRepo(t)
	bin := buildBinary(t)

	cmd := exec.Command(bin, "init", "--root", root)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	line := strings.TrimSpace(lastLine(string(out)))
	re := regexp.MustCompile(`^files=(\d+) symbols=(\d+) indexer=(ctags|regex) elapsed_ms=(\d+) db_bytes=(\d+)$`)
	parts := re.FindStringSubmatch(line)
	if parts == nil {
		t.Fatalf("summary line %q does not match expected format", line)
	}

	files, _ := strconv.Atoi(parts[1])
	symbols, _ := strconv.Atoi(parts[2])
	dbBytes, _ := strconv.Atoi(parts[5])

	testutil.AssertEqualInt(t, "init_summary_files", m.FilesIndexed, files)
	testutil.AssertMinInt(t, "init_summary_symbols", m.SymbolsIndexedMin, symbols)

	wantBytes := float64(m.InitDBBytes)
	gotBytes := float64(dbBytes)
	tolerance := m.Thresholds.InitDBBytesTolerancePct
	if wantBytes > 0 {
		diff := (gotBytes - wantBytes) / wantBytes
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			t.Fatalf("init db_bytes = %d, want within %.0f%% of %d", dbBytes, tolerance*100, int(wantBytes))
		}
	}
}

func TestMCP_FixtureMapDirectory(t *testing.T) {
	root, st, m := freshFixture(t)
	reader := testutil.NewReader(t, root)
	_, session, cleanup := connectMCPSession(t, root, st, reader)
	defer cleanup()

	text := callToolText(t, session, "map_directory", map[string]any{
		"dir": m.Map.HandlersDir,
	})
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, key := range []string{"dir", "definitions", "outgoing_calls", "definition_count", "outgoing_call_count"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
	if result["dir"] != m.Map.HandlersDir {
		t.Fatalf("dir = %v, want %q", result["dir"], m.Map.HandlersDir)
	}

	defs, ok := result["definitions"].([]any)
	if !ok {
		t.Fatalf("definitions is not array: %T", result["definitions"])
	}
	testutil.AssertMinInt(t, "handlers_defs", m.Map.DefsMin, int(result["definition_count"].(float64)))

	var handleCharge bool
	for _, d := range defs {
		entry, ok := d.(map[string]any)
		if !ok {
			continue
		}
		if entry["symbol"] == m.Map.HandleChargeSymbol {
			handleCharge = true
			break
		}
	}
	if !handleCharge {
		t.Fatalf("HandleCharge not in definitions: %+v", defs)
	}

	calls, ok := result["outgoing_calls"].([]any)
	if !ok {
		t.Fatalf("outgoing_calls is not array: %T", result["outgoing_calls"])
	}
	callees := make(map[string]bool)
	for _, c := range calls {
		entry, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := entry["callee_name"].(string); ok {
			callees[name] = true
		}
	}
	for _, want := range m.Map.OutgoingCallees {
		if !callees[want] {
			t.Fatalf("outgoing calls missing %q; got %+v", want, calls)
		}
	}
}

func TestMCP_FixtureFindCallers(t *testing.T) {
	root, st, m := freshFixture(t)
	reader := testutil.NewReader(t, root)
	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())
	_, session, cleanup := connectMCPSession(t, root, st, reader, coord)
	defer cleanup()

	text := callToolText(t, session, "find_callers", map[string]any{
		"name": "ProcessPayment",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	testutil.AssertMinInt(t, "process_payment_callers", m.Callers.ProcessPaymentMinHits, len(hits))

	want := m.Callers.ProcessPayment
	var match map[string]any
	for _, hit := range hits {
		if hit["file_path"] == want.FilePath && hit["enclosing_symbol"] == want.EnclosingSymbol {
			match = hit
			break
		}
	}
	if match == nil {
		t.Fatalf("no caller hit in %s with enclosing %q; got %+v", want.FilePath, want.EnclosingSymbol, hits)
	}

	required := []string{
		"callee_name", "file_path", "line", "col",
		"enclosing_symbol", "enclosing_kind", "enclosing_scope", "enclosing_symbol_id",
	}
	for _, key := range required {
		if _, ok := match[key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
	if match["callee_name"] != "ProcessPayment" {
		t.Fatalf("callee_name = %v, want ProcessPayment", match["callee_name"])
	}
	if int(match["line"].(float64)) != want.Line {
		t.Fatalf("line = %v, want %d", match["line"], want.Line)
	}
	if int(match["col"].(float64)) != want.Col {
		t.Fatalf("col = %v, want %d", match["col"], want.Col)
	}
	if match["enclosing_symbol_id"] != want.EnclosingSymbolID {
		t.Fatalf("enclosing_symbol_id = %v, want %q", match["enclosing_symbol_id"], want.EnclosingSymbolID)
	}
}

func TestMCP_FixtureSearchSchema(t *testing.T) {
	root, st, _ := freshFixture(t)
	reader := testutil.NewReader(t, root)
	ctx, session, cleanup := connectMCPSession(t, root, st, reader)
	defer cleanup()
	_ = ctx

	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query": "ProcessPayment",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	required := []string{"file_path", "symbol", "kind", "start_line", "end_line", "scope", "matched_in", "symbol_id"}
	for _, key := range required {
		if _, ok := hits[0][key]; !ok {
			t.Fatalf("missing key %q", key)
		}
	}
	wantID := store.FormatSymbolID(store.SymbolRecord{
		FilePath:  "src/billing/billing.go",
		Kind:      hits[0]["kind"].(string),
		Name:      "ProcessPayment",
		StartLine: int(hits[0]["start_line"].(float64)),
	})
	if hits[0]["symbol_id"] != wantID {
		t.Fatalf("symbol_id = %v, want %q", hits[0]["symbol_id"], wantID)
	}
}

func TestMCP_FixtureNameMatchRoundTrip(t *testing.T) {
	root, st, m := freshFixture(t)
	reader := testutil.NewReader(t, root)
	_, session, cleanup := connectMCPSession(t, root, st, reader)
	defer cleanup()

	searchText := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query":      "ProcessPayment",
		"name_match": "exact",
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(searchText), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	want := m.Search.ProcessPayment
	if hits[0]["symbol"] != want.Symbol {
		t.Fatalf("symbol = %v, want %q", hits[0]["symbol"], want.Symbol)
	}

	symbolID, ok := hits[0]["symbol_id"].(string)
	if !ok || symbolID == "" {
		t.Fatalf("missing symbol_id: %#v", hits[0])
	}

	readText := callToolText(t, session, "read_symbol", map[string]any{
		"symbol_id": symbolID,
	})
	if !strings.Contains(readText, "func ProcessPayment(") {
		t.Fatalf("read_symbol body = %q, want ProcessPayment definition", readText)
	}

	lineText := callToolText(t, session, "read_file_lines", map[string]any{
		"file_path":  hits[0]["file_path"],
		"start_line": int(hits[0]["start_line"].(float64)),
		"end_line":   int(hits[0]["end_line"].(float64)),
	})
	if readText != lineText {
		t.Fatalf("read_symbol and read_file_lines differ:\nread_symbol:\n%s\nread_file_lines:\n%s", readText, lineText)
	}
}

func TestMCP_FixtureSearchLimit(t *testing.T) {
	root, st, _ := freshFixture(t)
	reader := testutil.NewReader(t, root)
	_, session, cleanup := connectMCPSession(t, root, st, reader)
	defer cleanup()

	text := callToolText(t, session, "search_code_skeleton", map[string]any{
		"query": "func",
		"limit": 3,
	})
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) > 3 {
		t.Fatalf("len(hits) = %d, want <= 3", len(hits))
	}
}

func TestMCP_FixtureReadLineFormat(t *testing.T) {
	root, st, m := freshFixture(t)
	reader := testutil.NewReader(t, root)
	_, session, cleanup := connectMCPSession(t, root, st, reader)
	defer cleanup()

	slice := m.ReadSlice
	text := callToolText(t, session, "read_file_lines", map[string]any{
		"file_path":  slice.FilePath,
		"start_line": slice.StartLine,
		"end_line":   slice.EndLine,
	})
	lineRe := regexp.MustCompile(`^\d+\t`)
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if !lineRe.MatchString(line) {
			t.Fatalf("line missing number prefix: %q", line)
		}
	}
}

func connectMCPSession(t *testing.T, root string, st *store.Store, reader *read.Reader, coord ...*index.SyncCoordinator) (context.Context, *mcpsdk.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	cfg := litosmcp.Config{
		RepoRoot: root,
		Store:    st,
		Reader:   reader,
		Version:  "test",
	}
	if len(coord) > 0 {
		cfg.Coordinator = coord[0]
	}
	server := litosmcp.NewServer(cfg)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, clientSession, func() {
		clientSession.Close()
		serverSession.Close()
		serverSession.Wait()
	}
}

func callToolText(t *testing.T, session *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s tool error: %v", name, res.Content)
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	return text.Text
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
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
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

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

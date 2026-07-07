//go:build treesitter

package testutil_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/testutil"
)

func freshFixtureTreesitter(t *testing.T) (string, *store.Store, testutil.GoldenMetrics) {
	t.Helper()
	m := golden(t)
	root := testutil.CopyFixtureRepo(t)
	st, _ := testutil.InitFixtureWithExtractor(t, root, index.NewExtractor())
	return root, st, m
}

func TestReadSymbol_ProcessPaymentIsolation(t *testing.T) {
	root, st, _ := freshFixtureTreesitter(t)
	rec, err := st.GetSymbolByID("src/billing/billing.go#function#ProcessPayment#56")
	if err != nil {
		t.Fatal(err)
	}
	if rec.StartByte < 0 || rec.EndByte <= rec.StartByte {
		t.Fatalf("expected byte boundaries on ProcessPayment, got start=%d end=%d", rec.StartByte, rec.EndByte)
	}

	r := testutil.NewReader(t, root)
	text, err := r.ReadSymbol(rec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "func ProcessPayment(") {
		t.Fatalf("ReadSymbol() missing ProcessPayment header: %q", text)
	}
	if strings.Contains(text, "RefundPayment") {
		t.Fatalf("ReadSymbol() leaked RefundPayment: %q", text)
	}
}

func TestMCP_ReadSymbol_BytePrecision(t *testing.T) {
	root, st, _ := freshFixtureTreesitter(t)
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
	if strings.Contains(readText, "RefundPayment") {
		t.Fatalf("read_symbol leaked RefundPayment: %q", readText)
	}

	rec, err := st.GetSymbolByID(symbolID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.StartByte < 0 {
		t.Fatal("expected byte boundaries on indexed ProcessPayment")
	}
	lineText, err := reader.ReadLines(rec.FilePath, rec.StartLine, rec.EndLine)
	if err != nil {
		t.Fatal(err)
	}
	if readText != lineText {
		t.Fatalf("read_symbol and line range differ:\nread_symbol:\n%s\nlines:\n%s", readText, lineText)
	}
}

func TestMetrics_TreesitterRefineLatency(t *testing.T) {
	m := golden(t)
	root := testutil.CopyFixtureRepo(t)
	_ = os.Remove(filepath.Join(root, store.CacheDBName))

	start := time.Now()
	testutil.InitFixtureWithExtractor(t, root, index.NewExtractor())
	testutil.AssertMaxDuration(t, "treesitter_refine_ms", m.Thresholds.TreesitterRefineMs, time.Since(start).Milliseconds())
}

func TestMetrics_ReadSymbolTokenBudget(t *testing.T) {
	root, st, m := freshFixtureTreesitter(t)
	rec, err := st.GetSymbolByID("src/billing/billing.go#function#ProcessPayment#56")
	if err != nil {
		t.Fatal(err)
	}

	text, err := read.ReadSymbol(root, rec)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testutil.EstimateTokens(text)
	testutil.AssertMaxInt(t, "read_token_budget", m.Thresholds.ReadTokenBudget, tokens)
}

func TestFixtureFindCallers_TreesitterSkipsDeclarationFalsePositive(t *testing.T) {
	_, st, m := freshFixtureTreesitter(t)
	hits, err := st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.FilePath == "src/billing/billing.go" && h.Line == 56 {
			t.Fatalf("tree-sitter should not index func declaration as call site: %+v", h)
		}
	}
	want := m.Callers.ProcessPayment
	found := false
	for _, h := range hits {
		if h.FilePath == want.FilePath && h.EnclosingSymbol == want.EnclosingSymbol {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing real caller in %s (HandleCharge); got %+v", want.FilePath, hits)
	}
}

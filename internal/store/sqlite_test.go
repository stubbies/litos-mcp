package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbies/litos-mcp/internal/store"

	_ "modernc.org/sqlite"
)

func TestProbeFTS5_InMemory(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := store.ProbeFTS5(db); err != nil {
		t.Fatalf("FTS5 probe failed: %v", err)
	}
}

func TestOpen_CreatesCacheDBWithFTS5(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	want := filepath.Join(dir, store.CacheDBName)
	if st.Path() != want {
		t.Fatalf("Path() = %q, want %q", st.Path(), want)
	}

	if err := store.ProbeFTS5(st.DB()); err != nil {
		t.Fatalf("FTS5 probe on opened store failed: %v", err)
	}
}

func TestFTS5_InsertAndSearch(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	meta := store.FileMeta{Path: "src/billing.go", MtimeNs: 1, Size: 100}
	symbols := []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", Scope: "BillingService", StartLine: 45, EndLine: 75},
		{Name: "RefundPayment", Kind: "function", Scope: "BillingService", StartLine: 80, EndLine: 95},
	}
	if err := st.UpsertFile(meta, symbols); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	hits, err := st.Search("ProcessPayment", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	h := hits[0]
	if h.FilePath != "src/billing.go" || h.Symbol != "ProcessPayment" || h.Kind != "function" ||
		h.StartLine != 45 || h.EndLine != 75 || h.Scope != "BillingService" {
		t.Fatalf("unexpected hit: %+v", h)
	}
	wantID := "src/billing.go#function#ProcessPayment#45"
	if h.SymbolID != wantID {
		t.Fatalf("symbol_id = %q, want %q", h.SymbolID, wantID)
	}
}

func TestFTS5_TriggersSyncOnUpdateAndDelete(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	meta := store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 10}
	if err := st.UpsertFile(meta, []store.SymbolRecord{
		{Name: "Alpha", Kind: "function", StartLine: 1, EndLine: 5},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("Alpha", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search Alpha: hits=%v err=%v", hits, err)
	}

	// Re-upsert with renamed symbol to exercise UPDATE trigger path via delete+insert.
	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 2, Size: 10}, []store.SymbolRecord{
		{Name: "Beta", Kind: "function", StartLine: 1, EndLine: 5},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err = st.Search("Alpha", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("Alpha should be gone after reindex, got %d hits", len(hits))
	}

	hits, err = st.Search("Beta", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search Beta: hits=%v err=%v", hits, err)
	}

	if err := st.RemoveFile("a.go"); err != nil {
		t.Fatal(err)
	}
	hits, err = st.Search("Beta", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("Beta should be gone after RemoveFile, got %d hits", len(hits))
	}
}

func TestUpsertFile_PerFileAtomicity(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "x.go", MtimeNs: 100, Size: 50}, []store.SymbolRecord{
		{Name: "Foo", Kind: "function", StartLine: 1, EndLine: 3},
		{Name: "Bar", Kind: "type", StartLine: 10, EndLine: 20},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := st.SymbolCount()
	if err != nil || n != 2 {
		t.Fatalf("SymbolCount = %d, err = %v, want 2", n, err)
	}

	// Replace symbols entirely in one upsert.
	if err := st.UpsertFile(store.FileMeta{Path: "x.go", MtimeNs: 200, Size: 60}, []store.SymbolRecord{
		{Name: "Baz", Kind: "function", StartLine: 5, EndLine: 8},
	}); err != nil {
		t.Fatal(err)
	}

	n, err = st.SymbolCount()
	if err != nil || n != 1 {
		t.Fatalf("SymbolCount after replace = %d, want 1", n)
	}

	meta, ok, err := st.GetFileMeta("x.go")
	if err != nil || !ok {
		t.Fatalf("GetFileMeta: meta=%+v ok=%v err=%v", meta, ok, err)
	}
	if meta.MtimeNs != 200 || meta.Size != 60 {
		t.Fatalf("file meta not updated: %+v", meta)
	}
}

func TestIncremental_IsStale(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	stale, err := st.IsStale("main.go", 999, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("unindexed file should be stale")
	}

	if err := st.UpsertFile(store.FileMeta{Path: "main.go", MtimeNs: 1000, Size: 42}, nil); err != nil {
		t.Fatal(err)
	}

	stale, err = st.IsStale("main.go", 1000, 42)
	if err != nil || stale {
		t.Fatalf("matching mtime/size should not be stale: stale=%v err=%v", stale, err)
	}

	stale, err = st.IsStale("main.go", 1001, 42)
	if err != nil || !stale {
		t.Fatalf("changed mtime should be stale: stale=%v err=%v", stale, err)
	}

	stale, err = st.IsStale("main.go", 1000, 99)
	if err != nil || !stale {
		t.Fatalf("changed size should be stale: stale=%v err=%v", stale, err)
	}
}

func TestSearch_MultiToken(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "auth/jwt.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "VerifyToken", Kind: "function", Scope: "jwt verification", StartLine: 10, EndLine: 30},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("jwt verification", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) < 1 {
		t.Fatalf("expected at least 1 hit for multi-token query, got %d", len(hits))
	}
}

func TestSearch_LimitRespected(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	symbols := make([]store.SymbolRecord, 0, 5)
	for i := range 5 {
		symbols = append(symbols, store.SymbolRecord{
			Name: "HelperFunc", Kind: "function", StartLine: i + 1, EndLine: i + 2,
		})
	}
	if err := st.UpsertFile(store.FileMeta{Path: "helpers.go", MtimeNs: 1, Size: 1}, symbols); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("HelperFunc", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 3 {
		t.Fatalf("limit not respected: got %d hits", len(hits))
	}
}

func TestSearch_LikeFallback(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "weird.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "FuncWith*Star", Kind: "function", StartLine: 1, EndLine: 2},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("*Star", 10)
	if err != nil {
		t.Fatalf("Search with special chars: %v", err)
	}
	if len(hits) != 1 || hits[0].Symbol != "FuncWith*Star" {
		t.Fatalf("LIKE fallback failed: %+v", hits)
	}
}

func TestSearch_EmptyScopeUsesCOALESCE(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "plain.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "NoScopeFn", Kind: "function", Scope: "", StartLine: 1, EndLine: 1},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("NoScopeFn", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search empty scope symbol: hits=%v err=%v", hits, err)
	}
	if hits[0].Scope != "" {
		t.Fatalf("scope = %q, want empty string", hits[0].Scope)
	}
}

func TestWAL_ConcurrentReadDuringWrite(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Writer: continuously upsert files.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 50 {
			path := filepath.ToSlash(filepath.Join("pkg", "file.go"))
			meta := store.FileMeta{Path: path, MtimeNs: int64(i + 1), Size: int64(i)}
			syms := []store.SymbolRecord{
				{Name: "Worker", Kind: "function", StartLine: 1, EndLine: 2},
			}
			if err := st.UpsertFile(meta, syms); err != nil {
				errCh <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Readers: search while writes happen.
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := st.Search("Worker", 5); err != nil {
					errCh <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent op failed: %v", err)
	}
}

func TestListFilesAndRemoveFile(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(store.FileMeta{Path: "b.go", MtimeNs: 2, Size: 2}, nil); err != nil {
		t.Fatal(err)
	}

	files, err := st.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("ListFiles = %d, want 2", len(files))
	}

	if err := st.RemoveFile("a.go"); err != nil {
		t.Fatal(err)
	}
	n, err := st.FileCount()
	if err != nil || n != 1 {
		t.Fatalf("FileCount after remove = %d, want 1", n)
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		in, mode, want string
	}{
		{"", "and", `""`},
		{"  foo  ", "and", `"foo"`},
		{"jwt verification", "and", `"jwt" "verification"`},
		{"jwt verification", "or", `"jwt" OR "verification"`},
	}
	for _, tc := range tests {
		got := store.SanitizeFTSQuery(tc.in, tc.mode)
		if got != tc.want {
			t.Errorf("SanitizeFTSQuery(%q, %q) = %q, want %q", tc.in, tc.mode, got, tc.want)
		}
	}
}

func TestSearch_MatchModeOR(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "AlphaFunc", Kind: "function", StartLine: 1, EndLine: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(store.FileMeta{Path: "b.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "BetaFunc", Kind: "function", StartLine: 1, EndLine: 5},
	}); err != nil {
		t.Fatal(err)
	}

	// AND mode: no symbol contains both tokens.
	hits, err := st.SearchWithOptions("Alpha Beta", 10, store.SearchOptions{MatchMode: "and"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("AND mode: got %d hits, want 0", len(hits))
	}

	// OR mode: each token matches a different symbol.
	hits, err = st.SearchWithOptions("Alpha Beta", 10, store.SearchOptions{MatchMode: "or"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("OR mode: got %d hits, want 2", len(hits))
	}
}

func TestSearch_EmptyResultFallback(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "src/billing.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", StartLine: 10, EndLine: 20},
	}); err != nil {
		t.Fatal(err)
	}

	// Partial prefix won't match FTS token but LIKE fallback should.
	hits, err := st.Search("Proc", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Symbol != "ProcessPayment" {
		t.Fatalf("symbol = %q, want ProcessPayment", hits[0].Symbol)
	}
	if hits[0].MatchedIn != "symbol" {
		t.Fatalf("matched_in = %q, want symbol", hits[0].MatchedIn)
	}
}

func TestSearch_MatchedInPath(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "internal/auth/jwt.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "Verify", Kind: "function", StartLine: 5, EndLine: 15},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("jwt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].MatchedIn != "path" {
		t.Fatalf("matched_in = %q, want path", hits[0].MatchedIn)
	}
}

func TestSearch_MatchedInKind(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "models.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "User", Kind: "interface", Scope: "models", StartLine: 1, EndLine: 10},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search("interface", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].MatchedIn != "kind" {
		t.Fatalf("matched_in = %q, want kind", hits[0].MatchedIn)
	}
}

func TestFormatAndParseSymbolID(t *testing.T) {
	rec := store.SymbolRecord{
		FilePath:  "src/billing/billing.go",
		Kind:      "function",
		Name:      "ProcessPayment",
		StartLine: 56,
	}
	id := store.FormatSymbolID(rec)
	want := "src/billing/billing.go#function#ProcessPayment#56"
	if id != want {
		t.Fatalf("FormatSymbolID = %q, want %q", id, want)
	}

	parsed, err := store.ParseSymbolID(id)
	if err != nil {
		t.Fatalf("ParseSymbolID: %v", err)
	}
	if parsed.FilePath != rec.FilePath || parsed.Kind != rec.Kind ||
		parsed.Name != rec.Name || parsed.StartLine != rec.StartLine {
		t.Fatalf("parsed = %+v, want %+v", parsed, rec)
	}
}

func TestParseSymbolID_HashInPath(t *testing.T) {
	id := "src/foo#bar/baz.go#function#Fn#10"
	parsed, err := store.ParseSymbolID(id)
	if err != nil {
		t.Fatalf("ParseSymbolID: %v", err)
	}
	if parsed.FilePath != "src/foo#bar/baz.go" || parsed.Name != "Fn" || parsed.StartLine != 10 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseSymbolID_Invalid(t *testing.T) {
	cases := []string{"", "no-separators", "a#b#c", "a#b#c#notint"}
	for _, id := range cases {
		if _, err := store.ParseSymbolID(id); err == nil {
			t.Errorf("ParseSymbolID(%q) expected error", id)
		}
	}
}

func TestGetSymbolByID(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	meta := store.FileMeta{Path: "src/billing.go", MtimeNs: 1, Size: 100}
	symbols := []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", Scope: "BillingService", StartLine: 45, EndLine: 75},
	}
	if err := st.UpsertFile(meta, symbols); err != nil {
		t.Fatal(err)
	}

	id := "src/billing.go#function#ProcessPayment#45"
	rec, err := st.GetSymbolByID(id)
	if err != nil {
		t.Fatalf("GetSymbolByID: %v", err)
	}
	if rec.Name != "ProcessPayment" || rec.EndLine != 75 || rec.Scope != "BillingService" {
		t.Fatalf("unexpected record: %+v", rec)
	}

	if _, err := st.GetSymbolByID("bad-id"); err == nil {
		t.Fatal("expected error for invalid id")
	}
	if _, err := st.GetSymbolByID("src/billing.go#function#Gone#99"); err == nil {
		t.Fatal("expected error for stale id")
	}
}

func TestListSymbolsByFile(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "x.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "Beta", Kind: "function", StartLine: 20, EndLine: 25},
		{Name: "Alpha", Kind: "type", StartLine: 5, EndLine: 10},
	}); err != nil {
		t.Fatal(err)
	}

	symbols, err := st.ListSymbolsByFile("x.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 {
		t.Fatalf("got %d symbols, want 2", len(symbols))
	}
	if symbols[0].Name != "Alpha" || symbols[1].Name != "Beta" {
		t.Fatalf("order by start_line failed: %+v", symbols)
	}

	empty, err := st.ListSymbolsByFile("missing.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("unindexed file should return empty slice, got %d", len(empty))
	}
}

func TestSearch_NameMatchExact(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", StartLine: 10, EndLine: 20},
		{Name: "ProcessPaymentHelper", Kind: "function", StartLine: 25, EndLine: 30},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(store.FileMeta{Path: "b.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "processpayment", Kind: "function", StartLine: 5, EndLine: 8},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchWithOptions("ProcessPayment", 10, store.SearchOptions{NameMatch: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("exact match: got %d hits, want 1", len(hits))
	}
	if hits[0].Symbol != "ProcessPayment" || hits[0].FilePath != "a.go" {
		t.Fatalf("unexpected hit: %+v", hits[0])
	}
	if hits[0].MatchedIn != "symbol" {
		t.Fatalf("matched_in = %q, want symbol", hits[0].MatchedIn)
	}
	wantID := "a.go#function#ProcessPayment#10"
	if hits[0].SymbolID != wantID {
		t.Fatalf("symbol_id = %q, want %q", hits[0].SymbolID, wantID)
	}
}

func TestSearch_NameMatchExactCaseSensitive(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "processpayment", Kind: "function", StartLine: 1, EndLine: 2},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchWithOptions("ProcessPayment", 10, store.SearchOptions{NameMatch: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("case-sensitive exact: got %d hits, want 0", len(hits))
	}
}

func TestSearch_NameMatchContains(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "a.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", StartLine: 10, EndLine: 20},
		{Name: "ProcessPaymentHelper", Kind: "function", StartLine: 25, EndLine: 30},
		{Name: "Refund", Kind: "function", StartLine: 40, EndLine: 45},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchWithOptions("Process", 10, store.SearchOptions{NameMatch: "contains"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("contains match: got %d hits, want 2", len(hits))
	}
	for _, h := range hits {
		if !strings.Contains(h.Symbol, "Process") {
			t.Fatalf("unexpected symbol %q", h.Symbol)
		}
		if h.SymbolID == "" {
			t.Fatalf("missing symbol_id on hit %+v", h)
		}
	}
}

func TestSearch_NameMatchContainsSpecialChars(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "weird.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "FuncWith*Star", Kind: "function", StartLine: 1, EndLine: 2},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchWithOptions("*Star", 10, store.SearchOptions{NameMatch: "contains"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Symbol != "FuncWith*Star" {
		t.Fatalf("contains with special chars failed: %+v", hits)
	}
}

func TestSearch_NameMatchRoundTrip(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertFile(store.FileMeta{Path: "src/billing.go", MtimeNs: 1, Size: 1}, []store.SymbolRecord{
		{Name: "ProcessPayment", Kind: "function", Scope: "BillingService", StartLine: 45, EndLine: 75},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchWithOptions("ProcessPayment", 10, store.SearchOptions{NameMatch: "exact"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}

	rec, err := st.GetSymbolByID(hits[0].SymbolID)
	if err != nil {
		t.Fatalf("GetSymbolByID: %v", err)
	}
	if rec.Name != "ProcessPayment" || rec.EndLine != 75 {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

package query_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stubbies/litos-mcp/internal/index"
	"github.com/stubbies/litos-mcp/internal/query"
	"github.com/stubbies/litos-mcp/internal/read"
	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/testutil"
)

func setupQueryFixture(t *testing.T) (context.Context, *store.Store, *read.Reader, *index.SyncCoordinator, func()) {
	t.Helper()

	root := testutil.CopyFixtureRepo(t)
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Reindex(context.Background(), root, st, index.NewRegexExtractor()); err != nil {
		t.Fatal(err)
	}

	reader, err := read.New(root)
	if err != nil {
		t.Fatal(err)
	}

	coord := index.NewSyncCoordinator(root, st, index.NewRegexExtractor())

	cleanup := func() {
		st.Close()
	}
	return context.Background(), st, reader, coord, cleanup
}

func TestSearch_ProcessPayment(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	hits, err := query.Search(ctx, st, coord, query.SearchOpts{
		Query: "ProcessPayment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Symbol != "ProcessPayment" {
		t.Fatalf("symbol = %q, want ProcessPayment", hits[0].Symbol)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	hits, err := query.Search(ctx, st, coord, query.SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestOutline_BillingFile(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	entries, err := query.Outline(ctx, st, coord, "src/billing/billing.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 1 {
		t.Fatalf("len(entries) = %d, want >= 1", len(entries))
	}

	var found bool
	for _, e := range entries {
		if e.Symbol == "ProcessPayment" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ProcessPayment not in outline")
	}
}

func TestReadSymbol_RoundTrip(t *testing.T) {
	ctx, st, reader, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	hits, err := query.Search(ctx, st, coord, query.SearchOpts{
		Query:      "ProcessPayment",
		NameMatch:  "exact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatal("expected 1 search hit")
	}

	text, err := query.ReadSymbol(ctx, st, reader, hits[0].SymbolID)
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Fatal("expected non-empty symbol text")
	}
}

func TestFindCallers_ProcessPayment(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	result, err := query.FindCallers(ctx, st, coord, query.FindCallersOpts{
		Name: "ProcessPayment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) < 1 {
		t.Fatalf("len(hits) = %d, want >= 1", len(result.Hits))
	}

	var found bool
	for _, hit := range result.Hits {
		if hit.EnclosingSymbol == "HandleCharge" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected HandleCharge as enclosing symbol")
	}
}

func TestFindCallers_NoHits(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	_, err := query.FindCallers(ctx, st, coord, query.FindCallersOpts{
		Name: "NobodyCallsThis",
	})
	if !errors.Is(err, query.ErrNoCallers) {
		t.Fatalf("err = %v, want ErrNoCallers", err)
	}
}

func TestFindCallers_MissingInput(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	_, err := query.FindCallers(ctx, st, coord, query.FindCallersOpts{})
	if err == nil {
		t.Fatal("expected error for missing name and symbol_id")
	}
}

func TestMapDirectory_Handlers(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	result, err := query.MapDirectory(ctx, st, coord, query.MapDirectoryOpts{
		Dir: "src/handlers",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dir != "src/handlers" {
		t.Fatalf("dir = %q, want src/handlers", result.Dir)
	}
	if result.DefinitionCount < 3 {
		t.Fatalf("definition_count = %d, want >= 3", result.DefinitionCount)
	}

	var handleCharge bool
	for _, def := range result.Definitions {
		if def.Symbol == "HandleCharge" {
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
	if !callees["ProcessPayment"] {
		t.Fatalf("ProcessPayment not in outgoing calls: %+v", result.OutgoingCalls)
	}
	if !callees["RefundPayment"] {
		t.Fatalf("RefundPayment not in outgoing calls: %+v", result.OutgoingCalls)
	}
}

func TestMapDirectory_MissingDir(t *testing.T) {
	ctx, st, _, coord, cleanup := setupQueryFixture(t)
	defer cleanup()

	_, err := query.MapDirectory(ctx, st, coord, query.MapDirectoryOpts{})
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

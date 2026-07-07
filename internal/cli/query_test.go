package cli_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stubbies/litos-mcp/internal/store"
	"github.com/stubbies/litos-mcp/internal/testutil"
)

func TestQuery_SearchJSON(t *testing.T) {
	root, bin := setupQueryCLI(t)

	out := runCLI(t, bin, root, "search", "ProcessPayment", "--name-match", "exact", "--json")
	var hits []store.SearchHit
	if err := json.Unmarshal(out, &hits); err != nil {
		t.Fatalf("parse search JSON: %v\n%s", err, out)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Symbol != "ProcessPayment" {
		t.Fatalf("symbol = %q, want ProcessPayment", hits[0].Symbol)
	}
}

func TestQuery_OutlineJSON(t *testing.T) {
	root, bin := setupQueryCLI(t)

	out := runCLI(t, bin, root, "outline", "src/billing/billing.go", "--json")
	var entries []store.OutlineEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("parse outline JSON: %v\n%s", err, out)
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

func TestQuery_ReadSymbolText(t *testing.T) {
	root, bin := setupQueryCLI(t)

	searchOut := runCLI(t, bin, root, "search", "ProcessPayment", "--name-match", "exact", "--json")
	var hits []store.SearchHit
	if err := json.Unmarshal(searchOut, &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatal("expected 1 search hit")
	}

	out := runCLI(t, bin, root, "read-symbol", hits[0].SymbolID)
	text := strings.TrimSpace(string(out))
	if text == "" {
		t.Fatal("expected non-empty symbol text")
	}
	if !strings.Contains(text, "ProcessPayment") {
		t.Fatalf("symbol text %q missing ProcessPayment", text)
	}
}

func TestQuery_FindCallersJSON(t *testing.T) {
	root, bin := setupQueryCLI(t)

	out := runCLI(t, bin, root, "find-callers", "ProcessPayment", "--json")
	var hits []store.CallerHit
	if err := json.Unmarshal(out, &hits); err != nil {
		t.Fatalf("parse callers JSON: %v\n%s", err, out)
	}
	if len(hits) < 1 {
		t.Fatalf("len(hits) = %d, want >= 1", len(hits))
	}

	var found bool
	for _, hit := range hits {
		if hit.EnclosingSymbol == "HandleCharge" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected HandleCharge as enclosing symbol")
	}
}

func TestQuery_MapDirJSON(t *testing.T) {
	root, bin := setupQueryCLI(t)

	out := runCLI(t, bin, root, "map-dir", "src/handlers", "--json")
	var result store.DirectoryMap
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("parse map-dir JSON: %v\n%s", err, out)
	}
	if result.Dir != "src/handlers" {
		t.Fatalf("dir = %q, want src/handlers", result.Dir)
	}
	if result.DefinitionCount < 3 {
		t.Fatalf("definition_count = %d, want >= 3", result.DefinitionCount)
	}

	callees := make(map[string]bool)
	for _, call := range result.OutgoingCalls {
		callees[call.CalleeName] = true
	}
	if !callees["ProcessPayment"] {
		t.Fatalf("ProcessPayment not in outgoing calls")
	}
}

func TestQuery_MissingCache(t *testing.T) {
	root := testutil.CopyFixtureRepo(t)
	bin := buildBinary(t)

	cmd := exec.Command(bin, "search", "foo", "--root", root, "--json")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error when cache missing, got: %s", out)
	}
	if !strings.Contains(string(out), "index cache missing") {
		t.Fatalf("unexpected error: %s", out)
	}
}

func setupQueryCLI(t *testing.T) (string, string) {
	t.Helper()
	root := testutil.CopyFixtureRepo(t)
	bin := buildBinary(t)

	initCmd := exec.Command(bin, "init", "--root", root)
	initCmd.Dir = root
	initOut, err := initCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init fixture: %v\n%s", err, initOut)
	}
	return root, bin
}

func runCLI(t *testing.T, bin, root string, args ...string) []byte {
	t.Helper()
	all := append(args, "--root", root)
	cmd := exec.Command(bin, all...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("%s failed: %v\nstderr: %s", strings.Join(args, " "), err, ee.Stderr)
		}
		t.Fatalf("%s failed: %v", strings.Join(args, " "), err)
	}
	return out
}

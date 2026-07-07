//go:build treesitter

package treesitter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/index/treesitter"
	"github.com/stubbies/litos-mcp/internal/store"
)

func TestRefineBoundaries_GoNestedFunction(t *testing.T) {
	dir := t.TempDir()
	rel := "nested.go"
	src := `package main

func outer() {
	fn := func() {
		println("nested")
	}
	fn()
}

func sibling() {}
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := []store.SymbolRecord{
		{Name: "outer", FilePath: rel, Kind: "function", StartLine: 3, EndLine: 3},
		{Name: "sibling", FilePath: rel, Kind: "function", StartLine: 10, EndLine: 10},
	}

	refined, err := treesitter.RefineBoundaries(dir, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(refined) != 2 {
		t.Fatalf("got %d symbols, want 2", len(refined))
	}

	outer := refined[0]
	if outer.Name != "outer" || outer.StartByte < 0 || outer.EndByte <= outer.StartByte {
		t.Fatalf("outer boundaries not set: %+v", outer)
	}
	outerBody := src[outer.StartByte:outer.EndByte]
	if !containsAll(outerBody, "func outer", "nested") {
		t.Fatalf("outer span should include nested literal, got %q", outerBody)
	}
	if containsAll(outerBody, "func sibling") {
		t.Fatal("outer span must not include sibling function")
	}

	sibling := refined[1]
	if sibling.Name != "sibling" || sibling.StartByte < 0 || sibling.EndByte <= sibling.StartByte {
		t.Fatalf("sibling boundaries not set: %+v", sibling)
	}
	siblingBody := src[sibling.StartByte:sibling.EndByte]
	if !containsAll(siblingBody, "func sibling") {
		t.Fatalf("sibling span = %q", siblingBody)
	}
	if containsAll(siblingBody, "func outer") {
		t.Fatal("sibling span must not include outer function")
	}
}

func TestRefineBoundaries_GoGroupedTypes(t *testing.T) {
	dir := t.TempDir()
	rel := "types.go"
	src := `package main

type (
	Alpha int
	Beta  string
)
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := []store.SymbolRecord{
		{Name: "Alpha", FilePath: rel, Kind: "type", StartLine: 4, EndLine: 4},
		{Name: "Beta", FilePath: rel, Kind: "type", StartLine: 5, EndLine: 5},
	}

	refined, err := treesitter.RefineBoundaries(dir, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(refined) != 2 {
		t.Fatalf("got %d symbols, want 2", len(refined))
	}

	alphaBody := src[refined[0].StartByte:refined[0].EndByte]
	if !containsAll(alphaBody, "Alpha int") {
		t.Fatalf("Alpha span = %q", alphaBody)
	}
	if containsAll(alphaBody, "Beta") {
		t.Fatalf("Alpha span must not include Beta, got %q", alphaBody)
	}

	betaBody := src[refined[1].StartByte:refined[1].EndByte]
	if !containsAll(betaBody, "Beta", "string") {
		t.Fatalf("Beta span = %q", betaBody)
	}
	if containsAll(betaBody, "Alpha") {
		t.Fatalf("Beta span must not include Alpha, got %q", betaBody)
	}
}

func TestRefineBoundaries_TSArrowFunction(t *testing.T) {
	dir := t.TempDir()
	rel := "arrow.ts"
	src := `export const processItems = (items: string[]) => {
  return items.map(x => x.trim());
};
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := []store.SymbolRecord{
		{Name: "processItems", FilePath: rel, Kind: "const", StartLine: 1, EndLine: 1},
	}

	refined, err := treesitter.RefineBoundaries(dir, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(refined) != 1 {
		t.Fatalf("got %d symbols, want 1", len(refined))
	}

	sym := refined[0]
	if sym.StartByte < 0 || sym.EndByte <= sym.StartByte {
		t.Fatalf("boundaries not set: %+v", sym)
	}
	body := src[sym.StartByte:sym.EndByte]
	if !containsAll(body, "processItems", "=>", "items.map") {
		t.Fatalf("arrow function span = %q", body)
	}
	if sym.EndLine < sym.StartLine {
		t.Fatalf("end_line %d before start_line %d", sym.EndLine, sym.StartLine)
	}
}

func TestRefineBoundaries_PyDecoratedDef(t *testing.T) {
	dir := t.TempDir()
	rel := "decorated.py"
	src := `@route("/pay")
@requires_auth
def process_payment(amount):
    return amount * 100
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := []store.SymbolRecord{
		{Name: "process_payment", FilePath: rel, Kind: "function", StartLine: 3, EndLine: 3},
	}

	refined, err := treesitter.RefineBoundaries(dir, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(refined) != 1 {
		t.Fatalf("got %d symbols, want 1", len(refined))
	}

	sym := refined[0]
	if sym.StartByte < 0 || sym.EndByte <= sym.StartByte {
		t.Fatalf("boundaries not set: %+v", sym)
	}
	body := src[sym.StartByte:sym.EndByte]
	if !containsAll(body, "@route", "@requires_auth", "def process_payment", "return amount") {
		t.Fatalf("decorated def span = %q", body)
	}
	if sym.StartLine != 1 {
		t.Fatalf("start_line = %d, want 1 (includes decorators)", sym.StartLine)
	}
}

func TestRefineBoundaries_LineTolerance(t *testing.T) {
	dir := t.TempDir()
	rel := "tol.go"
	src := `package main

func Target() {}
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := []store.SymbolRecord{
		{Name: "Target", FilePath: rel, Kind: "function", StartLine: 4, EndLine: 4},
	}

	refined, err := treesitter.RefineBoundaries(dir, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if refined[0].StartByte < 0 {
		t.Fatalf("expected match within ±1 line, got %+v", refined[0])
	}
}

func TestEnabled(t *testing.T) {
	if !treesitter.Enabled() {
		t.Fatal("Expected Enabled() true with treesitter build tag")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

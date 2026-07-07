//go:build !treesitter

package testutil_test

import (
	"testing"

	"github.com/stubbies/litos-mcp/internal/store"
)

// Regex call extraction matches func declarations that look like calls (e.g. "func Foo(").
// Document the limitation so agents know to filter by dir or prefer a treesitter build.
func TestFixtureFindCallers_RegexFalsePositiveOnDeclaration(t *testing.T) {
	_, st, _ := freshFixture(t)
	hits, err := st.FindCallers("ProcessPayment", "", 20)
	if err != nil {
		t.Fatal(err)
	}

	var falsePositive *store.CallerHit
	for i := range hits {
		h := &hits[i]
		if h.FilePath == "src/billing/billing.go" && h.Line == 56 {
			falsePositive = h
			break
		}
	}
	if falsePositive == nil {
		t.Fatalf("expected regex false positive on func ProcessPayment declaration; got %+v", hits)
	}
	if falsePositive.EnclosingSymbol != "ProcessPayment" {
		t.Fatalf("false positive enclosing = %q, want ProcessPayment", falsePositive.EnclosingSymbol)
	}
}

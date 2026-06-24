package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegexExtractor_go(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "billing.go")
	content := `package billing

type BillingService struct{}

func (s *BillingService) ProcessPayment() error {
	return nil
}

type Ledger interface {
	Post()
}
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ext := NewRegexExtractor()
	symbols, err := ext.Extract(context.Background(), root, []string{"billing.go"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	names := map[string]int{}
	for _, sym := range symbols {
		names[sym.Name] = sym.EndLine
		if sym.EndLine < sym.StartLine {
			t.Fatalf("%s: end_line %d < start_line %d", sym.Name, sym.EndLine, sym.StartLine)
		}
	}

	for _, want := range []string{"BillingService", "ProcessPayment", "Ledger"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing symbol %q; got %+v", want, symbols)
		}
	}

	if end := names["BillingService"]; end != 4 {
		t.Fatalf("BillingService end_line=%d want 4", end)
	}
	if end := names["ProcessPayment"]; end != 8 {
		t.Fatalf("ProcessPayment end_line=%d want 8", end)
	}
}

func TestRegexExtractor_typescript(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "auth.ts")
	content := `export interface TokenClaims {
  sub: string;
}

export class AuthService {
  verify(): boolean {
    return true;
  }
}

export function parseToken(input: string): TokenClaims {
  return { sub: "" };
}

export const JWT_ALG = "HS256";
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ext := NewRegexExtractor()
	symbols, err := ext.Extract(context.Background(), root, []string{"auth.ts"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	names := map[string]bool{}
	for _, sym := range symbols {
		names[sym.Name] = true
	}
	for _, want := range []string{"TokenClaims", "AuthService", "parseToken", "JWT_ALG"} {
		if !names[want] {
			t.Fatalf("missing symbol %q", want)
		}
	}
}

func TestRegexExtractor_python(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "service.py")
	content := `class PaymentService:
    def process(self):
        pass

def helper():
    return 1
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ext := NewRegexExtractor()
	symbols, err := ext.Extract(context.Background(), root, []string{"service.py"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	names := map[string]bool{}
	for _, sym := range symbols {
		names[sym.Name] = true
	}
	for _, want := range []string{"PaymentService", "process", "helper"} {
		if !names[want] {
			t.Fatalf("missing symbol %q", want)
		}
	}
}

func TestRegexExtractor_skipsUnknownExtension(t *testing.T) {
	root := t.TempDir()
	ext := NewRegexExtractor()
	symbols, err := ext.Extract(context.Background(), root, []string{"readme.md"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) != 0 {
		t.Fatalf("expected no symbols for .md, got %d", len(symbols))
	}
}

package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractCallSitesRegex_go(t *testing.T) {
	root := t.TempDir()
	rel := "handlers/payment.go"
	content := `package handlers

import "example/billing"

type PaymentHandler struct{}

func (h *PaymentHandler) HandleCharge() {
	result, err := billing.ProcessPayment(h.service, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
`
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := extractCallSitesRegex(context.Background(), root, []string{rel})
	if err != nil {
		t.Fatalf("extractCallSitesRegex: %v", err)
	}

	var processPayment *struct {
		line int
		col  int
	}
	for _, call := range calls {
		if call.CalleeName == "ProcessPayment" {
			processPayment = &struct {
				line int
				col  int
			}{call.Line, call.Col}
		}
	}
	if processPayment == nil {
		t.Fatalf("missing ProcessPayment call; got %+v", calls)
	}
	if processPayment.line != 8 {
		t.Fatalf("ProcessPayment line = %d, want 8", processPayment.line)
	}
	if processPayment.col <= 0 {
		t.Fatalf("ProcessPayment col = %d, want positive", processPayment.col)
	}
}

func TestExtractCallSitesRegex_skipsUnknownExtension(t *testing.T) {
	root := t.TempDir()
	calls, err := extractCallSitesRegex(context.Background(), root, []string{"readme.md"})
	if err != nil {
		t.Fatalf("extractCallSitesRegex: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no calls for .md, got %d", len(calls))
	}
}

func TestExtractCallSitesRegex_multipleCallsPerLine(t *testing.T) {
	root := t.TempDir()
	rel := "chain.go"
	content := "package main\n\nfunc main() { foo(bar()) }\n"
	abs := filepath.Join(root, rel)
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := extractCallSitesRegex(context.Background(), root, []string{rel})
	if err != nil {
		t.Fatalf("extractCallSitesRegex: %v", err)
	}

	names := map[string]bool{}
	for _, call := range calls {
		names[call.CalleeName] = true
	}
	for _, want := range []string{"main", "foo", "bar"} {
		if !names[want] {
			t.Fatalf("missing callee %q; got %+v", want, calls)
		}
	}
}

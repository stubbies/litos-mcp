//go:build treesitter

package treesitter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbies/litos-mcp/internal/index/treesitter"
)

func TestExtractCallSites_GoSelectorExpression(t *testing.T) {
	dir := t.TempDir()
	rel := "handlers/payment.go"
	src := `package handlers

import "example/billing"

type PaymentHandler struct{}

func (h *PaymentHandler) HandleCharge() {
	result, err := billing.ProcessPayment(h.service, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
`
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := treesitter.ExtractCallSites(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
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

func TestExtractCallSites_GoNestedCalls(t *testing.T) {
	dir := t.TempDir()
	rel := "chain.go"
	src := "package main\n\nfunc main() { foo(bar()) }\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := treesitter.ExtractCallSites(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, call := range calls {
		names[call.CalleeName] = true
	}
	for _, want := range []string{"foo", "bar"} {
		if !names[want] {
			t.Fatalf("missing callee %q; got %+v", want, calls)
		}
	}
}

func TestExtractCallSites_TypeScriptMemberExpression(t *testing.T) {
	dir := t.TempDir()
	rel := "client.ts"
	src := `import { billing } from "./billing";

export function handleCharge() {
  return billing.processPayment(service, req);
}
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := treesitter.ExtractCallSites(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, call := range calls {
		if call.CalleeName == "processPayment" {
			found = true
			if call.Line != 4 {
				t.Fatalf("processPayment line = %d, want 4", call.Line)
			}
		}
	}
	if !found {
		t.Fatalf("missing processPayment call; got %+v", calls)
	}
}

func TestExtractCallSites_PythonAttributeCall(t *testing.T) {
	dir := t.TempDir()
	rel := "handler.py"
	src := `from billing import service

def handle_charge():
    return service.process_payment(amount)
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	calls, err := treesitter.ExtractCallSites(dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, call := range calls {
		if call.CalleeName == "process_payment" {
			found = true
			if call.Line != 4 {
				t.Fatalf("process_payment line = %d, want 4", call.Line)
			}
		}
	}
	if !found {
		t.Fatalf("missing process_payment call; got %+v", calls)
	}
}

func TestExtractCallSites_SkipsUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	calls, err := treesitter.ExtractCallSites(dir, []string{"readme.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("expected no calls for .md, got %d", len(calls))
	}
}

func TestCallSitesEnabled(t *testing.T) {
	if !treesitter.CallSitesEnabled() {
		t.Fatal("expected CallSitesEnabled() true with treesitter build tag")
	}
}

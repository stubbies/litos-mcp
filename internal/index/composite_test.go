//go:build !treesitter

package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompositeExtractor_DelegatesName(t *testing.T) {
	ext := NewExtractor()
	if ext.Name() != "regex" && ext.Name() != "ctags" {
		t.Fatalf("unexpected extractor name: %q", ext.Name())
	}
	if _, ok := ext.(*compositeExtractor); !ok {
		t.Fatal("NewExtractor should return *compositeExtractor")
	}
}

func TestCompositeExtractor_ExtractWithoutRefinement(t *testing.T) {
	dir := t.TempDir()
	rel := "plain.go"
	src := "package main\n\nfunc Plain() {}\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ext := NewExtractor()
	symbols, err := ext.Extract(context.Background(), dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}
	for _, sym := range symbols {
		if sym.StartByte >= 0 && sym.EndByte > sym.StartByte {
			t.Fatalf("without treesitter tag bytes should stay unset, got %+v", sym)
		}
	}
}

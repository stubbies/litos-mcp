//go:build treesitter

package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompositeExtractor_RefinesBoundaries(t *testing.T) {
	dir := t.TempDir()
	rel := "sample.go"
	src := `package main

func Sample() {
	println("hi")
}
`
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ext := NewExtractor()
	symbols, err := ext.Extract(context.Background(), dir, []string{rel})
	if err != nil {
		t.Fatal(err)
	}

	var sample *struct {
		startByte, endByte int
	}
	for i := range symbols {
		if symbols[i].Name == "Sample" {
			sample = &struct{ startByte, endByte int }{
				startByte: symbols[i].StartByte,
				endByte:   symbols[i].EndByte,
			}
			break
		}
	}
	if sample == nil {
		t.Fatal("Sample symbol not found")
	}
	if sample.startByte < 0 || sample.endByte <= sample.startByte {
		t.Fatalf("expected byte boundaries on Sample, got start=%d end=%d", sample.startByte, sample.endByte)
	}
	body := src[sample.startByte:sample.endByte]
	if !containsSubstring(body, "func Sample") || containsSubstring(body, "package main") {
		t.Fatalf("refined span = %q", body)
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfSubstring(s, sub) >= 0)
}

func indexOfSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

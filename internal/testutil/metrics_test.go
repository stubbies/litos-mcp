package testutil_test

import (
	"testing"

	"github.com/stubbies/litos-mcp/internal/testutil"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 2},
		{"a", 1},
	}
	for _, tc := range tests {
		got := testutil.EstimateTokens(tc.in)
		if got != tc.want {
			t.Fatalf("EstimateTokens(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

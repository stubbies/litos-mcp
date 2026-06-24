package testutil

import (
	"fmt"
	"math"
	"testing"
)

// EstimateTokens returns a conservative token proxy: ceil(len(s) / 4).
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return int(math.Ceil(float64(len(s)) / 4.0))
}

// AssertEqualInt fails when got != want and prints a metric diff.
func AssertEqualInt(t *testing.T, name string, want, got int) {
	t.Helper()
	if got != want {
		t.Fatalf("metric %s: got %d, want %d\n%s", name, got, want, metricDiff(name, want, got))
	}
}

// AssertMinInt fails when got < min.
func AssertMinInt(t *testing.T, name string, min, got int) {
	t.Helper()
	if got < min {
		t.Fatalf("metric %s: got %d, want >= %d\n%s", name, got, min, metricDiff(name, min, got))
	}
}

// AssertMaxInt fails when got > max.
func AssertMaxInt(t *testing.T, name string, max, got int) {
	t.Helper()
	if got > max {
		t.Fatalf("metric %s: got %d, want <= %d\n%s", name, got, max, metricDiff(name, max, got))
	}
}

// AssertMaxFloat fails when got > max.
func AssertMaxFloat(t *testing.T, name string, max, got float64) {
	t.Helper()
	if got > max {
		t.Fatalf("metric %s: got %.4f, want <= %.4f\n%s", name, got, max, metricDiff(name, max, got))
	}
}

// AssertMaxDuration fails when elapsed milliseconds exceed maxMs.
func AssertMaxDuration(t *testing.T, name string, maxMs int, elapsedMs int64) {
	t.Helper()
	if elapsedMs > int64(maxMs) {
		t.Fatalf("metric %s: got %dms, want <= %dms\n%s", name, elapsedMs, maxMs, metricDiff(name, maxMs, elapsedMs))
	}
}

func metricDiff(name string, expected, actual any) string {
	return fmt.Sprintf("--- expected %s\n+++ actual %s\n- %v\n+ %v", name, name, expected, actual)
}

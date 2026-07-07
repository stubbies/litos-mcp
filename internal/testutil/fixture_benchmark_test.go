package testutil_test

import (
	"testing"

	"github.com/stubbies/litos-mcp/internal/testutil"
)

func BenchmarkFixtureSearch(b *testing.B) {
	root := testutil.CopyFixtureRepo(b)
	st, _ := testutil.InitFixture(b, root)

	queries := []string{
		"ProcessPayment", "jwt verification", "BillingService", "User",
		"ApiClient", "config", "handlers", "Verify", "Slugify", "Migrate",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		if _, err := st.Search(q, 10); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchmarkSearchRegression(t *testing.T) {
	m := testutil.LoadGoldenMetrics(t)
	root := testutil.CopyFixtureRepo(t)
	st, _ := testutil.InitFixture(t, root)

	for i := 0; i < 50; i++ {
		if _, err := st.Search("ProcessPayment", 10); err != nil {
			t.Fatal(err)
		}
	}

	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := st.Search("ProcessPayment", 10); err != nil {
				b.Fatal(err)
			}
		}
	})
	if nsOp := result.NsPerOp(); nsOp > m.Benchmarks.SearchNsOpMax {
		t.Fatalf("search regression: %dns/op exceeds ceiling %d (2× baseline guard)", nsOp, m.Benchmarks.SearchNsOpMax)
	}
}

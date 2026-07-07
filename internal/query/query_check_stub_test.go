//go:build !treesitter

package query_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stubbies/litos-mcp/internal/query"
)

func TestCheckFile_UnavailableWithoutTreesitter(t *testing.T) {
	_, err := query.CheckFile(context.Background(), t.TempDir(), "main.go")
	if !errors.Is(err, query.ErrSyntaxCheckUnavailable) {
		t.Fatalf("err = %v, want ErrSyntaxCheckUnavailable", err)
	}
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestOutdirReviewStructuredFallback(t *testing.T) {
	for _, e := range []error{errors.New("没有可定名的非测试 Go 源文件"), errors.New("no such file in unrelated loader"), os.ErrPermission} {
		if isUndeterminableBecauseUninitialized(e) {
			t.Fatalf("untyped error allowed fallback: %v", e)
		}
	}
	for _, e := range []error{fmt.Errorf("new wording: %w", errPackageUninitialized), fmt.Errorf("missing path: %w", os.ErrNotExist)} {
		if !isUndeterminableBecauseUninitialized(e) {
			t.Fatalf("typed cause lost: %v", e)
		}
	}
}

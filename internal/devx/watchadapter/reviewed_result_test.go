package watchadapter

import (
	"context"
	"errors"
	"github.com/goxml/goxml/internal/devx/watch"
	"strings"
	"testing"
)

func TestReviewedCancellationPreservesCleanupCause(t *testing.T) {
	cause := errors.New("owned tree cleanup failure")
	rc, out := generationResult(watch.ExecResult{RC: -1, Cancelled: true, Err: errors.Join(context.Canceled, cause)})
	if rc != -1 || !strings.Contains(out, cause.Error()) || !strings.Contains(out, context.Canceled.Error()) {
		t.Fatalf("lost cancellation/cleanup evidence: rc=%d out=%s", rc, out)
	}
}

package cli

import "testing"

func TestBatchReviewPathCaseIdentity(t *testing.T) {
	upper, lower := "module/Settings", "module/settings"
	if normalizeDirCase(upper, false) == normalizeDirCase(lower, false) {
		t.Fatal("case-sensitive targets were collapsed")
	}
	if normalizeDirCase(upper, true) != normalizeDirCase(lower, true) {
		t.Fatal("Windows case aliases were not unified")
	}
}

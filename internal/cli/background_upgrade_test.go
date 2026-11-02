package cli

import "testing"

// A v4 record could legitimately describe the old generator that ignored
// Background. It cannot authorize reuse after the region-B implementation.
func TestBackgroundRejectsPreRenderingV4Record(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	rec := mustSaveRecord(t, root, key)
	rec.Version = 4
	if err := saveCacheRecord(root, rec); err != nil {
		t.Fatal(err)
	}
	got, status, err := loadCacheRecord(root, key)
	if err != nil || got != nil || status != cacheCorrupt {
		t.Fatalf("old background-less cache accepted: got=%v status=%v err=%v", got, status, err)
	}
}

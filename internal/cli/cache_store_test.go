package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// TASK-GENERATE-CACHE-001 切片 B2：缓存记录读写、损坏回退、原子写
//
// 变红方式（已实测）：
//   - 删掉 cacheRecordUnusable 里的版本判断 → TestCacheStoreVersionMismatchIsCorrupt 红；
//   - 删掉键自校验 → TestCacheStoreKeyMismatchIsCorrupt 红；
//   - 让 loadCacheRecord 在损坏时返回 error → TestCacheStoreCorruptJSONFallsBack 红；
//   - 让 saveCacheRecord 直接 os.WriteFile 到目标 → TestCacheStoreNoTempLeftover 仍绿
//     （该用例只证明「不留残骸」，**不证明崩溃原子性**，见文件末说明）。
// ============================================================================

// cacheTestKey 是一个形态合法的键（64 位小写 hex）。
func cacheTestKey() string { return strings.Repeat("a", 64) }

// cacheTestRoot 返回隔离的 moduleRoot（缓存落在其 .goxml/cache 下）。
func cacheTestRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// mustSaveRecord 保存一条可用记录。
func mustSaveRecord(t *testing.T, root, keyHash string) cacheRecord {
	t.Helper()
	rec := newCacheRecord(keyHash, "ui/home.gxml", "ui/home_gen.go",
		strings.Repeat("1", 64), strings.Repeat("2", 64))
	if err := saveCacheRecord(root, rec); err != nil {
		t.Fatalf("保存缓存失败: %v", err)
	}
	return rec
}

// TestCacheStoreRoundTrip：写入后能原样读回，状态为 ok。
func TestCacheStoreRoundTrip(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	want := mustSaveRecord(t, root, key)

	got, status, err := loadCacheRecord(root, key)
	if err != nil {
		t.Fatalf("读取缓存报错: %v", err)
	}
	if status != cacheOK {
		t.Fatalf("状态应为 ok，得到 %v", status)
	}
	if got == nil {
		t.Fatal("ok 状态却返回 nil 记录")
	}
	if *got != want {
		t.Fatalf("往返后记录不一致:\n got=%+v\n want=%+v", *got, want)
	}
}

// TestCacheStoreMissingIsNotError：缓存不存在是合法状态，不是错误。
//
// 首次生成就是这个状态；把它当错误会让「第一次跑」失败。
func TestCacheStoreMissingIsNotError(t *testing.T) {
	root := cacheTestRoot(t)
	rec, status, err := loadCacheRecord(root, cacheTestKey())
	if err != nil {
		t.Fatalf("不存在时不应报错: %v", err)
	}
	if status != cacheMissing || rec != nil {
		t.Fatalf("期望 (nil, missing)，得到 (%v, %v)", rec, status)
	}
}

// TestCacheStoreCorruptJSONFallsBack：坏 JSON = 未命中，不报错，且坏文件被清掉。
//
// 这是「缓存损坏 → 安全回退」的核心用例；与 Manifest「损坏即自锁」故意相反。
func TestCacheStoreCorruptJSONFallsBack(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	path := cacheRecordPath(root, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, status, err := loadCacheRecord(root, key)
	if err != nil {
		t.Fatalf("缓存损坏不得让构建失败，却报错: %v", err)
	}
	if status != cacheCorrupt || rec != nil {
		t.Fatalf("期望 (nil, corrupt)，得到 (%v, %v)", rec, status)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("损坏的缓存文件应被删除，实际仍存在: %v", statErr)
	}
}

// TestCacheStoreVersionMismatchIsCorrupt：格式版本不符必须视为未命中。
//
// 若放行，旧格式记录的字段含义可能与新代码不同 → 假命中。
func TestCacheStoreVersionMismatchIsCorrupt(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	rec := mustSaveRecord(t, root, key)
	rec.Version = cacheFormatVersion + 1
	if err := saveCacheRecord(root, rec); err != nil {
		t.Fatal(err)
	}

	got, status, err := loadCacheRecord(root, key)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if status != cacheCorrupt || got != nil {
		t.Fatalf("版本不符应视为 corrupt，得到 (%v, %v)", got, status)
	}
}

// TestCacheStoreKeyMismatchIsCorrupt：文件名与内容键不符 → 记录不属于本次输入。
//
// 构造方式：先以键 b 正常保存（文件名 b.json、内容 KeyHash=b），
// 再把文件**搬成** a.json。直接改 rec.KeyHash 再 save 造不出来——
// save 用同一字段命名文件，写出的记录永远自洽。
func TestCacheStoreKeyMismatchIsCorrupt(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	other := strings.Repeat("b", 64)

	mustSaveRecord(t, root, other)
	if err := os.Rename(cacheRecordPath(root, other), cacheRecordPath(root, key)); err != nil {
		t.Fatal(err)
	}

	got, status, _ := loadCacheRecord(root, key)
	if status != cacheCorrupt || got != nil {
		t.Fatalf("键不符应视为 corrupt，得到 (%v, %v)", got, status)
	}
}

// TestCacheStoreMissingOutputHashIsCorrupt：没有 OutputHash 就无法识别产物被改。
func TestCacheStoreMissingOutputHashIsCorrupt(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	rec := mustSaveRecord(t, root, key)
	rec.OutputHash = ""
	if err := saveCacheRecord(root, rec); err != nil {
		t.Fatal(err)
	}

	got, status, _ := loadCacheRecord(root, key)
	if status != cacheCorrupt || got != nil {
		t.Fatalf("缺 OutputHash 应视为 corrupt，得到 (%v, %v)", got, status)
	}
}

// TestCacheStoreNoTempLeftover：保存后缓存目录只剩记录文件，没有 *.tmp 残骸。
func TestCacheStoreNoTempLeftover(t *testing.T) {
	root := cacheTestRoot(t)
	mustSaveRecord(t, root, cacheTestKey())

	entries, err := os.ReadDir(cacheDir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("保存后残留临时文件: %s", e.Name())
		}
		if !cacheRecordNamePattern.MatchString(e.Name()) {
			t.Fatalf("缓存目录出现非记录文件: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("期望 1 个缓存文件，实际 %d", len(entries))
	}
}

// TestCacheStoreOverwriteKeepsLatest：同键再写必须覆盖为最新内容。
func TestCacheStoreOverwriteKeepsLatest(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	mustSaveRecord(t, root, key)

	rec := newCacheRecord(key, "ui/home.gxml", "ui/home_gen.go",
		strings.Repeat("3", 64), strings.Repeat("4", 64))
	if err := saveCacheRecord(root, rec); err != nil {
		t.Fatal(err)
	}

	got, status, _ := loadCacheRecord(root, key)
	if status != cacheOK {
		t.Fatalf("状态应为 ok，得到 %v", status)
	}
	if got.OutputHash != strings.Repeat("4", 64) {
		t.Fatalf("未读到最新记录，OutputHash=%q", got.OutputHash)
	}
}

// TestCacheStoreRejectsBadKeyHash：非法键不得落到缓存目录外。
func TestCacheStoreRejectsBadKeyHash(t *testing.T) {
	root := cacheTestRoot(t)
	bad := "../escape"
	if err := saveCacheRecord(root, newCacheRecord(bad, "a", "b", "c", "d")); err == nil {
		t.Fatal("保存非法键应报错")
	}
	if _, _, err := loadCacheRecord(root, bad); err == nil {
		t.Fatal("读取非法键应报错")
	}
}

// TestCacheStoreIgnoresJunkFiles：缓存目录里的无关文件不影响正常记录读取。
func TestCacheStoreIgnoresJunkFiles(t *testing.T) {
	root := cacheTestRoot(t)
	key := cacheTestKey()
	mustSaveRecord(t, root, key)

	junk := filepath.Join(cacheDir(root), "README.txt")
	if err := os.WriteFile(junk, []byte("not a cache"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, status, err := loadCacheRecord(root, key)
	if err != nil || status != cacheOK {
		t.Fatalf("无关文件不应影响读取，得到 status=%v err=%v", status, err)
	}
	if _, statErr := os.Stat(junk); statErr != nil {
		t.Fatalf("无关文件不应被删除: %v", statErr)
	}
}

// 说明（如实记录证据边界）：
//
// 「进程在写缓存中途被杀」的崩溃原子性**无法在进程内测试**。
// 本文件用等价手段覆盖其后果链：
//   - 半截 JSON → TestCacheStoreCorruptJSONFallsBack（视为未命中，重新生成）；
//   - 临时残骸 → TestCacheStoreNoTempLeftover（只认 <hex>.json）。
//
// 因此可以说「半写不会造成假命中」，但**不能**说「已测试崩溃场景」。

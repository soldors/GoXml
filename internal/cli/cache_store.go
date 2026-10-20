// 本文件实现增量缓存的记录读写（TASK-GENERATE-CACHE-001 切片 B2）。
//
// 三条冻结规则（任务卡 §3/§4）：
//  1. 缓存损坏 → **安全回退**（视为未命中、best-effort 删除），绝不报错；
//     这与 Manifest 的「损坏即自锁」是**故意的不对称**：
//     Manifest 决定所有权与清理范围，缓存只提速，不能成为新的失败源。
//  2. 写入必须原子（临时文件 + os.Rename），避免半截 JSON 被当成有效缓存。
//  3. 文件名即键（`<keyHash>.json`），加载只认 64 位 hex + `.json`，
//     忽略中断残留的 `*.tmp`。
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// cacheDirName / cacheDir 是缓存目录（契约 §11：`.goxml/cache/`）。
const cacheDirName = "cache"

// cacheRecordNamePattern 限定缓存文件名形态：64 位小写 hex + .json。
//
// 只认这个形态，是为了让 `*.tmp`（写入中断残骸）与任何人工放进去的文件
// 都不会被当成缓存记录。
var cacheRecordNamePattern = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// cacheRecord 是一条缓存记录（缓存文件的内容）。
//
// 与 Manifest entry 的分工：Manifest 记录「这个产物归我管、源是什么」，
// 缓存记录「这份键对应的产物内容长什么样、由哪个版本生成」。
// OutputHash 是识别「产物被手改」的依据（命中条件 H5），Manifest 里没有它。
type cacheRecord struct {
	// Version 是缓存格式版本，必须等于 cacheFormatVersion。
	Version int `json:"version"`
	// KeyHash 是缓存键（等于文件名，冗余存一份用于自校验）。
	KeyHash string `json:"keyHash"`
	// Source / Output 是项目相对路径（`/` 分隔），供诊断。
	Source string `json:"source"`
	Output string `json:"output"`
	// SourceHash 是 .gxml 内容哈希（K1），供诊断与「键一致但源不同」的排查。
	SourceHash string `json:"sourceHash"`
	// OutputHash 是产物内容的 SHA-256，命中校验用（H5）。
	OutputHash string `json:"outputHash"`
	// CompilerVersion 是生成器版本（K4）。
	CompilerVersion string `json:"compilerVersion"`
	// CreatedAt 是 RFC3339 时间戳。**不参与缓存键**：时间进入键会让命中率恒为 0。
	CreatedAt string `json:"createdAt"`
}

// cacheLoadStatus 是加载缓存记录的三种结果。
type cacheLoadStatus int

const (
	// cacheMissing 表示该键没有缓存文件（首次生成、或缓存被删）。
	cacheMissing cacheLoadStatus = iota
	// cacheOK 表示记录存在、可解析、版本与键自校验通过。
	cacheOK
	// cacheCorrupt 表示记录存在但不可用（JSON 坏、版本不符、键不符、字段缺失）。
	cacheCorrupt
)

// String 便于测试与诊断输出。
func (s cacheLoadStatus) String() string {
	switch s {
	case cacheMissing:
		return "missing"
	case cacheOK:
		return "ok"
	case cacheCorrupt:
		return "corrupt"
	default:
		return "unknown"
	}
}

// cacheDir 返回缓存目录的绝对路径。
func cacheDir(moduleRoot string) string {
	return filepath.Join(moduleRoot, manifestDir, cacheDirName)
}

// cacheRecordPath 返回某个键对应的缓存文件路径。
//
// 键必须先通过 validateKeyHash：否则调用方传入 `../x` 之类会写到缓存目录外。
func cacheRecordPath(moduleRoot, keyHash string) string {
	return filepath.Join(cacheDir(moduleRoot), keyHash+".json")
}

// validateKeyHash 校验键形态（64 位小写 hex）。
func validateKeyHash(keyHash string) error {
	if keyHash == "" {
		return fmt.Errorf("缓存键为空")
	}
	if !cacheRecordNamePattern.MatchString(keyHash + ".json") {
		return fmt.Errorf("缓存键形态非法（需 64 位小写 hex）: %q", keyHash)
	}
	return nil
}

// loadCacheRecord 读取缓存记录。
//
// 返回 (记录, 状态)。**只有「读不动文件系统」这类真错误才返回 error**：
// 文件不存在、JSON 损坏、版本不符、键不符、字段缺失都归为状态，
// 因为缓存不可用时唯一正确的动作是「当作未命中，重新生成」，而不是让构建失败。
//
// 损坏的记录会被 best-effort 删除：留着只会让每次运行都重复解析失败。
func loadCacheRecord(moduleRoot, keyHash string) (*cacheRecord, cacheLoadStatus, error) {
	if err := validateKeyHash(keyHash); err != nil {
		return nil, cacheMissing, err
	}
	path := cacheRecordPath(moduleRoot, keyHash)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, cacheMissing, nil
		}
		return nil, cacheMissing, fmt.Errorf("读取缓存 %s: %w", path, err)
	}

	var rec cacheRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		removeCacheRecord(moduleRoot, keyHash)
		return nil, cacheCorrupt, nil
	}
	if reason := cacheRecordUnusable(rec, keyHash); reason != "" {
		removeCacheRecord(moduleRoot, keyHash)
		return nil, cacheCorrupt, nil
	}
	return &rec, cacheOK, nil
}

// cacheRecordUnusable 返回记录不可用的原因（空串表示可用）。
//
// 每一项都对应一次真实的「假命中」风险：
//   - 版本不符：旧格式字段含义可能已变，按新语义读会误判；
//   - 键不符：文件名与内容不一致（复制/改名/并发写）→ 记录不属于本次输入；
//   - 关键字段缺失：没有 OutputHash 就无法识别产物被改（H5 失效）。
func cacheRecordUnusable(rec cacheRecord, keyHash string) string {
	if rec.Version != cacheFormatVersion {
		return "版本不符"
	}
	if rec.KeyHash != keyHash {
		return "键不符"
	}
	if rec.Output == "" || rec.OutputHash == "" || rec.SourceHash == "" {
		return "关键字段缺失"
	}
	if rec.CompilerVersion == "" {
		return "生成器版本缺失"
	}
	return ""
}

// saveCacheRecord 原子写入缓存记录（临时文件 + os.Rename）。
//
// 顺序纪律（任务卡 §4）：缓存必须在**产物提交且 Manifest 保存之后**才写。
// 本函数只管原子落盘，不负责顺序——顺序由调用方（B3）保证。
func saveCacheRecord(moduleRoot string, rec cacheRecord) error {
	if err := validateKeyHash(rec.KeyHash); err != nil {
		return fmt.Errorf("保存缓存: %w", err)
	}
	dir := cacheDir(moduleRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建缓存目录: %w", err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化缓存记录: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".goxml-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("创建缓存临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后路径已不存在，忽略错误
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写缓存临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭缓存临时文件: %w", err)
	}
	target := cacheRecordPath(moduleRoot, rec.KeyHash)
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("替换缓存记录: %w", err)
	}
	return nil
}

// removeCacheRecord 尽力删除一条缓存记录，失败不报错。
//
// 用途：损坏记录清理、以及失效后避免留下「看起来还有效」的旧记录。
// 缓存本身就是可删的，删除失败最坏结果是下次再判一次未命中。
func removeCacheRecord(moduleRoot, keyHash string) {
	if validateKeyHash(keyHash) != nil {
		return
	}
	_ = os.Remove(cacheRecordPath(moduleRoot, keyHash))
}

// newCacheRecord 组装一条记录（CreatedAt 由调用方传入，便于测试固定时间）。
func newCacheRecord(keyHash, source, output, sourceHash, outputHash string) cacheRecord {
	return cacheRecord{
		Version:         cacheFormatVersion,
		KeyHash:         keyHash,
		Source:          source,
		Output:          output,
		SourceHash:      sourceHash,
		OutputHash:      outputHash,
		CompilerVersion: codegen.GoxmlVersion,
	}
}

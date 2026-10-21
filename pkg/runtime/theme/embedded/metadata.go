// Package embedded 持有 Goxml 的嵌入兜底字体及其来源元数据
// （TASK-FONT-001-B：系统字体探测失败时使用）。
//
// # 内容
//
//   - GoxmlSansSC-Regular.otf：由官方 Noto Sans SC 2.004 构建期子集化而来，
//     GB2312 全覆盖（6,763 汉字）+ ASCII 可打印字符；**不是完整 CJK**，
//     清单外码位无字形（见 METADATA.json 的 coverage 段）。
//   - charset.txt：子集用的完整码位清单，一行一个，可逐项核对。
//   - LICENSE：SIL Open Font License 1.1 全文（OFL 要求随附）。
//   - METADATA.json：上游来源、两级 SHA-256、工具与参数、覆盖边界。
//
// # 产物不由本包生成
//
// 子集化只在**构建期**由 `scripts/font/subset.py` 执行；运行时绝不子集化
// （B 卡 §2.1）。产物逐字节可复现（已固定 `--no-recalc-timestamp` 与
// `recalcTimestamp=False`），重跑脚本应得到同一 SHA-256。
//
// # 构建标签
//
// `nofontembed` 会**排除**嵌入数据（见 assets_stub.go），用于 B5 的
// 「嵌入前后二进制增量」实测；此时 Load 返回 ErrNotEmbedded。
package embedded

import (
	"encoding/json"
	"fmt"
)

// PostScriptName 是嵌入字体的稳定标识（改名后的 PostScript 名）。
//
// 取自 METADATA.json，并由 subset.py 写入字体 name 表 nameID 6；
// 测试会交叉断言三者一致，因此这里是常量而不是运行时读出来的值。
const PostScriptName = "GoxmlSansSC-Regular"

// FamilyName 是改名后的字体族名。
const FamilyName = "Goxml Sans SC"

// Upstream 描述字体来源（用于可追溯性审计）。
type Upstream struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
	License    string `json:"license"`
}

// Digest 描述一个文件的大小与哈希。
type Digest struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// SourceDigest 是上游原始文件（子集化输入）的摘要。
type SourceDigest struct {
	File        string `json:"file"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	GitBlobSHA1 string `json:"gitBlobSha1"`
}

// CharsetInfo 描述子集字符集清单。
type CharsetInfo struct {
	File       string `json:"file"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	Codepoints int    `json:"codepoints"`
	Hanzi      int    `json:"hanzi"`
}

// LicenseInfo 描述随附的许可证全文。
type LicenseInfo struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Name   string `json:"name"`
}

// Tool 记录生成工具与参数（可复现性）。
type Tool struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Args    []string `json:"args"`
}

// Coverage 如实标注覆盖边界。
//
// CompleteCJK 恒为 false：本字体**不是**完整 CJK 覆盖，任何地方都不得
// 把它描述成"完整 CJK"（B 卡 §2.2）。
type Coverage struct {
	Charset           string   `json:"charset"`
	CompleteCJK       bool     `json:"completeCJK"`
	Note              string   `json:"note"`
	OutsideCodepoints []string `json:"outsideCodepoints"`
}

// Metadata 是 METADATA.json 的结构化形式。
type Metadata struct {
	FamilyName     string       `json:"familyName"`
	PostScriptName string       `json:"postScriptName"`
	Version        string       `json:"version"`
	Upstream       Upstream     `json:"upstream"`
	Source         SourceDigest `json:"source"`
	Subset         Digest       `json:"subset"`
	Charset        CharsetInfo  `json:"charset"`
	License        LicenseInfo  `json:"license"`
	Tool           Tool         `json:"tool"`
	Coverage       Coverage     `json:"coverage"`
}

// ParseMetadata 解析 METADATA.json 并做基本一致性校验。
//
// 校验的是"元数据自相矛盾"这类问题（不是与文件系统比对——那是测试的事）：
// 字体名必须与常量一致，否则说明脚本与包定义脱节了。
func ParseMetadata(data []byte) (Metadata, error) {
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return Metadata{}, fmt.Errorf("embedded: METADATA.json 解析失败: %w", err)
	}
	if m.FamilyName != FamilyName {
		return Metadata{}, fmt.Errorf("embedded: familyName 为 %q，期望 %q", m.FamilyName, FamilyName)
	}
	if m.PostScriptName != PostScriptName {
		return Metadata{}, fmt.Errorf("embedded: postScriptName 为 %q，期望 %q",
			m.PostScriptName, PostScriptName)
	}
	if m.Coverage.CompleteCJK {
		return Metadata{}, fmt.Errorf("embedded: coverage.completeCJK 必须为 false" +
			"（本字体不是完整 CJK 覆盖）")
	}
	return m, nil
}

//go:build nofontembed

package embedded

import "errors"

// ErrNotEmbedded 表示本次构建按 `nofontembed` 标签**排除**了嵌入字体。
//
// 存在的唯一理由：TASK-FONT-001-B 的 B5 需要实测「嵌入前 / 嵌入后」
// 二进制增量，因此必须能构建出一个不含字体数据的产物。生产构建不得使用该标签，
// 否则系统字体缺失时就没有兜底了（这也是 B6 要求"两条链路都失败必须报错"的由来）。
var ErrNotEmbedded = errors.New("embedded: 本构建排除了嵌入字体（构建标签 nofontembed）")

// Assets 与 assets.go 保持同一定义，保证两种构建下 API 一致。
type Assets struct {
	Font        []byte
	Metadata    Metadata
	MetadataRaw []byte
	Charset     []byte
	License     []byte
}

// Available 报告本次构建是否带入了嵌入字体。
func Available() bool { return false }

// Load 在 nofontembed 构建下恒失败，且**不返回空资产 + nil**——
// 那会让调用方以为"加载成功但字体是空的"，属于伪装成功。
func Load() (Assets, error) { return Assets{}, ErrNotEmbedded }

// Font 在 nofontembed 构建下返回 nil。
func Font() []byte { return nil }

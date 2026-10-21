//go:build !nofontembed

package embedded

import (
	_ "embed"
	"errors"
)

// errNoFontData 表示嵌入变量存在但为空——正常构建不应该发生，
// 一旦发生必须报错而不是当成"可用但空"。
var errNoFontData = errors.New("embedded: 嵌入字体数据为空")

// 嵌入资产。构建标签 `nofontembed` 时本文件不参与编译，
// 由 assets_stub.go 提供同签名的空实现（B5 的体积实测依赖这一点）。
var (
	//go:embed GoxmlSansSC-Regular.otf
	fontData []byte

	//go:embed METADATA.json
	metadataData []byte

	//go:embed charset.txt
	charsetData []byte

	//go:embed LICENSE
	licenseData []byte
)

// Assets 是一次 Load 的结果（只读，调用方不得修改底层切片）。
type Assets struct {
	Font        []byte
	Metadata    Metadata
	MetadataRaw []byte
	Charset     []byte
	License     []byte
}

// Available 报告本次构建是否带入了嵌入字体。
func Available() bool { return true }

// Load 返回嵌入资产。
//
// 不做字体解析（那是 theme 包的事）：本包只负责"字节是否存在、元数据是否自洽"。
func Load() (Assets, error) {
	meta, err := ParseMetadata(metadataData)
	if err != nil {
		return Assets{}, err
	}
	if len(fontData) == 0 {
		return Assets{}, errNoFontData
	}
	return Assets{
		Font:        fontData,
		Metadata:    meta,
		MetadataRaw: metadataData,
		Charset:     charsetData,
		License:     licenseData,
	}, nil
}

// Font 直接返回嵌入字体字节（Load 的便捷子集，供只需要字节的调用方使用）。
func Font() []byte { return fontData }

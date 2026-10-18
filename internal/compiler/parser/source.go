// Package parser 实现 GXML 的语法分析。
//
// 设计取舍（见 docs/04-WBS/任务卡/TASK-PARSER-001.md）：
//
//   - 词法与 XML 语法规则复用标准库 encoding/xml，不重复实现转义、注释、
//     CDATA、单双引号等规则。
//   - 标准库的 xml.Attr 不携带源码位置，因此本包用 InputOffset() 维护
//     「字节偏移 → 行列」映射，并对起始标签的原始字节区间重新扫描，
//     从而为每个属性给出精确的 SourceSpan。
//   - Parser 只做语法层校验（结构、标签、属性名、值语法分类）。
//     绑定路径是否存在、事件方法签名是否正确，属于 Analyzer 的职责。
package parser

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// 标签名与属性名。GXML 名称区分大小写。
const (
	tagGoxml  = "Goxml"
	tagDefs   = "Defs"
	tagDef    = "Def"
	tagSlot   = "Slot"
	tagPage   = "Page"
	tagIf     = "If"
	tagThen   = "Then"
	tagElse   = "Else"
	tagFor    = "For"
	tagColumn = "Column"
	tagRow    = "Row"
	tagGrid   = "Grid"
	tagSpacer = "Spacer"
	tagExpand = "Expanded"
)

// builtinControls 是内置控件标签名。它们同时是保留名，
// 用户不能用 <Def> 定义同名组件，否则宏展开无法判定该用哪一个。
var builtinControls = []string{"Text", "Button", "Input", "Image"}

// sourceMap 把字节偏移换算为行列号。
//
// GXML 规范要求行列从 1 开始，且列按字符（而非字节）计数，
// 否则含中文的源码会得到错误的列号。
type sourceMap struct {
	src string
	// lineStarts[i] 是第 i 行（0 基）首字节的偏移。
	lineStarts []int
}

func newSourceMap(src string) *sourceMap {
	m := &sourceMap{src: src, lineStarts: []int{0}}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			m.lineStarts = append(m.lineStarts, i+1)
		}
	}
	return m
}

// position 把字节偏移换算成 Position。偏移越界时钳到合法范围。
func (m *sourceMap) position(off int64) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > int64(len(m.src)) {
		off = int64(len(m.src))
	}
	i := int(off)

	// 二分查找所在行。
	n := sort.Search(len(m.lineStarts), func(k int) bool { return m.lineStarts[k] > i }) - 1
	if n < 0 {
		n = 0
	}
	lineStart := m.lineStarts[n]

	// 列按 rune 计数，保证中文与 ASCII 混排时列号符合编辑器直觉。
	col = utf8.RuneCountInString(m.src[lineStart:i]) + 1
	return n + 1, col
}

// span 由字节区间构造 SourceSpan。
func (m *sourceMap) span(start, end int64) Span {
	sl, sc := m.position(start)
	el, ec := m.position(end)
	return Span{Start: Position{Line: sl, Col: sc}, End: Position{Line: el, Col: ec}}
}

// String 便于调试。
func (m *sourceMap) String() string {
	return fmt.Sprintf("sourceMap(%d bytes, %d lines)", len(m.src), len(m.lineStarts))
}

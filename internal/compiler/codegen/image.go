package codegen

import (
	"fmt"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// imageExpr 生成 <Image> 的渲染表达式。
//
// v1 只支持静态 Src：绑定 Src 需要运行时资源解析，与「运行时不读文件」
// 的红线冲突，因此明确拒绝而不是生成一个空占位。
//
// 尺寸（Width/Height）由外层 sizeWrap 统一处理；这里只回答「用哪个 Gio 原语」。
// Fit 固定为 Contain：在用户给定尺寸内等比缩放且不裁剪；Position 居中。
// 若将来需要 Cover/平铺，应新增显式属性并走契约变更，不得在这里猜测。
func (g *generator) imageExpr(n *contracts.NodeIR) (string, error) {
	if len(n.Children) > 0 {
		return "", fmt.Errorf("codegen: <Image> must not have children")
	}
	if len(n.Events) > 0 {
		return "", fmt.Errorf("codegen: <Image> does not support events")
	}
	raw := ""
	found := false
	for _, a := range n.Attrs {
		if a.Name != "Src" {
			continue
		}
		if a.Value.IsBinding {
			return "", AssetPathDiagnostic(g.opts.SourcePath,
				AssetRef{Src: "<binding>", Span: n.Span},
				fmt.Errorf("Src must be a static literal in v1"))
		}
		raw, found = a.Value.Raw, true
		break
	}
	if !found {
		return "", AssetPathDiagnostic(g.opts.SourcePath,
			AssetRef{Span: n.Span}, fmt.Errorf("Src is required"))
	}
	normalized, err := NormalizeAssetSrc(raw)
	if err != nil {
		return "", AssetPathDiagnostic(g.opts.SourcePath,
			AssetRef{Src: raw, Span: n.Span}, err)
	}

	if g.assetSlots == nil {
		g.assetSlots = make(map[string]int)
	}
	slot, ok := g.assetSlots[normalized]
	if !ok {
		slot = len(g.assets)
		g.assetSlots[normalized] = slot
		g.assets = append(g.assets, normalized)
	}
	fitExpr, err := g.imageFitExpr(n)
	if err != nil {
		return "", err
	}
	g.imports.add("gioui.org/widget", "widget")
	return fmt.Sprintf(
		"widget.Image{Src: v.img%d, Fit: %s, Position: layout.Center}.Layout(gtx)", slot, fitExpr), nil
}

// imageFitExpr 把静态 Fit 属性映射为 Gio widget.Fit 标识符。
//
// v1 只接受静态字面量；默认 Contain。Cover/Unscaled 直接映射，
// 不引入项目自定义枚举——取值集合由 Gio 的 Fit 常量决定。
func (g *generator) imageFitExpr(n *contracts.NodeIR) (string, error) {
	// 直接检查 IR 属性，而不只依赖 Facts：Facts 缺失时 staticAttr 对绑定
	// 属性也返回 false，会被误当成「未设置」而静默取默认 Contain。
	for _, a := range n.Attrs {
		if a.Name == "Fit" && a.Value.IsBinding {
			return "", contracts.NewError(g.opts.SourcePath, n.Span, "",
				"<Image Fit> accepts only a literal in v1; data binding is not supported")
		}
	}
	raw, ok := staticAttr(n, "Fit")
	if !ok {
		return "widget.Contain", nil
	}
	switch strings.TrimSpace(raw) {
	case "Contain":
		return "widget.Contain", nil
	case "Cover":
		return "widget.Cover", nil
	case "Unscaled":
		return "widget.Unscaled", nil
	default:
		return "", contracts.NewError(g.opts.SourcePath, n.Span, "",
			"<Image Fit> expects Contain, Cover or Unscaled, got %q", raw)
	}
}

// writeImageDecode 生成构造期的资产解码块。
//
// 每个资产只解码一次并把 paint.ImageOp 存入字段；Layout 内不再触碰
// bytes/base64/image，满足「帧热路径零分配」与「运行时不读文件」。
//
// 静态页构造函数签名冻结为无 error（代码生成输出契约 §4.3），因此解码
// 失败只能 panic。该失败在正常链路不可达：pipeline 已在生成前用
// image.DecodeConfig 校验过同一批字节；panic 是生成文件被篡改/损坏的
// 不变式护栏，不是用户输入错误处理路径。
func writeImageDecode(b *strings.Builder, assets []string, assetVar, indent string, returnsError bool) {
	for i, p := range assets {
		raw := fmt.Sprintf("raw%d", i)
		img := fmt.Sprintf("img%d", i)
		pathLit := quote(p)

		b.WriteString(indent + raw + ", err := " + assetVar + ".ReadFile(" + pathLit + ")\n")
		b.WriteString(indent + "if err != nil {\n")
		if returnsError {
			b.WriteString(indent + "\treturn nil, fmt.Errorf(\"goxml: read embedded asset %q: %w\", " + pathLit + ", err)\n")
		} else {
			b.WriteString(indent + "\tpanic(\"goxml: read embedded asset \" + " + pathLit + " + \": \" + err.Error())\n")
		}
		b.WriteString(indent + "}\n")

		b.WriteString(indent + img + ", _, err := image.Decode(bytes.NewReader(" + raw + "))\n")
		b.WriteString(indent + "if err != nil {\n")
		if returnsError {
			b.WriteString(indent + "\treturn nil, fmt.Errorf(\"goxml: decode embedded asset %q: %w\", " + pathLit + ", err)\n")
		} else {
			b.WriteString(indent + "\tpanic(\"goxml: decode embedded asset \" + " + pathLit + " + \": \" + err.Error())\n")
		}
		b.WriteString(indent + "}\n")
		b.WriteString(indent + "v.img" + fmt.Sprint(i) + " = paint.NewImageOp(" + img + ")\n")
	}
}

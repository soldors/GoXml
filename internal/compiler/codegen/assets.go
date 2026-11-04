package codegen

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // 注册 JPEG 解码器，供 image.DecodeConfig 与生成代码使用。
	_ "image/png"  // 注册 PNG 解码器。
	"path"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// <Image> 静态资产
//
// 设计边界（ADR-0015、代码生成输出契约 §4.8）：
//   - 只接受静态 Src；相对**目标 Go 包目录**（生成文件所在目录）；
//   - Codegen 不读磁盘，只负责把路径写成 `//go:embed`；
//   - 存在性、可读性、格式与预算由 pipeline 在 Codegen 之前门禁；
//   - 生成代码在构造期解码一次并缓存 paint.ImageOp，Layout 不再解码。
//
// 为什么用 //go:embed 而不是把字节 Base64 进源码：
//   - 资产内容变化时生成文件不变，CLI 缓存键无需感知资产哈希；
//     Go 构建会按 embed 文件内容重新编译；
//   - 源码体积不随资产体积膨胀；
//   - embed.FS.ReadFile 仍是编译期数据，不违反「运行时不读文件」。
// ============================================================================

const (
	// MaxAssetBytes 是单个资产编码后允许的最大字节数（1 MiB）。
	//
	// 首版刻意保守：//go:embed 会进入最终二进制，过大的资产应由使用者
	// 在构建前压缩或经后续资产管线版本处理。
	MaxAssetBytes = 1 << 20

	// MaxAssetPixels 是解码后允许的最大像素数（4M，约 2000×2000）。
	//
	// 限制像素而非仅限制字节，是为了挡住「压缩比很高但解码后巨大」的
	// 资产在构造期一次性占用大量内存。
	MaxAssetPixels = 4 << 20
)

// AssetRef 是一个已通过路径归一化的 <Image Src> 引用。
type AssetRef struct {
	// Src 是源码中的原始属性值，仅用于诊断消息。
	Src string
	// Path 是归一化后的包内相对路径（统一 `/`）。
	Path string
	// Span 是 <Image> 节点在源码中的范围。
	Span contracts.SourceSpan
}

// AssetRefs 返回页面中所有唯一的静态 Image 资产引用，按首次出现顺序。
//
// 只遍历 Page：未使用的 <Def> 体内的 Image 不应进入 manifest 或二进制。
// 绑定 Src 不在此返回——它会在 Codegen 的 imageExpr 中被明确拒绝。
func AssetRefs(v *analyzer.VerifiedDoc) []AssetRef {
	if v == nil || v.Document() == nil {
		return nil
	}
	return AssetRefsFromDoc(v.Document())
}

// AssetRefsFromDoc 是 AssetRefs 的 IR 版本：在**仅展开、尚未类型检查**的
// DocumentIR 上收集静态 Image 资产引用。
//
// 存在理由（R17-A）：绑定页的 VM 包若因**旧生成文件的 //go:embed 指向已删除资产**
// 而加载失败，Analyzer 会先报 GXML-ROOT-002，把资产缺失错误地归到 VM 加载上。
// 在 Analyzer 之前用本函数跑资产门禁，可让「资产缺失/损坏」得到 CODEGEN-ASSET-*
// 的准确归属；其它（与资产无关的）VM/Go 加载错误不受影响，仍由 Analyzer 原样报出。
func AssetRefsFromDoc(doc *contracts.DocumentIR) []AssetRef {
	if doc == nil || doc.Page == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []AssetRef
	var walk func(*contracts.NodeIR)
	walk = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Tag == "Image" {
			if raw, ok := staticAttr(n, "Src"); ok {
				if normalized, err := NormalizeAssetSrc(raw); err == nil && !seen[normalized] {
					seen[normalized] = true
					out = append(out, AssetRef{Src: raw, Path: normalized, Span: n.Span})
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
		if n.If != nil {
			for _, c := range n.If.Then {
				walk(c)
			}
			for _, c := range n.If.Else {
				walk(c)
			}
		}
	}
	walk(doc.Page)
	return out
}

// CountAssets 返回页面中唯一静态 Image 资产数。
func CountAssets(v *analyzer.VerifiedDoc) int {
	return len(AssetRefs(v))
}

// NormalizeAssetSrc 把 Src 归一化为包内相对路径。
//
// 拒绝：
//   - 空值、控制字符；
//   - 绝对路径、Windows 卷名、UNC、盘符或 URL 风格冒号；
//   - 任何 `..` 段（先拒绝再 Clean，避免「先净化后放行」的歧义）；
//   - 以 `.` 或 `_` 开头的路径段——Go 的 //go:embed 默认不匹配这类文件，
//     与其生成一个构建期才失败的指令，不如在 Codegen 阶段明确报错。
func NormalizeAssetSrc(src string) (string, error) {
	raw := strings.TrimSpace(src)
	if raw == "" {
		return "", fmt.Errorf("Src is empty")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("Src contains a control character")
		}
	}
	p := strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("Src must be relative to the target package directory")
	}
	if strings.HasSuffix(p, "/") {
		return "", fmt.Errorf("Src must name a file, not a directory")
	}
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		return "", fmt.Errorf("Src must not contain a volume name")
	}
	if strings.Contains(p, ":") {
		return "", fmt.Errorf("Src must not contain ':'")
	}
	// //go:embed 的未加引号形态使用 path.Match；即使我们总是加引号，
	// 也不接受通配符，避免「以为在引用一个文件，实际匹配到多个」。
	if strings.ContainsAny(p, "*?[") {
		return "", fmt.Errorf("Src must not contain glob metacharacters")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("Src must not contain '..'")
		}
		if seg == "" || seg == "." {
			continue
		}
		if seg[0] == '.' || seg[0] == '_' {
			return "", fmt.Errorf("Src segment %q must not start with '.' or '_'", seg)
		}
	}
	clean := path.Clean(p)
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("Src does not name a file")
	}
	return clean, nil
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// AssetPathDiagnostic 把路径错误包装成 CODEGEN-ASSET-001。
func AssetPathDiagnostic(file string, ref AssetRef, err error) contracts.CompilerError {
	if ref.Src == "" {
		return contracts.NewError(file, ref.Span, CodeAssetPath, "%v", err)
	}
	return contracts.NewError(file, ref.Span, CodeAssetPath,
		"asset path %q: %v", ref.Src, err)
}

// AssetReadDiagnostic 把读取失败包装成 CODEGEN-ASSET-002。
func AssetReadDiagnostic(file string, ref AssetRef, err error) contracts.CompilerError {
	return contracts.NewError(file, ref.Span, CodeAssetLoad,
		"asset %q: %v", ref.Path, err)
}

// ValidateAssetBytes 校验资产字节与格式；invalid 为 true 时返回诊断。
//
// 先 DecodeConfig 检查格式与尺寸（避免对超大图先完整解码），再完整
// Decode 一次确认字节没有截断/损坏。后者是关键：静态页构造函数在
// Decode 失败时会 panic，若这里只验头，截断的 PNG 会通过门禁并在运行期
// 变成进程崩溃。完整解码的成本被 1 MiB / 4M 像素预算限制在可接受范围。
func ValidateAssetBytes(file string, ref AssetRef, data []byte) (diag contracts.CompilerError, invalid bool) {
	if len(data) > MaxAssetBytes {
		return contracts.NewError(file, ref.Span, CodeAssetLimit,
			"asset %q is %d bytes; limit is %d", ref.Path, len(data), MaxAssetBytes), true
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return contracts.NewError(file, ref.Span, CodeAssetLoad,
			"asset %q: %v", ref.Path, err), true
	}
	if format != "png" && format != "jpeg" {
		return contracts.NewError(file, ref.Span, CodeAssetLoad,
			"asset %q: unsupported format %q (PNG/JPEG only)", ref.Path, format), true
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return contracts.NewError(file, ref.Span, CodeAssetLoad,
			"asset %q has invalid dimensions %dx%d", ref.Path, cfg.Width, cfg.Height), true
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > MaxAssetPixels {
		return contracts.NewError(file, ref.Span, CodeAssetLimit,
			"asset %q is %dx%d (%d pixels); limit is %d", ref.Path, cfg.Width, cfg.Height, pixels, MaxAssetPixels), true
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return contracts.NewError(file, ref.Span, CodeAssetLoad,
			"asset %q: %v", ref.Path, err), true
	}
	return contracts.CompilerError{}, false
}

package cli

// R15-A：把 GOFLAGS 中 `-overlay=`/`-modfile=` 指向文件的内容纳入缓存身份（K11）。
//
// 问题：K10 只记录 GOFLAGS **字符串**。若字符串不变，只改 `-overlay` 指向的 JSON、
// 其替换源文件，或 `-modfile` 指向的替代模块文件内容，字符串相同 ⇒ 键不变 ⇒ 可能
// 沿用旧缓存。这里读取这些文件内容并算指纹，使内容变化 ⇒ 键变化（安全 miss）。
//
// 保守边界：任一被引用文件读不到/解析失败 ⇒ 返回 ok=false，调用方标 dependencyUnsafe
// 让本输入**保守 miss**（不误命中，也不硬失败——合法工程仍可生成）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// buildInputsFingerprint 计算 GOFLAGS 引用的 overlay/modfile 内容指纹。
//
// 返回 (fingerprint, ok)：
//   - 若 GOFLAGS 未引用 overlay/modfile，返回 ("", true)（无需额外分量）。
//   - 引用且全部可读：返回稳定指纹（含路径与内容 SHA-256），ok=true。
//   - 引用但读取/解析失败：ok=false（调用方保守 miss）。
func buildInputsFingerprint(goflags, moduleRoot string) (string, bool) {
	fields, err := goFlagsSplit(goflags)
	if err != nil {
		return "", false
	}
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(moduleRoot, p)
	}
	var overlayPath, modfilePath string
	for _, f := range fields {
		trimmed := strings.TrimLeft(f, "-")
		switch {
		case strings.HasPrefix(trimmed, "overlay="):
			overlayPath = strings.TrimPrefix(trimmed, "overlay=")
		case strings.HasPrefix(trimmed, "modfile="):
			modfilePath = strings.TrimPrefix(trimmed, "modfile=")
		case trimmed == "overlay" || trimmed == "modfile":
			// Go requires -flag=value for GOFLAGS options taking an argument.
			return "", false
		}
	}
	if overlayPath == "" && modfilePath == "" {
		return "", true
	}

	var b strings.Builder
	hashFile := func(rel string) ([]byte, error) {
		full := resolve(rel)
		data, rerr := os.ReadFile(full)
		if rerr != nil {
			return nil, rerr
		}
		sum := sha256.Sum256(data)
		b.WriteString("file:")
		b.WriteString(rel)
		b.WriteString("=")
		b.WriteString(hex.EncodeToString(sum[:]))
		b.WriteString("\n")
		return data, nil
	}

	if overlayPath != "" {
		// 解析 overlay JSON，把每个替换目标内容也算进去（替换源变化会影响构建）。
		data, rerr := hashFile(overlayPath)
		if rerr != nil {
			return "", false
		}
		var cfg struct{ Replace map[string]string }
		if jerr := json.Unmarshal(data, &cfg); jerr != nil {
			// 解析失败 ⇒ 无法可靠建模 ⇒ 保守 miss。
			return "", false
		}
		keys := make([]string, 0, len(cfg.Replace))
		for from := range cfg.Replace {
			keys = append(keys, from)
		}
		sort.Strings(keys)
		for _, from := range keys {
			to := cfg.Replace[from]
			if to == "" {
				// 删除语义：仅记录映射本身（无文件内容）。
				b.WriteString("overlay-del:")
				b.WriteString(from)
				b.WriteString("\n")
				continue
			}
			// 记录映射关系与替换源内容（替换源相对 moduleRoot 解析）。
			b.WriteString("overlay-map:")
			b.WriteString(from)
			b.WriteString("->")
			b.WriteString(to)
			b.WriteString("\n")
			if _, err := hashFile(to); err != nil {
				return "", false
			}
		}
	}

	if modfilePath != "" {
		if _, err := hashFile(modfilePath); err != nil {
			return "", false
		}
		// The Go tool uses the companion sum file for alternate modules.
		sumPath := strings.TrimSuffix(modfilePath, ".mod") + ".sum"
		if _, err := hashFile(sumPath); err != nil && !os.IsNotExist(err) {
			return "", false
		}
	}

	return b.String(), true
}

// ---- R15/R16：静态资产文件内容也进入缓存身份（K12）----
//
// 缺口：K3 只看目标包 **.go** 源；`<Image Src>` 引用的资产文件（PNG/JPEG）不在其中。
// 于是"资产被删除/改坏/改错大小写"时缓存**假命中**，旧产物沿用、资产门禁不再执行
// （历史反例：删掉 logo.png 后 generate 仍报"缓存命中"）。
//
// 这里解析 .gxml 的 IR、收集 Page 中**静态** Src，按目标目录读取资产文件内容算指纹，
// 使其进入缓存键：资产内容/尺寸/存在性变化 ⇒ 键变化 ⇒ 安全 miss ⇒ 门禁重新执行。
// 解析失败/前端有诊断 ⇒ 返回 ok=true 且空指纹（不在此处抢报；让阶段一按既有语义处理）。
// 无法读取资产（缺失/损坏）⇒ 指纹含"缺失/不可读"标记（键仍稳定），miss 后由门禁给正式诊断。

// assetRefsFromSource 解析并展开 .gxml，返回 Page 中按首次出现顺序的静态 Src（未去重）。
func assetRefsFromSource(file string, src []byte) []string {
	doc, perr := parser.Parse(file, src)
	if perr != nil && len(perr) > 0 && doc == nil {
		return nil
	}
	if doc == nil {
		return nil
	}
	res := expander.Expand(file, doc)
	if res.Doc == nil || res.Doc.Page == nil {
		return nil
	}
	var out []string
	var walk func(n *contracts.NodeIR)
	walk = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Tag == "Image" {
			for _, a := range n.Attrs {
				if a.Name == "Src" && !a.Value.IsBinding {
					out = append(out, a.Value.Raw)
					break
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
	walk(res.Doc.Page)
	return out
}

// assetsFingerprint 计算静态资产引用文件内容的稳定指纹（K12 分量）。
// 返回空串表示页面无静态资产（无需额外分量）。
func assetsFingerprint(file string, src []byte, targetDir string) string {
	srcs := assetRefsFromSource(file, src)
	if len(srcs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("assets:\n")
	for _, raw := range srcs {
		norm, err := codegen.NormalizeAssetSrc(raw)
		if err != nil {
			// 非法 Src 不入指纹（门禁会按 ASSET-001 报错；此处不抢报）。
			b.WriteString("invalid:" + raw + "\n")
			continue
		}
		full := filepath.Join(targetDir, filepath.FromSlash(norm))
		data, rerr := os.ReadFile(full)
		if rerr != nil {
			// 缺失/不可读：记稳定标记，使"存在→缺失"改变键（=> miss => 门禁报错）。
			b.WriteString("missing:" + norm + "\n")
			continue
		}
		sum := sha256.Sum256(data)
		b.WriteString("asset:" + norm + "=" + hex.EncodeToString(sum[:]) + "\n")
		// Windows 文件系统大小写不敏感：改错大小写时 ReadFile 仍成功、字节相同。
		// 把**磁盘实际拼写**也纳入键，使大小写错拼触发安全 miss => 门禁按 ASSET-001 报错。
		if actual, ok := onDiskSpelling(targetDir, norm); ok {
			b.WriteString("spelling:" + norm + "=>" + actual + "\n")
		}
	}
	return b.String()
}

// onDiskSpelling 返回 rel（/ 分隔）在 targetDir 下的**实际磁盘拼写**。
// 逐段读取目录项并按大小写不敏感匹配，返回真实名字；用于让"大小写错拼"进入缓存键。
func onDiskSpelling(targetDir, rel string) (string, bool) {
	segs := strings.Split(rel, "/")
	cur := targetDir
	actual := make([]string, 0, len(segs))
	for _, seg := range segs {
		entries, err := os.ReadDir(cur)
		if err != nil {
			return "", false
		}
		found := ""
		for _, e := range entries {
			if strings.EqualFold(e.Name(), seg) {
				found = e.Name()
				break
			}
		}
		if found == "" {
			return "", false
		}
		actual = append(actual, found)
		cur = filepath.Join(cur, found)
	}
	return strings.Join(actual, "/"), true
}

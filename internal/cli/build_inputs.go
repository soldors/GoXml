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

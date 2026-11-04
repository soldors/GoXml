package cli

// ============================================================================
// R10-A 批内目标包批处理解析：一次/有界分组 `go list -e -json` 查询多个已初始化目录，
// 按返回对象的 Dir 字段映射回各自目录，核「完整/唯一/无额外对象」，再对每个目录做与
// 单目录版本**同等**的严格校验（Name/InvalidGoFiles/选中文件包声明）。
//
// 正确性优先，不为提速省略任何所有权/错误校验：
//   - 权威查询失败（超时/坏 JSON/多对象/缺对象/无法归因）→ error，**绝不回退宿主标签**；
//   - 仅「真实未初始化目录」（目录不存在 / 无选中 Go 源）保留 ok=false 回退原 source 包名；
//   - 多包 / InvalidGoFiles / 选中文件声明不一致 / 读失败 → fail-closed；
//   - overlay 仍逐文件由调用方（targetNameOverlay）按 Manifest+保护头+Source 证明后传入，
//     批量只是把多目录的 overlay 合并进一份 -overlay JSON，不改变每个文件的证明来源。
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// batchListChunk 是单次 go list 查询的目录数上限（明确分组上限，避免超长 argv/大内存）。
const batchListChunk = 20

// batchTargetResult 是某目录的权威解析结果。
type batchTargetResult struct {
	name string
	ok   bool // true=已定名；false=真实未初始化（回退源包名）
	err  error
}

// resolveTargetPackagesBatch 批量解析 dirs（已初始化目标目录集合）。
// overlayFor 对单个 dir 返回该目录自有旧产物 overlay（同 targetNameOverlay 语义）。
// 返回 map[dir]batchTargetResult；无法归因/完整性问题以 error 形式放进对应或全局 err。
func resolveTargetPackagesBatch(goExe, moduleRoot string, env []string, dirs []string, overlayFor func(dir string) (map[string][]byte, map[string]bool, error)) (map[string]batchTargetResult, error) {
	if goExe == "" {
		goExe = "go"
	}
	res := make(map[string]batchTargetResult, len(dirs))
	// 去重 + 稳定顺序。
	uniq := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, d := range dirs {
		key := filepath.Clean(d)
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, d)
	}
	// 按 chunk 分组。
	for start := 0; start < len(uniq); start += batchListChunk {
		end := start + batchListChunk
		if end > len(uniq) {
			end = len(uniq)
		}
		chunk := uniq[start:end]
		if err := resolveOneChunk(goExe, moduleRoot, env, chunk, overlayFor, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// resolveOneChunk 对一组目录执行一次 go list -e -json，并把结果校验后填入 res。
func resolveOneChunk(goExe, moduleRoot string, env []string, chunk []string, overlayFor func(dir string) (map[string][]byte, map[string]bool, error), res map[string]batchTargetResult) error {
	// 合并本 chunk 的 overlay：只包含本 chunk 各目录的自有旧产物。
	mergedOverlay := map[string][]byte{}
	// dir → 绝对目录（用于按 .Dir 归因；go list 返回绝对 Dir）。
	absToDir := make(map[string]string, len(chunk))
	// 目录不存在/无选中源者无需查询（可直接 ok=false），但为了统一错误语义，仍可跳过查询。
	var query []string
	for _, d := range chunk {
		abs, err := filepath.Abs(d)
		if err != nil {
			res[d] = batchTargetResult{err: fmt.Errorf("解析目录绝对路径 %s: %w", d, err)}
			continue
		}
		var ov map[string][]byte
		var owned map[string]bool
		if overlayFor != nil {
			o, ow, oerr := overlayFor(d)
			if oerr != nil {
				res[d] = batchTargetResult{err: oerr}
				continue
			}
			ov, owned = o, ow
		}
		// 统一有效源探测：物理目录不存在也可由 overlay 提供；只排除已证工具自有产物
		// （owned：Manifest+保护头+Source），不做字节启发式。
		hasUser, perr := effectiveUserGoFiles(d, ov, owned)
		if perr != nil {
			res[d] = batchTargetResult{err: perr}
			continue
		}
		if !hasUser {
			// 无真实用户源（全为旧产物/测试/删除/空）→ 未初始化，回退源包名。
			res[d] = batchTargetResult{ok: false}
			continue
		}
		for p, b := range ov {
			mergedOverlay[p] = b
		}
		absToDir[normalizeDir(abs)] = d
		query = append(query, d)
	}
	if len(query) == 0 {
		return nil
	}

	// 一次 go list -e -json 查询本 chunk 的全部真实目录。
	ctx, cancel := context.WithTimeout(context.Background(), targetListTimeout)
	defer cancel()
	args := []string{"list", "-e", "-json"}
	var overlayFile string
	if len(mergedOverlay) > 0 {
		repl := make(map[string]string, len(mergedOverlay))
		for p, b := range mergedOverlay {
			if b == nil {
				repl[p] = ""
				continue
			}
			tmp, terr := writeOverlayReplacementFile(b)
			if terr != nil {
				return fmt.Errorf("写 overlay 替换文件失败: %w", terr)
			}
			defer os.Remove(tmp)
			repl[p] = tmp
		}
		ovJSON, jerr := jsonMarshalOverlay(repl)
		if jerr != nil {
			return jerr
		}
		f, ferr := os.CreateTemp("", "goxml-batch-overlay-*.json")
		if ferr != nil {
			return fmt.Errorf("写 overlay 文件: %w", ferr)
		}
		overlayFile = f.Name()
		if _, werr := f.Write(ovJSON); werr != nil {
			f.Close()
			os.Remove(overlayFile)
			return fmt.Errorf("写 overlay 文件: %w", werr)
		}
		f.Close()
		defer os.Remove(overlayFile)
		args = append(args, "-overlay", overlayFile)
	}
	args = append(args, query...)

	cmd := exec.CommandContext(ctx, goExe, args...)
	if moduleRoot != "" {
		cmd.Dir = moduleRoot
	}
	cmd.Env = append(cmdEnvBase(), "GOTOOLCHAIN=local")
	cmd.Env = append(cmd.Env, env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("go list -json 超时（%s）", targetListTimeout)
		}
		return fmt.Errorf("go list -json 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	pkgs, derr := decodeGoListJSON(out)
	if derr != nil {
		return derr
	}
	// 按 .Dir 归因。**只收集可归因且唯一**的对象；无法归因（如跨 module 目录 go list
	// 返回 Dir=""）/ 缺对象 / 多对象的目录**不填 batchRes**，交给 resolver 单查兜底——
	// 单查路径对未初始化/非法 import path/跨 module/多包给出与既有完全一致的诊断，
	// 不因个别目录的查询异常而使整批失败（也不静默把错误当成功）。
	byDir := make(map[string][]goListPkgObj)
	for _, p := range pkgs {
		bd, ok := absToDir[normalizeDir(p.Dir)]
		if !ok {
			// Unknown objects (including GOFLAGS=-deps dependencies) cannot be
			// silently accepted. A directory-less error object needs the existing
			// single-query diagnostic; a named external package is an extra object.
			if p.Dir == "" {
				return nil // leave this chunk for authoritative single-query fallback
			}
			return fmt.Errorf("go list -json 返回本批目标以外的包对象 %s（拒绝忽略额外对象）", p.Dir)
		}
		byDir[bd] = append(byDir[bd], p)
	}
	for _, d := range query {
		objs := byDir[d]
		if len(objs) != 1 {
			// 缺对象或多对象：不填 batchRes → 单查兜底（单查给出正确 fail-closed 诊断）。
			continue
		}
		name, files, verr := validateBatchObject(d, objs[0], mergedOverlay)
		if verr != nil {
			res[d] = batchTargetResult{err: verr}
			continue
		}
		_ = files
		res[d] = batchTargetResult{name: name, ok: true}
	}
	return nil
}

// validateBatchObject 对单个目录的 go list 对象做与 resolveTargetPackageGo 同等的严格校验。
func validateBatchObject(dir string, p goListPkgObj, overlay map[string][]byte) (string, []string, error) {
	if p.Name == "" {
		msg := "空包名"
		if p.Error != nil && p.Error.Err != "" {
			msg = p.Error.Err
		}
		return "", nil, fmt.Errorf("go list -json 未能确定 %s 的包名: %s", dir, msg)
	}
	if len(p.InvalidGoFiles) > 0 {
		detail := "invalid Go files"
		if p.Error != nil {
			detail = p.Error.Err
		}
		return "", nil, fmt.Errorf("目录 %s 存在多个非测试 package 或非法包声明: %s", dir, detail)
	}
	files := append([]string{}, p.GoFiles...)
	seen := map[string]bool{}
	for _, f := range p.GoFiles {
		seen[f] = true
	}
	for _, f := range p.CgoFiles {
		if !seen[f] {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return "", nil, fmt.Errorf("目标 %s 在有效构建上下文下无选中的 Go 源文件", dir)
	}
	for _, filename := range files {
		if filepath.Base(filename) != filename {
			return "", nil, fmt.Errorf("invalid selected Go filename %q", filename)
		}
		full := filepath.Join(dir, filename)
		// R21-A：overlay 优先（与单查/ go list / go build 一致），支持 overlay 新增文件。
		var data []byte
		if replacement, ok := overlay[full]; ok {
			if replacement == nil {
				// 用户 overlay 删除（空目标）：该文件应视为不存在。go list 通常已排除；
				// 若仍列出则跳过包子句校验，避免把「删除」误判成非法包。
				continue
			}
			data = replacement
		} else {
			rd, err := readPackageFile(full)
			if err != nil {
				return "", nil, fmt.Errorf("读取包源码 %s: %w", full, err)
			}
			data = rd
		}
		parsed, perr := parser.ParseFile(token.NewFileSet(), full, data, parser.PackageClauseOnly)
		if perr != nil {
			return "", nil, fmt.Errorf("解析包源码 %s: %w", full, perr)
		}
		if parsed.Name.Name != p.Name {
			return "", nil, fmt.Errorf("目录 %s 存在多个非测试 package（%s / %s）", dir, p.Name, parsed.Name.Name)
		}
	}
	return p.Name, files, nil
}

// normalizeDir 归一化目录路径用于 .Dir 映射比较（Windows 大小写/分隔符不敏感）。
func normalizeDir(p string) string {
	return normalizeDirCase(p, runtime.GOOS == "windows")
}

// Unix package directories differing only by case are distinct. Only Windows
// uses case folding for batch attribution; never merge Unix targets by spelling.
func normalizeDirCase(p string, fold bool) string {
	p = filepath.Clean(p)
	if fold {
		return strings.ToLower(p)
	}
	return p
}

// sortDirsStable 返回稳定排序副本（用于可复现的分组）。
func sortDirsStable(dirs []string) []string {
	out := append([]string{}, dirs...)
	sort.Strings(out)
	return out
}

// jsonMarshalOverlay 序列化 overlay 映射 {"Replace": {path: tmpfile}} —— 与单目录版本一致。
func jsonMarshalOverlay(repl map[string]string) ([]byte, error) {
	type ov struct {
		Replace map[string]string
	}
	return json.Marshal(ov{Replace: repl})
}

// collectTargetDirs 从输入集合枚举阶段零的全部目标目录（与 resolveInput 的 targetDir
// 计算一致：无 outRoot 用 sourceDir，有 outRoot 用 outRoot 下对应 rel 目录）。
func collectTargetDirs(inputs []discovered, outRoot string) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, in := range inputs {
		td := in.sourceDir
		if outRoot != "" {
			td = filepath.Join(outRoot, filepath.FromSlash(filepath.Dir(in.rel)))
		}
		key := normalizeDir(td)
		if seen[key] {
			continue
		}
		seen[key] = true
		dirs = append(dirs, td)
	}
	return dirs
}

// lookupBatch 归一化查 batch 结果。
func lookupBatch(batchRes map[string]batchTargetResult, dir string) (batchTargetResult, bool) {
	if r, ok := batchRes[dir]; ok {
		return r, true
	}
	want := normalizeDir(dir)
	for k, r := range batchRes {
		if normalizeDir(k) == want {
			return r, true
		}
	}
	return batchTargetResult{}, false
}

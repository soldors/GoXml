package cli

// ============================================================================
// R9-A S1 生产接线：目标包名/选文件由**当前 Go 工具链权威计算**（结构化 go list -json）。
//
// 背景（R8-A F3 两处真实后果，见 review/a-runtime）：
//   - 默认/冗余 GOEXPERIMENT=greenteagc 时真实 go list rc0 选中 enabled 包，旧候选 rc2 误拒；
//   - GOENV 持久化 windows/386、进程 GOARCH 未设时真实 go list 选 disabled 包，旧候选写成 enabled。
// 根因：`go env GOEXPERIMENT` 是 raw delta，空 delta 在不同架构下默认集不同；旧实现把
// 宿主 build.Default.ToolTags 当目标口径 → 与 go list 分叉（实测 repro0：386 下基线生成
// `package expenabled`，权威是 `expdisabled`）。
//
// S1 不再自行推导实验/CPU 标签：直接让 Go 对**目标有效环境**计算包名与选文件集合。
// 与隔离候选 78 行版本的关键差异（任务要求，不能照搬）：
//   - 用 **`go list -e -json`**消费结构化错误，不解析人类文本或盲取首行；
//   - 消费 `Name`/`GoFiles`/`Error`/`Incomplete` 结构化字段；
//   - 目标目录须**恰一个包对象**，多包/错误对象/读取失败 → fail-closed（带 cause）；
//   - 与 Analyzer/缓存同一有效 env（GOENV/GOFLAGS/GOEXPERIMENT/CPU 由实测或调用方注入）。
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

const targetListTimeout = 20 * time.Second

// goListPkgObj 是 `go list -json` 关心的字段子集。
type goListPkgObj struct {
	Dir            string
	Name           string
	ImportPath     string
	GoFiles        []string
	CgoFiles       []string
	InvalidGoFiles []string
	// Error 是加载/解析错误（-json 下非 nil 表示该包对象出错）。
	Error *struct {
		Err string
	}
	// Incomplete 表示包信息不完整（如缺依赖）。
	Incomplete bool
}

// resolveTargetPackageGo 用结构化 `go list -json` 在目标有效环境下解析目录的包名与选文件集合。
//
// 语义（生产契约）：
//   - dir 必须属 moduleRoot 这个 module（调用方保证已 findModuleRoot 对齐）；
//   - 结果须**恰一个包对象**；多对象/错误对象/Name 空 → error（fail-closed，带 cause）；
//   - 选中文件含 GoFiles ∪ CgoFiles（cgo 文件也是该包有效源）；
//   - 类型/依赖加载失败不在此处泛化拒绝：只消费**名称与文件选择**；若 `.Error` 是
//     语法/import 类错误，不阻断取 Name（后续 Analyzer 阶段按 cause 链报 ROOT-002 等），
//     但若 Name 为空或对象不唯一，仍 fail-closed。
//   - 受控子进程：显式 Go 路径、超时、GOTOOLCHAIN=local；失败/超时/坏 JSON → error。
//
// env 是被测目标的有效环境覆写（GOENV/GOOS/GOARCH/GOEXPERIMENT/GOAMD64...），
// 由调用方按与 Analyzer/缓存同一份实测值传入；不改系统/父环境。
//
// overlay（可选）是 go list -overlay 映射（path→替代字节），用于把**本工具旧生成物**
// 映射为 `//go:build ignore`，使定名只看到真实用户源（与 staleGeneratedOverlay 一致语义），
// 避免"旧产物的包名"参与本次目标包定名（R9-A 任务要求保留自有旧生成物排除）。
func resolveTargetPackageGo(goExe, moduleRoot, dir string, env []string, overlay map[string][]byte) (string, []string, error) {
	if goExe == "" {
		goExe = "go"
	}
	ctx, cancel := context.WithTimeout(context.Background(), targetListTimeout)
	defer cancel()
	args := []string{"list", "-e", "-json"}
	var overlayFile string
	if len(overlay) > 0 {
		repl := make(map[string]string, len(overlay))
		for p, b := range overlay {
			// go list -overlay 要求替换内容是**磁盘文件路径**，不是内联字节。
			tmp, terr := writeOverlayReplacementFile(b)
			if terr != nil {
				return "", nil, fmt.Errorf("写 overlay 替换文件失败: %w", terr)
			}
			defer os.Remove(tmp)
			repl[p] = tmp
		}
		ovJSON, jerr := json.Marshal(map[string]any{"Replace": repl})
		if jerr != nil {
			return "", nil, fmt.Errorf("序列化 overlay: %w", jerr)
		}
		f, ferr := os.CreateTemp("", "goxml-overlay-*.json")
		if ferr != nil {
			return "", nil, fmt.Errorf("写 overlay 文件: %w", ferr)
		}
		overlayFile = f.Name()
		if _, werr := f.Write(ovJSON); werr != nil {
			f.Close()
			os.Remove(overlayFile)
			return "", nil, fmt.Errorf("写 overlay 文件: %w", werr)
		}
		f.Close()
		defer os.Remove(overlayFile)
		args = append(args, "-overlay", overlayFile)
	}
	args = append(args, dir)
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
			return "", nil, fmt.Errorf("go list -json 超时（%s）", targetListTimeout)
		}
		return "", nil, fmt.Errorf("go list -json 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	pkgs, derr := decodeGoListJSON(out)
	if derr != nil {
		return "", nil, derr
	}
	if len(pkgs) != 1 {
		return "", nil, fmt.Errorf("go list -json 对 %s 返回 %d 个包对象（目标须恰一个 Go package）", dir, len(pkgs))
	}
	p := pkgs[0]
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
	// 选中文件：GoFiles ∪ CgoFiles（去重、保序）。
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
	// Independently prove every selected declaration; never substitute host MatchFile.
	for _, filename := range files {
		if filepath.Base(filename) != filename {
			return "", nil, fmt.Errorf("invalid selected Go filename %q", filename)
		}
		full := filepath.Join(dir, filename)
		data, err := readPackageFile(full)
		if err != nil {
			return "", nil, fmt.Errorf("读取包源码 %s: %w", full, err)
		}
		if replacement, ok := overlay[full]; ok {
			data = replacement
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), full, data, parser.PackageClauseOnly)
		if err != nil {
			return "", nil, fmt.Errorf("解析包源码 %s: %w", full, err)
		}
		if parsed.Name.Name != p.Name {
			return "", nil, fmt.Errorf("目录 %s 存在多个非测试 package（%s / %s）", dir, p.Name, parsed.Name.Name)
		}
	}
	return p.Name, files, nil
}

// decodeGoListJSON 把 `go list -json` 的连续 JSON 对象流解码为切片。
// go list -json 对多包会输出多个连续对象（无分隔）；用 json.Decoder 循环读取。
func decodeGoListJSON(data []byte) ([]goListPkgObj, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var pkgs []goListPkgObj
	for {
		var p goListPkgObj
		err := dec.Decode(&p)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("解析 go list -json 输出: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// ============================================================================
// 目标包解析器（批内复用，沿调用参数显式传递）。
//
// pipeline 阶段零设置一个闭包 resolver：对每个唯一 targetDir 复用同一 `go list -json`
// 结果（不每个 gxml 重复起子进程）；不设置包级可变状态。测试默认 nil → packageDirName
// 走既有 MatchFile 逻辑，保证既有测试零改动。
//
// resolver 语义：返回 (pkgName, ok, err)：
//   - ok=true：权威定名成功；
//   - ok=false,err=nil：目录不存在/为空/仅测试或已证自有产物（回退源包名）；
//   - err!=nil：多包/读取/解析失败（fail-closed）。
// ============================================================================

type targetPkgResolverFn func(dir string) (name string, ok bool, err error)

// newTargetPkgResolver 构造一个**批内缓存** resolver：同一 dir + moduleRoot 只跑一次
// `go list -json`。env 是目标有效环境的覆写（GOENV/GOOS/GOARCH/GOEXPERIMENT/CPU），
// 由调用方与 Analyzer/缓存共享有效环境。仅实际目录无用户非测试Go源时ok=false，
// Go按目标配置排除所有用户文件仍为错误；不按错误文案降级至宿主MatchFile。
func newTargetPkgResolver(goExe, moduleRoot string, env []string, overlayFor func(dir string) (map[string][]byte, error)) targetPkgResolverFn {
	type cacheEntry struct {
		name string
		ok   bool
		err  error
		done bool
	}
	cache := map[string]*cacheEntry{}
	return func(dir string) (string, bool, error) {
		key := dir
		e := cache[key]
		if e == nil {
			e = &cacheEntry{}
			cache[key] = e
			var overlay map[string][]byte
			if overlayFor != nil {
				ov, oerr := overlayFor(dir)
				if oerr != nil {
					e.err = oerr
					e.done = true
					return e.name, e.ok, e.err
				}
				overlay = ov
			}
			// Only genuinely uninitialised directories may use the source-package fallback.
			entries, statErr := os.ReadDir(dir)
			if os.IsNotExist(statErr) {
				e.done = true
				return "", false, nil
			}
			if statErr != nil {
				e.err = fmt.Errorf("读取目录 %s: %w", dir, statErr)
				e.done = true
				return "", false, e.err
			}
			count := 0
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
					continue
				}
				if _, owned := overlay[filepath.Join(dir, name)]; !owned {
					count++
				}
			}
			if count == 0 {
				e.done = true
				return "", false, nil
			}
			e.name, _, e.err = resolveTargetPackageGo(goExe, moduleRoot, dir, env, overlay)
			e.ok = e.err == nil

			e.done = true
		}
		return e.name, e.ok, e.err
	}
}

// writeOverlayReplacementFile 把 overlay 替代字节写成临时文件，返回路径（供 go list -overlay）。
func writeOverlayReplacementFile(b []byte) (string, error) {
	f, err := os.CreateTemp("", "goxml-ovl-*.go")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// targetNameOverlay 构造「目标定名 overlay」：对该 dir 下**本工具已登记且有保护头**的
// 产物，映射为 `//go:build ignore`，使 go list 定名时只看到真实用户源（排除旧产物包名）。
// 与 staleGeneratedOverlay 同语义，但用于**定名阶段**（写盘前、更早）。
// 只读：不挪/改/删任何目标或 Manifest。返回的 map 供 go list -overlay 使用。
func targetNameOverlay(moduleRoot string, man *manifest, dir string) (map[string][]byte, error) {
	if man == nil || len(man.Entries) == 0 {
		return nil, nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil
	}
	var overlay map[string][]byte
	for _, e := range man.Entries {
		full := filepath.Join(moduleRoot, filepath.FromSlash(e.Output))
		// 只处理落在该 dir 的产物。
		if !samePathDir(filepath.Dir(full), absDir) {
			continue
		}
		data, exists, rerr := readOutput(full)
		if rerr != nil {
			return nil, rerr
		}
		if !exists || !codegen.HasProtectedHeader(data) || !headerSourceMatches(data, filepath.Base(filepath.FromSlash(e.Source))) {
			continue
		}
		if overlay == nil {
			overlay = make(map[string][]byte)
		}
		overlay[full] = []byte("//go:build ignore\n\npackage ignore\n")
	}
	return overlay, nil
}

// samePathDir 比较两个目录是否同一路径（大小写不敏感，Windows 友好）。
func samePathDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

package pipeline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
)

// validateAssets 是 Codegen 之前的静态资产门禁（ADR-0015）。
//
// 职责划分：
//   - Codegen 只把 Src 写成 //go:embed，不读磁盘；
//   - pipeline 负责存在性、路径越界、格式与预算；
//   - 失败时返回带 CODEGEN-ASSET-* 与源码位置的诊断，不进入生成。
//
// 资产根目录是生成文件所在的 Go 包目录（TargetDir），不是 .gxml 源目录：
// //go:embed 的匹配范围由生成文件所在包决定，两者在 --out-dir 下可能不同。
func validateAssets(v *analyzer.VerifiedDoc, cfg Config, file string) []contracts.CompilerError {
	refs := codegen.AssetRefs(v)
	if len(refs) == 0 {
		return nil
	}
	root := cfg.TargetDir
	if root == "" {
		root = cfg.SourceDir
	}
	if root == "" {
		out := make([]contracts.CompilerError, 0, len(refs))
		for _, ref := range refs {
			out = append(out, codegen.AssetReadDiagnostic(file, ref,
				fmt.Errorf("target package directory is not configured")))
		}
		return out
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		out := make([]contracts.CompilerError, 0, len(refs))
		for _, ref := range refs {
			out = append(out, codegen.AssetReadDiagnostic(file, ref, err))
		}
		return out
	}

	out := make([]contracts.CompilerError, 0, len(refs))
	for _, ref := range refs {
		data, diag := readAssetBytes(rootAbs, file, ref)
		if diag != nil {
			out = append(out, *diag)
			continue
		}
		if diag, invalid := codegen.ValidateAssetBytes(file, ref, data); invalid {
			out = append(out, diag)
		}
	}
	return out
}

// ReadAssetForFingerprint reads an asset through the same path and byte-budget
// checks as the generation gate. Format validation remains in validateAssets.
func ReadAssetForFingerprint(root, rel string) ([]byte, bool) {
	norm, err := codegen.NormalizeAssetSrc(rel)
	if err != nil || root == "" {
		return nil, false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, false
	}
	data, diag := readAssetBytes(rootAbs, "", codegen.AssetRef{Path: norm})
	return data, diag == nil
}

func readAssetBytes(rootAbs, file string, ref codegen.AssetRef) ([]byte, *contracts.CompilerError) {
	full, err := assetPathInPackage(rootAbs, ref.Path)
	if err == nil {
		err = assetBoundaryProblem(rootAbs, ref.Path)
	}
	if err == nil {
		err = assetSpellingProblem(rootAbs, ref.Path)
	}
	if err != nil {
		d := codegen.AssetPathDiagnostic(file, ref, err)
		return nil, &d
	}
	readError := func(err error) ([]byte, *contracts.CompilerError) {
		d := codegen.AssetReadDiagnostic(file, ref, err)
		return nil, &d
	}
	tooLarge := func(size int64) ([]byte, *contracts.CompilerError) {
		d := contracts.NewError(file, ref.Span, codegen.CodeAssetLimit,
			"asset %q is %d bytes; limit is %d", ref.Path, size, codegen.MaxAssetBytes)
		return nil, &d
	}

	info, err := os.Stat(full)
	if err != nil {
		return readError(err)
	}
	if !info.Mode().IsRegular() {
		return readError(fmt.Errorf("%s is not a regular file", ref.Path))
	}
	if info.Size() > codegen.MaxAssetBytes {
		return tooLarge(info.Size())
	}
	f, err := os.Open(full)
	if err != nil {
		return readError(err)
	}
	defer f.Close()
	// LimitReader keeps growth after Stat from bypassing the byte budget.
	data, err := io.ReadAll(io.LimitReader(f, codegen.MaxAssetBytes+1))
	if err != nil {
		return readError(err)
	}
	if len(data) > codegen.MaxAssetBytes {
		return tooLarge(int64(len(data)))
	}
	return data, nil
}

// assetPathInPackage 把归一化相对路径拼到包目录，并复核未逃逸。
func assetPathInPackage(rootAbs, rel string) (string, error) {
	full := filepath.Join(rootAbs, filepath.FromSlash(rel))
	if !withinDir(rootAbs, full) {
		return "", fmt.Errorf("asset path %q escapes the package directory", rel)
	}
	return full, nil
}

func withinDir(rootAbs, candidate string) bool {
	rel, err := filepath.Rel(rootAbs, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// assetBoundaryProblem 逐级 Lstat 包目录以下的每个组件，证明资产不会经链接读到包外。
//
// 只检查包目录**以下**的组件：包目录本身经 junction/symlink 到达时，资产仍然
// 在这个包目录内，`//go:embed` 的匹配范围也由该包决定，因此不构成越界。
//
// 返回 nil 的两种情况语义不同，调用方都安全：
//   - 所有组件都是普通目录/文件 → 边界成立；
//   - 某组件不存在 → 后面没有可读的字节，存在性诊断由 Stat 负责。
//
// 其它 Lstat 失败（权限、IO）无法证明边界，按结构化失败处理，不降级为「先读再看」。
//
// 两条已知残留（不是本门禁能解决的问题，记录以免被当成已覆盖）：
//   - 硬链接：两个名字指向同一份数据，名字仍在包内时 Lstat 看不出差别。字节确实
//     来自包内路径，`//go:embed` 与 Go 构建看到的也是同一个包内名字，因此不判为越界；
//     要靠内容去重发现它需要 inode/卷内 ID，跨平台形态不在 v1 范围。
//   - TOCTOU：门禁通过到 `go build` 读盘之间资产仍可被外部替换。生成期不做事务锁，
//     这条时间窗由使用者（或后续把资产哈希纳入构建）承担，本文件不声称防住。
func assetBoundaryProblem(rootAbs, rel string) error {
	current := rootAbs
	for _, seg := range strings.Split(rel, "/") {
		current = filepath.Join(current, seg)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("cannot prove the package boundary at %q: %w", current, err)
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symbolic link; assets must be ordinary files inside the package directory", current)
		}
		// Go 1.26 在 Windows 上把目录 junction 报成 ModeIrregular（不是 ModeSymlink，
		// 也不是 IsDir），所以必须单独判这一位。
		if mode&os.ModeIrregular != 0 {
			return fmt.Errorf("path component %q is a junction or reparse point; its target cannot be proven to stay inside the package directory", current)
		}
	}
	return nil
}

// assetSpellingProblem 比对 Src 声明的拼写与磁盘上的实际拼写，逐级给出可修正建议。
//
// 为什么必须在编译期判：`//go:embed` 的模式匹配按宿主文件系统语义进行，大小写
// 不敏感的 Windows 会用**声明**的大小写建 FS 键并编译通过，而同一份生成文件在
// macOS/Linux 上匹配不到文件、要到 go build 才失败，且不带 .gxml 的源码位置。
//
// 只在**确证错配**时报错：某级既无精确项也无大小写匹配项时返回 nil，让存在性
// 检查给出更准确的 CODEGEN-ASSET-002。目录读不动时同样返回 nil，不猜测。
func assetSpellingProblem(rootAbs, rel string) error {
	declared := strings.Split(rel, "/")
	confirmed := make([]string, 0, len(declared))
	current := rootAbs
	firstSeg, firstActual := "", ""
	mismatched := 0
	for _, seg := range declared {
		entries, err := os.ReadDir(current)
		if err != nil {
			// 读不动就停在这里：已经确证过错配就照报，否则不猜测。
			if firstSeg != "" {
				return spellingError(firstSeg, firstActual, mismatched, confirmed)
			}
			return nil
		}
		exact, folded := "", ""
		for _, e := range entries {
			name := e.Name()
			if name == seg {
				exact = name
				break
			}
			if folded == "" && strings.EqualFold(name, seg) {
				folded = name
			}
		}
		actual := exact
		if actual == "" {
			actual = folded
		}
		if actual == "" {
			// 这一段既不精确也不折叠匹配：路径真的缺一段。已有错配照报，
			// 否则交给存在性检查给出 CODEGEN-ASSET-002。
			if firstSeg != "" {
				return spellingError(firstSeg, firstActual, mismatched, confirmed)
			}
			return nil
		}
		if exact == "" {
			mismatched++
			if firstSeg == "" {
				firstSeg, firstActual = seg, folded
			}
		}
		confirmed = append(confirmed, actual)
		current = filepath.Join(current, actual)
	}
	if firstSeg == "" {
		return nil
	}
	return spellingError(firstSeg, firstActual, mismatched, confirmed)
}

// spellingError 组装大小写诊断：指出首个错配段，并给出逐级修正后的完整路径。
func spellingError(seg, actual string, mismatched int, confirmed []string) error {
	use := strings.Join(confirmed, "/")
	if mismatched > 1 {
		return fmt.Errorf("%d path segments differ from the disk spelling; segment %q is spelled %q there, use %q",
			mismatched, seg, actual, use)
	}
	return fmt.Errorf("segment %q is spelled %q on disk; use %q", seg, actual, use)
}

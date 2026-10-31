// 本文件实现增量生成的命中判定与失效原因（TASK-GENERATE-CACHE-001 切片 B3）。
//
// 分层：
//   - cache_key.go   纯函数：算出键（K1–K7）与 K3 排除集
//   - cache_store.go 缓存记录的读写与损坏回退
//   - 本文件         把「输入 + Manifest + 磁盘现状」判成 hit 或 miss(reason)
//   - pipeline.go    按判定结果决定跳过还是生成，并负责输出与顺序
//
// 判定顺序是**冻结**的（任务卡 §3），顺序变了会改变用户看到的 reason，
// 也会改变「用户接管」这一硬错误的触发时机：
//
//	H1 Manifest 登记一致 → H2/H3 缓存记录可用 → H4 产物存在
//	→ H6 保护头（决定是否硬错误）→ H5 产物哈希
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// cacheMissReason 是未命中的原因（稳定词表，测试按此断言）。
//
// 这些字符串会出现在 stdout：「缓存未命中 <output>：<reason>」。
// 改词等于改用户体验，必须同步任务卡 §5。
type cacheMissReason string

const (
	// reasonBypassed 是 --no-cache 的内部原因：不读缓存，但**不输出**未命中行
	// （用户明确要求绕过，报「未命中」只会造成噪声）。
	reasonBypassed cacheMissReason = "cache-bypassed"
	// reasonMissing 表示该键没有缓存记录，且找不到同源的历史记录可解释。
	reasonMissing cacheMissReason = "cache-missing"
	// reasonCorrupt 表示缓存记录存在但不可用（坏 JSON／版本不符／自校验失败）。
	reasonCorrupt cacheMissReason = "cache-corrupt"
	// reasonInputChanged 表示 .gxml 内容变了。
	reasonInputChanged cacheMissReason = "input-changed"
	// reasonPackageSourcesChanged 表示同包 Go 源码指纹变了（K3）。
	reasonPackageSourcesChanged cacheMissReason = "package-sources-changed"
	// reasonCompilerVersionChanged 表示生成器版本变了（K4）。
	reasonCompilerVersionChanged cacheMissReason = "compiler-version-changed"
	// reasonOptionsChanged 表示产物路径/包名等 CLI 选项变了（K5/K6）。
	reasonOptionsChanged cacheMissReason = "options-changed"
	// reasonOutputMissing 表示缓存有效但产物被删了（H4）。
	reasonOutputMissing cacheMissReason = "output-missing"
	// reasonOutputModified 表示产物内容与记录不符（H5）：被手改，或换了源路径。
	reasonOutputModified cacheMissReason = "output-modified"
)

// String 实现 fmt.Stringer（便于日志与测试输出）。
func (r cacheMissReason) String() string { return string(r) }

// resolvedInput 是一个输入的解析结果（尚未生成）。
type resolvedInput struct {
	in discovered
	// pkgName 是目标包的包名（现行为：从**源目录**解析，保持既有语义）。
	pkgName string
	source  []byte
	// sourceHash 是 .gxml 内容哈希（K1）。
	sourceHash string
	// targetDir 是产物所在目录（--out-dir 时与源目录不同）。
	targetDir string
	// sourcePath 是保护头 `// Source:` 的值（相对目标包目录）。
	sourcePath string
	// target 是产物绝对路径。
	target string
	// sourceRel / outputRel 是项目相对路径（`/` 分隔），用于键与输出。
	sourceRel string
	outputRel string
}

// resolveInput 解析一个输入，不触碰磁盘写入。
func resolveInput(in discovered, moduleRoot, outRoot string) (*resolvedInput, error) {
	pkgName, err := packageDirName(in.sourceDir)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(in.abs)
	if err != nil {
		return nil, fmt.Errorf("读取源文件: %w", err)
	}

	targetDir := in.sourceDir
	if outRoot != "" {
		targetDir = filepath.Join(outRoot, filepath.FromSlash(filepath.Dir(in.rel)))
	}
	sourcePath := filepath.Base(in.abs)
	outputName := strings.TrimSuffix(sourcePath, ".gxml") + "_gen.go"
	target := filepath.Join(targetDir, outputName)

	srcRel, err := filepath.Rel(moduleRoot, in.abs)
	if err != nil {
		return nil, fmt.Errorf("计算源相对路径: %w", err)
	}
	outRel, err := filepath.Rel(moduleRoot, target)
	if err != nil {
		return nil, fmt.Errorf("计算输出相对路径: %w", err)
	}
	if err := validateOutputBoundary(moduleRoot, target); err != nil {
		return nil, err
	}

	return &resolvedInput{
		in:         in,
		pkgName:    pkgName,
		source:     src,
		sourceHash: inputHashOf(src),
		targetDir:  targetDir,
		sourcePath: sourcePath,
		target:     target,
		sourceRel:  filepath.ToSlash(srcRel),
		outputRel:  filepath.ToSlash(outRel),
	}, nil
}

// packageSourcesFingerprint 收集 K3：目标包内参与指纹的 Go 源码。
//
// 规则（任务卡 §2 的 D-1 裁决）：
//   - 只收 `*.go`，**排除 `*_test.go`**（测试不进入构建产物）；
//   - 排除集合见 fingerprintExclusion（Manifest 产物 ∪ 本批 target）——
//     不排除会让「自己写出的产物」进入指纹，命中率恒为 0（陷阱 T1）；
//   - 按文件名排序，保证结果稳定。
func packageSourcesFingerprint(dir string, exclude map[string]bool) ([]cachePackageSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// --out-dir 指向的新目录可能尚不存在：此时包内没有源码，指纹为空集。
		// 这不是错误——把它当错误会让「首次生成到新目录」直接失败。
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取包目录 %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if exclude[filepath.Clean(filepath.Join(dir, name))] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]cachePackageSource, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("读取包源码 %s: %w", name, err)
		}
		out = append(out, cachePackageSource{Rel: name, Hash: inputHashOf(data)})
	}
	return out, nil
}

// cacheKeyFor 为一输入算出缓存键。
func cacheKeyFor(r *resolvedInput, exclude map[string]bool) (string, error) {
	sources, err := packageSourcesFingerprint(r.targetDir, exclude)
	if err != nil {
		return "", err
	}
	return cacheKey(cacheKeyInput{
		SourceRel:       r.sourceRel,
		OutputRel:       r.outputRel,
		SourcePath:      r.sourcePath,
		SourceHash:      r.sourceHash,
		PackageName:     r.pkgName,
		BuildFlags:      os.Getenv("GOFLAGS"),
		CompilerVersion: codegen.GoxmlVersion,
		PackageSources:  sources,
	})
}

// manifestEntryOf 查找某源在 Manifest 中的登记（H1）。
func manifestEntryOf(man *manifest, sourceRel string) (entry, bool) {
	for _, e := range man.Entries {
		if e.Source == sourceRel {
			return e, true
		}
	}
	return entry{}, false
}

// loadSourceIndex 扫描缓存目录，建立「源路径 → 最近记录」索引。
//
// 用途仅限**解释未命中原因**：命中的判定永远只看本次键对应的记录。
// 索引里的记录属于「同源但键不同」的历史版本，用它可以区分
// input-changed / options-changed / compiler-version-changed / package-sources-changed。
//
// 扫描失败不致命：没有索引只会让原因退化为 cache-missing，不影响正确性。
func loadSourceIndex(moduleRoot string) map[string]cacheRecord {
	index := make(map[string]cacheRecord)
	dir := cacheDir(moduleRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return index
	}
	for _, e := range entries {
		if e.IsDir() || !cacheRecordNamePattern.MatchString(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rec cacheRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		if rec.Source == "" || rec.KeyHash != strings.TrimSuffix(e.Name(), ".json") {
			continue
		}
		// 同一源可能有多条历史记录：保留 CreatedAt 最新的一条。
		if prev, ok := index[rec.Source]; !ok || rec.CreatedAt > prev.CreatedAt {
			index[rec.Source] = rec
		}
	}
	return index
}

// explainMiss 用同源历史记录推断未命中原因。
//
// key 是本次算出的键：若历史记录的键与本次**相同**，说明缓存本身有效，
// 缺的只是 Manifest 登记（H1）——此时报 cache-missing 才准确，
// 不能误报成「包源码变了」。
func explainMiss(r *resolvedInput, key string, index map[string]cacheRecord) cacheMissReason {
	prev, ok := index[r.sourceRel]
	if !ok {
		return reasonMissing
	}
	if prev.KeyHash == key {
		return reasonMissing
	}
	switch {
	case prev.CompilerVersion != codegen.GoxmlVersion:
		return reasonCompilerVersionChanged
	case prev.SourceHash != r.sourceHash:
		return reasonInputChanged
	case prev.Output != r.outputRel:
		return reasonOptionsChanged
	default:
		// 源内容、输出路径、版本都没变，键却变了 → 只可能是 K3 或 SourcePath。
		return reasonPackageSourcesChanged
	}
}

// decideCacheHit 判定命中；返回 (是否命中, 未命中原因, 错误)。
//
// 错误只有两类，且都是**必须停下**的：
//   - 用户接管了产物（保护头被改）：覆盖会毁掉用户代码（契约 §13 第 3 条）；
//   - 读产物失败（权限等）。
//
// 其余一切「不可用」都退化为未命中——缓存不能成为新的失败源。
func decideCacheHit(moduleRoot string, r *resolvedInput, key string, man *manifest, index map[string]cacheRecord, noCache bool) (bool, cacheMissReason, error) {
	// 用户接管检查先于一切：即使本次注定 miss，也不能覆盖它。
	existing, exists, err := readOutput(r.target)
	if err != nil {
		return false, "", err
	}
	if exists && !codegen.HasProtectedHeader(existing) {
		return false, "", fmt.Errorf(
			"拒绝覆盖 %s：该文件没有 Goxml 保护头，视为用户接管（契约 §13 第 3 条）。"+
				"请删除该文件、或改回保护头后再生成", r.outputRel)
	}

	if noCache {
		return false, reasonBypassed, nil
	}

	// H1：Manifest 登记必须与本次一致。
	e, ok := manifestEntryOf(man, r.sourceRel)
	if !ok || e.Output != r.outputRel || e.InputHash != r.sourceHash || e.CompilerVersion != codegen.GoxmlVersion {
		return false, explainMiss(r, key, index), nil
	}

	// H2/H3：键对应的记录可用。
	rec, status, err := loadCacheRecord(moduleRoot, key)
	if err != nil {
		return false, "", err
	}
	if status == cacheCorrupt {
		return false, reasonCorrupt, nil
	}
	if status == cacheMissing {
		return false, explainMiss(r, key, index), nil
	}

	// H4：产物存在。
	if !exists {
		return false, reasonOutputMissing, nil
	}
	// H6 的 Source 一致性：保护头合法但 Source 指向别的源（改名/搬移后的旧产物）。
	// 这类产物归工具所有，可覆盖，但不算命中。
	if !headerSourceMatches(existing, r.sourcePath) {
		return false, reasonOutputModified, nil
	}
	// H5：产物内容与记录一致（识别手改）。
	if inputHashOf(existing) != rec.OutputHash {
		return false, reasonOutputModified, nil
	}
	return true, "", nil
}

// readOutput 读取产物；不存在返回 (nil, false, nil)。
func readOutput(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取产物 %s: %w", path, err)
	}
	return data, true, nil
}

// headerSourceMatches 判断产物第二行是否为 `// Source: <sourcePath>`。
func headerSourceMatches(data []byte, sourcePath string) bool {
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return false
	}
	return strings.TrimRight(lines[1], "\r") == "// Source: "+sourcePath
}

// newCacheRecordFor 组装一条缓存记录（产物哈希取**实际写入的字节**）。
func newCacheRecordFor(r *resolvedInput, key string, artifact []byte) cacheRecord {
	rec := newCacheRecord(key, r.sourceRel, r.outputRel, r.sourceHash, inputHashOf(artifact))
	rec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	return rec
}

// manifestOutputPaths 返回仍带保护头的 Manifest 产物（用于 K3 排除集）。
//
// 只排除 Manifest 产物不够（见陷阱 T1），调用方还要并上本批 target；
// 本函数只负责「已登记」这一半。
func manifestOutputPaths(moduleRoot string, man *manifest) []string {
	out := make([]string, 0, len(man.Entries))
	for _, e := range man.Entries {
		if e.Output == "" {
			continue
		}
		target := filepath.Join(moduleRoot, filepath.FromSlash(e.Output))
		// Manifest is untrusted ownership metadata. A user-owned Go file must
		// still participate in the fingerprint, even if listed as an output;
		// otherwise a cache hit can hide new syntax/binding/naming errors.
		data, err := os.ReadFile(target)
		if err != nil || !codegen.HasProtectedHeader(data) {
			continue
		}
		out = append(out, target)
	}
	return out
}

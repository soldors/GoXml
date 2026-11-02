// 本文件实现增量生成的缓存键（TASK-GENERATE-CACHE-001 切片 B1）。
//
// 本文件只做**纯函数**：给定已收集好的输入，算出缓存键 / 排除集。
// 不读磁盘、不决定是否命中——那些归 B2/B3（契约 §11）。
//
// 键必须覆盖契约 §11 要求的全部六项，缺一项就会「假命中」：
//
//	K1 源内容      cacheKeyInput.SourceHash
//	K2 <Def> 内容  v1 无跨文件 Def，已被 K1 覆盖（见 cacheKey 注释）
//	K3 包源码指纹  cacheKeyInput.PackageSources
//	K4 生成器版本  cacheKeyInput.CompilerVersion
//	K5 CLI 选项    cacheKeyInput.PackageName
//	K6 输出路径    OutputRel + SourcePath（后者进保护头，属产物内容）
//	K7 缓存格式    cacheFormatVersion
package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cacheFormatVersion 是缓存记录与缓存键的格式版本（K7）。
//
// 键的构成本身变化（增减分量、改变规范化方式）必须提升它，
// 否则旧缓存会被新算法当成命中——这正是「版本演进边界」要防的事。
//
// v3（PRODUCT-R5-B）：新增 K8 依赖闭包指纹（DependencySources）——
// 跨包输出的源 VM 包及其本模块传递闭包此前完全不在键内，改坏源 VM
// 会假命中（实测 repro-b1）。v2 旧记录因键变化自然失效（安全 miss），
// 无需迁移。
//
// v4（R5-FOLLOWUP-B 非作者审修）：K10 构建上下文从 os.Getenv 改为 go env 实测
// 的**真实有效上下文**（含 GOENV 持久化值），键含义变化 -> 升版使旧记录安全 miss。
// v5（R6 Background）：新生成器开始绘制叶子静态背景，拒绝复用旧版忽略背景的产物。
// v6（R8 Input）：负责人批准事件先行及程序化同步静默，旧v5生成物不可继续命中。
// GXML语言版本仍为1，只有内部缓存标识演进。
const cacheFormatVersion = 6

// cacheKeyInput 是缓存键的全部输入，均为**已解析的纯数据**。
//
// 路径一律项目相对、`/` 分隔：绝对路径会让同一项目在不同磁盘位置算出不同键，
// 缓存完全无法复用；validateKeyInput 会拒绝它们。
type cacheKeyInput struct {
	// SourceRel 是 .gxml 的项目相对路径（`/` 分隔），参与 K6。
	SourceRel string

	// OutputRel 是生成产物的项目相对路径（`/` 分隔），参与 K6。
	// --out-dir 变化会改变它，因此不需要单独把 outRoot 放进来。
	OutputRel string

	// SourcePath 是产物保护头 `// Source:` 的值（相对目标包目录），参与 K6。
	SourcePath string

	// SourceHash 是 .gxml 内容的 SHA-256（hex），即 K1。
	SourceHash string

	// PackageName 是目标 Go package 的包名（解析结果，CLI 选项的效果），即 K5。
	PackageName string
	// BuildFlags includes the effective GOFLAGS environment. A tag can change
	// model types without changing package names or any source-file bytes.
	BuildFlags string

	// CompilerVersion 是生成器版本，即 K4。
	CompilerVersion string

	// PackageSources 是目标包的相关 Go 源码指纹，即 K3。
	// 顺序不敏感：cacheKey 内部排序后写入。
	PackageSources []cachePackageSource

	// DependencySources 是源 VM 类型所在包及其本模块传递闭包的源码指纹，
	// 加模块身份（go.mod/go.sum），即 K8（PRODUCT-R5-B 新增）。
	// 跨包输出时源 VM 不在目标包内，K3 看不见它的变化——没有 K8 就会假命中。
	// 顺序不敏感：cacheKey 内部排序后写入。
	DependencySources []cachePackageSource

	// BuildContext 是有效构建上下文（GOOS/GOARCH/CGO_ENABLED 的取值，
	// 空串=宿主默认），即 K10（PRODUCT-R5-B 切片 B3）。
	// go list 对同一包目录的选择文件随这三项变化（build 标签）——
	// 键不含它们时，跨上下文的类型/文件选择变化会被旧记录假命中。
	BuildContext string
}

// cachePackageSource 是一个参与 K3 的 Go 源文件指纹。
type cachePackageSource struct {
	// Rel 是相对目标包目录的文件名（同包内唯一即可）。
	Rel string
	// Hash 是文件内容的 SHA-256（hex）。
	Hash string
}

// cacheKey 计算缓存键（形态：64 位 hex 的 SHA-256）。
//
// 规范化规则（冻结，见任务卡 §2）：
//   - 分量按固定顺序写入，标签与值都带 4 字节大端长度前缀；
//   - K3 按 Rel 字典序排序后写入；
//   - 任何路径分量都必须是相对路径。
//
// 为什么要长度前缀：直接拼接会让 ("ab","c") 与 ("a","bc") 得到同一串，
// 于是「源路径 A + 输出 B」和「源路径 A' + 输出 B'」可能算出同一个键。
// 长度前缀把每个字段变成自定界片段，消除这类歧义。
//
// 关于 K2（传递引用的 <Def>）：v1 的 `<Def>` 只能声明在同一个 .gxml 内，
// 其内容已整体进入 K1 的源哈希，因此单列 K2 是冗余的。
// **若将来支持跨文件 Def，必须在此显式拆出 K2 并提升 cacheFormatVersion。**
func cacheKey(in cacheKeyInput) (string, error) {
	if err := validateKeyInput(in); err != nil {
		return "", err
	}

	h := sha256.New()
	writeKeyField(h, "cacheFormat", strconv.Itoa(cacheFormatVersion))
	writeKeyField(h, "sourceRel", in.SourceRel)
	writeKeyField(h, "outputRel", in.OutputRel)
	writeKeyField(h, "sourcePath", in.SourcePath)
	writeKeyField(h, "sourceHash", in.SourceHash)
	writeKeyField(h, "packageName", in.PackageName)
	writeKeyField(h, "buildFlags", in.BuildFlags)
	writeKeyField(h, "compilerVersion", in.CompilerVersion)

	srcs := make([]cachePackageSource, len(in.PackageSources))
	copy(srcs, in.PackageSources)
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].Rel < srcs[j].Rel })

	// 文件数量显式入键：避免「N 个文件」与「N+1 个文件但内容拼接相同」混淆。
	writeKeyField(h, "packageSourceCount", strconv.Itoa(len(srcs)))
	for _, s := range srcs {
		writeKeyField(h, "packageSourceRel", s.Rel)
		writeKeyField(h, "packageSourceHash", s.Hash)
	}

	// K8 依赖闭包（PRODUCT-R5-B）：与 K3 同构写入（数量 + 逐条）。
	deps := make([]cachePackageSource, len(in.DependencySources))
	copy(deps, in.DependencySources)
	sort.Slice(deps, func(i, j int) bool { return deps[i].Rel < deps[j].Rel })
	writeKeyField(h, "dependencySourceCount", strconv.Itoa(len(deps)))
	for _, s := range deps {
		writeKeyField(h, "dependencySourceRel", s.Rel)
		writeKeyField(h, "dependencySourceHash", s.Hash)
	}
	// K10 构建上下文（切片 B3）：GOOS/GOARCH/CGO_ENABLED 影响包内文件选择
	// 与类型口径（build 标签），入键防跨上下文假命中。
	writeKeyField(h, "buildContext", in.BuildContext)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeKeyField 写入一个自定界字段：长度(标签) 标签 长度(值) 值。
func writeKeyField(h hash.Hash, label, value string) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(label)))
	h.Write(n[:])
	h.Write([]byte(label))
	binary.BigEndian.PutUint32(n[:], uint32(len(value)))
	h.Write(n[:])
	h.Write([]byte(value))
}

// validateKeyInput 拒绝会让缓存不可移植或含义不清的输入。
//
// 这些不是防御性冗余：绝对路径与空包名都会让「同一项目、同一输入」
// 算出不同键或无法区分项目，问题一旦发生表现为「缓存永不命中」，
// 比报错难排查得多。
func validateKeyInput(in cacheKeyInput) error {
	for _, f := range []struct {
		label string
		value string
	}{
		{"SourceRel", in.SourceRel},
		{"OutputRel", in.OutputRel},
		{"SourcePath", in.SourcePath},
	} {
		if err := validateRelativePath(f.label, f.value); err != nil {
			return err
		}
	}
	if in.SourceHash == "" {
		return fmt.Errorf("cacheKey: SourceHash 为空（K1 缺失会让所有源共用同一键）")
	}
	if in.PackageName == "" {
		return fmt.Errorf("cacheKey: PackageName 为空（K5 缺失会跨包假命中）")
	}
	if in.CompilerVersion == "" {
		return fmt.Errorf("cacheKey: CompilerVersion 为空（K4 缺失会让生成逻辑变化后仍命中）")
	}
	for _, s := range in.DependencySources {
		if s.Rel == "" || s.Hash == "" {
			return fmt.Errorf("cacheKey: 依赖闭包条目 Rel/Hash 为空")
		}
		if err := validateRelativePath("DependencySources.Rel", s.Rel); err != nil {
			return err
		}
	}
	for _, s := range in.PackageSources {
		if s.Rel == "" {
			return fmt.Errorf("cacheKey: 包源码条目 Rel 为空")
		}
		if err := validateRelativePath("PackageSources.Rel", s.Rel); err != nil {
			return err
		}
		if s.Hash == "" {
			return fmt.Errorf("cacheKey: 包源码 %q 的 Hash 为空", s.Rel)
		}
	}
	return nil
}

// validateRelativePath 校验路径为相对路径且不越出项目根。
func validateRelativePath(label, p string) error {
	if p == "" {
		return fmt.Errorf("cacheKey: %s 为空", label)
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("cacheKey: %s 必须是项目相对路径，得到绝对路径 %q", label, p)
	}
	// Windows 盘符形态（C:\x 或 C:/x）：filepath.IsAbs 在非该 OS 上未必判定。
	if len(p) >= 2 && p[1] == ':' {
		return fmt.Errorf("cacheKey: %s 必须是项目相对路径，得到盘符路径 %q", label, p)
	}
	for _, part := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if part == ".." {
			return fmt.Errorf("cacheKey: %s 不得包含 %q（越出项目根会让缓存跨项目复用）: %q", label, "..", p)
		}
	}
	return nil
}

// fingerprintExclusion 计算 K3（包源码指纹）必须排除的绝对路径集合。
//
// 两个来源缺一不可（任务卡 §2.1 的陷阱 T1）：
//
//	manifestOutputs —— Manifest 已登记的产物；
//	batchTargets    —— 本批**计划**写入的产物。
//
// 只排除「已登记」不够：Manifest 在批次提交**之后**才更新，
// 本批刚写出的 `_gen.go` 在算键那一刻还没登记。若把它算进 K3：
// 第一次运行后包目录多了产物 → 第二次算出的键必然不同 → **命中率恒为 0**，
// 而且这种 bug 不会报错，只会表现为「缓存永远不生效」。
func fingerprintExclusion(manifestOutputs, batchTargets []string) map[string]bool {
	ex := make(map[string]bool, len(manifestOutputs)+len(batchTargets))
	for _, list := range [][]string{manifestOutputs, batchTargets} {
		for _, p := range list {
			if p == "" {
				continue
			}
			ex[filepath.Clean(p)] = true
		}
	}
	return ex
}

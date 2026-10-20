package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/font/opentype"

	"github.com/goxml/goxml/contracts"
	platformfont "github.com/goxml/goxml/pkg/platform/font"
)

// ============================================================================
// TASK-FONT-001-A：CJK 系统字体探测 + contracts.FontProvider 接线
//
// 范围严格限定在 A 卡 §1.1：候选探测、解析失败继续、诊断、超时、缓存、
// Resolve 语义、命中后装配。**不含** B 嵌入兜底 / C fallback 链 / D 懒加载。
//
// 路线 B（ADR-0012 决策 1）：字体决策只发生在运行时；生成代码不受影响。
// ============================================================================

// 合法字体族名（A 卡 §4.1）。
//
// A 阶段**只实现 CJK 探测**：`Body` / `Mono` 的拉丁探测未实现，调用它们返回
// ErrFontNotFound 并说明原因——不返回「看起来能用」的默认值伪装成功
// （A 卡约束 5）。
const (
	// FamilyCJK 汉字（含日文汉字、韩文）。A 阶段唯一实现探测的族。
	FamilyCJK = "CJK"
	// FamilyBody 正文拉丁字体。A 阶段未实现探测。
	FamilyBody = "Body"
	// FamilyMono 等宽字体。A 阶段未实现探测。
	FamilyMono = "Mono"
)

// defaultProbeTimeout 是单个族探测的默认时间预算。
//
// 依据规范 §3.2「字体加载耗时 ≤ 20ms（冷启动内）」。注意这是**预算**不是保证：
// 见 probe 的超时语义说明。
const defaultProbeTimeout = 20 * time.Millisecond

// ReadFileFunc 读取候选字体文件。生产用 os.ReadFile；测试注入可控实现。
type ReadFileFunc func(path string) ([]byte, error)

// CandidateStatus 是候选字体的探测结果状态。
type CandidateStatus string

const (
	// StatusOK 命中并解析成功。
	StatusOK CandidateStatus = "ok"
	// StatusMissing 文件不存在。
	StatusMissing CandidateStatus = "missing"
	// StatusUnreadable 文件存在但读取失败（权限/IO）。
	StatusUnreadable CandidateStatus = "unreadable"
	// StatusParseFailed 读取成功但解析失败（损坏或非字体文件）。
	StatusParseFailed CandidateStatus = "parse_failed"
	// StatusSkippedTimeout 因 deadline 已过而**未被尝试**。
	StatusSkippedTimeout CandidateStatus = "skipped_timeout"
)

// CandidateResult 是单个候选的探测结果（诊断的最小单元）。
type CandidateResult struct {
	Path   string
	Status CandidateStatus
	// Err 是失败原因（StatusOK 时为 nil）。诊断要能回答"为什么"。
	Err error
	// Faces 是解析出的字体面数（仅 StatusOK 有意义）。
	Faces int
}

// ProbeReport 是一次探测的完整诊断。
//
// 可观测性是硬要求（规范 §2.3「探测结果必须可观测」，A 卡约束 10 的一半）：
// 调用方与测试必须能在**失败**时也拿到这份报告。
type ProbeReport struct {
	Family     string
	Candidates []CandidateResult
	// Resolved 是命中的候选路径；空表示未命中。
	Resolved string
	// ID 是命中集合的稳定标识（空表示未命中）。
	ID string
	// Err 是最终错误（成功为 nil）。
	Err error
	// Timeout 表示发生过「因 deadline 放弃后续候选」。
	Timeout bool
	// Elapsed 是探测实际耗时（可能**超过** Timeout，见 probe 的超时语义）。
	Elapsed time.Duration
	// Scanned 是实际发生读取/解析尝试的候选数（缓存命中的测试用它证明没重扫）。
	Scanned int
}

// String 返回可打印的诊断文本。
func (r *ProbeReport) String() string {
	if r == nil {
		return "<nil report>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "probe family=%s elapsed=%v timeout=%v", r.Family, r.Elapsed, r.Timeout)
	if r.Resolved != "" {
		fmt.Fprintf(&b, " resolved=%s id=%s", r.Resolved, r.ID)
	}
	if r.Err != nil {
		fmt.Fprintf(&b, " err=%v", r.Err)
	}
	for _, c := range r.Candidates {
		if c.Err != nil {
			fmt.Fprintf(&b, "\n  - %s [%s] %v", c.Path, c.Status, c.Err)
			continue
		}
		fmt.Fprintf(&b, "\n  - %s [%s]", c.Path, c.Status)
	}
	return b.String()
}

// fontSet 是「稳定标识 → 已解析字体集合」的绑定（评审要点 1）。
//
// 关键约束：标识必须能**反查**到解析好的 `[]font.FontFace`，
// 不得只返回文件名然后在别处重新扫描、重新解析。
type fontSet struct {
	ID    string
	Path  string
	Faces []font.FontFace
}

// ProviderOptions 配置一个 Provider。
type ProviderOptions struct {
	// Read 读取候选文件；nil 表示 os.ReadFile。
	Read ReadFileFunc
	// Timeout 探测时间预算；0 表示 defaultProbeTimeout。
	Timeout time.Duration
	// Candidates 候选路径；nil 表示当前平台的 platformfont.CJKCandidates()。
	Candidates []string
	// Now 注入时钟；nil 表示 time.Now。
	Now func() time.Time
}

func (o ProviderOptions) withDefaults() ProviderOptions {
	if o.Read == nil {
		o.Read = os.ReadFile
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultProbeTimeout
	}
	if o.Candidates == nil {
		o.Candidates = platformfont.CJKCandidates()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Provider 实现 contracts.FontProvider：探测 + 注册 + 缓存。
//
// 同一个对象同时管理「Resolve 结果」与「已解析集合」，这是评审要点 1 的落地：
// 标识与集合由**同一份缓存**持有，不可能出现"标识存在但集合丢失/需重扫"。
type Provider struct {
	mu       sync.Mutex
	opts     ProviderOptions
	sets     map[string]*fontSet
	resolved map[string]string
	scans    int
	last     *ProbeReport
}

// 编译期证明接线：本类型实现冻结的契约接口。
var _ contracts.FontProvider = (*Provider)(nil)

// NewProvider 创建 Provider。零值选项全部取默认（见 ProviderOptions）。
//
// 注意：构造**不**做任何读取或解析——规范 §3.3 禁止在 init/main 同步加载字体。
func NewProvider(opts ProviderOptions) *Provider {
	return &Provider{
		opts:     opts.withDefaults(),
		sets:     make(map[string]*fontSet),
		resolved: make(map[string]string),
	}
}

// Resolve 返回 family 对应的字体集合标识（contracts.FontProvider）。
//
// 语义（A 卡 §4）：
//   - 命中：返回非空、稳定、可反查的标识；
//   - 未命中/未实现/未知族名：返回包装了 contracts.ErrFontNotFound 的错误，
//     **不**返回空串 + nil，**不**伪装成默认字体。
//   - 成功的解析结果会被缓存：同一 family 二次调用不重新扫描磁盘。
//     失败**不**缓存，以便环境变化后可以重试。
func (p *Provider) Resolve(family string) (string, error) {
	id, _, err := p.ResolveWithReport(family)
	return id, err
}

// ResolveWithReport 与 Resolve 相同，另外返回诊断报告。
func (p *Provider) ResolveWithReport(family string) (string, *ProbeReport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if id, ok := p.resolved[family]; ok {
		return id, p.last, nil
	}
	switch family {
	case FamilyCJK:
		// A 阶段唯一实现探测的族。
	case FamilyBody, FamilyMono:
		// 严格失败，不伪装：A 只探测 CJK，拉丁族探测未实现。
		return "", nil, fmt.Errorf("%w: family %q 在 A 阶段未实现探测（A 只探测 %s）",
			contracts.ErrFontNotFound, family, FamilyCJK)
	default:
		return "", nil, fmt.Errorf("%w: unknown family %q (合法值: %s, %s, %s)",
			contracts.ErrFontNotFound, family, FamilyCJK, FamilyBody, FamilyMono)
	}

	p.scans++
	id, faces, rep := probe(p.opts)
	p.last = rep
	if rep.Err != nil {
		return "", rep, rep.Err
	}
	p.sets[id] = &fontSet{ID: id, Path: rep.Resolved, Faces: faces}
	p.resolved[family] = id
	rep.ID = id
	return id, rep, nil
}

// Faces 按标识反查已解析的字体集合（评审要点 1 的验证入口）。
func (p *Provider) Faces(id string) ([]font.FontFace, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	set, ok := p.sets[id]
	if !ok {
		return nil, false
	}
	return set.Faces, true
}

// LastReport 返回最近一次探测的诊断报告（未探测过时为 nil）。
func (p *Provider) LastReport() *ProbeReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

// Scans 返回实际发生磁盘扫描的次数（测试用：证明缓存命中不再扫描）。
func (p *Provider) Scans() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scans
}

// probe 顺序尝试候选，返回诊断与命中的字体集合。
//
// # 超时语义（A 卡约束 3；评审要点 2）
//
// 必须区分两件事，不得混为一谈：
//
//  1. **逻辑超时**：deadline 过后不再**启动**新的候选，调用方结束等待，
//     报告记录 Timeout=true 并把剩余候选标为 StatusSkippedTimeout。
//  2. **底层终止**：`os.ReadFile` 与 `opentype.ParseCollection` 都是同步、
//     不可取消的操作。Go 没有可移植的方法中止一次已发出的文件读取。
//
// 本实现的取舍是**不使用 goroutine 丢弃（abandonment）**：
// 把读/解析丢进 goroutine 然后超时返回，只会让"调用方不再等待"看起来像
// "操作已取消"，而后台任务其实还在跑——多次探测会累积成无法回收的后台工作。
// 代价是：若某次 read 真的挂住，probe 的总耗时会**超过** Timeout，
// 报告里的 Elapsed 会如实反映（不会把超时包装成"准时返回"）。
//
// 因此：不要把「Resolve 已返回」误读为「底层 IO 已终止」。
func probe(opts ProviderOptions) (string, []font.FontFace, *ProbeReport) {
	opts = opts.withDefaults()
	start := opts.Now()
	rep := &ProbeReport{Family: FamilyCJK}
	deadline := start.Add(opts.Timeout)

	if len(opts.Candidates) == 0 {
		rep.Err = fmt.Errorf("%w: 当前平台无 CJK 候选路径", contracts.ErrFontNotFound)
		rep.Elapsed = opts.Now().Sub(start)
		return "", nil, rep
	}

	for i, path := range opts.Candidates {
		if !opts.Now().Before(deadline) {
			rep.Timeout = true
			for _, rest := range opts.Candidates[i:] {
				rep.Candidates = append(rep.Candidates, CandidateResult{
					Path:   rest,
					Status: StatusSkippedTimeout,
				})
			}
			break
		}
		rep.Scanned++

		data, err := opts.Read(path)
		if err != nil {
			status := StatusUnreadable
			if errors.Is(err, os.ErrNotExist) {
				status = StatusMissing
			}
			rep.Candidates = append(rep.Candidates, CandidateResult{Path: path, Status: status, Err: err})
			continue
		}
		faces, err := opentype.ParseCollection(data)
		if err != nil {
			// 文件存在但不是有效字体：记录原因并继续下一个候选（规范 §2.3）。
			rep.Candidates = append(rep.Candidates, CandidateResult{
				Path: path, Status: StatusParseFailed, Err: err,
			})
			continue
		}
		if len(faces) == 0 {
			rep.Candidates = append(rep.Candidates, CandidateResult{
				Path: path, Status: StatusParseFailed,
				Err: fmt.Errorf("collection 解析结果为空"),
			})
			continue
		}
		rep.Candidates = append(rep.Candidates, CandidateResult{
			Path: path, Status: StatusOK, Faces: len(faces),
		})
		rep.Resolved = path
		id := setID(path)
		rep.Elapsed = opts.Now().Sub(start)
		return id, faces, rep
	}

	rep.Elapsed = opts.Now().Sub(start)
	if rep.Err == nil {
		rep.Err = fmt.Errorf("%w: %d 个候选中无可用 CJK 字体", contracts.ErrFontNotFound, len(rep.Candidates))
	}
	return "", nil, rep
}

// setID 由命中路径生成稳定标识。
//
// 标识必须：非空、稳定、可比较、可诊断（A 卡 §4.2）。它指向**整个**解析结果
// （一个 .ttc 可能含多个 face），反查入口是 Provider.Faces。
func setID(path string) string {
	return "system:" + filepath.Base(path)
}

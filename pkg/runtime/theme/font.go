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
	"github.com/goxml/goxml/pkg/runtime/theme/embedded"
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
	// StatusCoverageInsufficient 解析成功，但按字形判定的 CJK 覆盖不足（C 卡 §3）。
	//
	// 与 StatusParseFailed 的区别很重要：文件是**好的**，只是它不是可用的 CJK
	// 字体（例如纯拉丁字体）。没有这个状态时，这类字体会被当成命中成功，
	// 于是汉字全部渲染成 notdef 且不报错——正是规范 §1.2 禁止的静默降级。
	StatusCoverageInsufficient CandidateStatus = "coverage_insufficient"
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
	// Elapsed 是**本报告所记录的那一次探测**的耗时
	// （可能**超过** Timeout，见 probe 的超时语义）。
	//
	// 与 `LoadReport.ProbeElapsed` 的对照（D-ACCEPT-07 冻结，两者**不可混用**）：
	//
	//	- 本字段＝这份报告所描述的**那一次**探测花了多久。
	//	  缓存命中返回的是历史报告的派生副本，本字段因此**可能是历史值**——
	//	  这是有意为之：它仍如实描述它所记录的那次探测。
	//	- `LoadReport.ProbeElapsed`＝**本轮**加载实际花在探测上的时间，
	//	  本轮没探测就是 0。
	//
	// 要判断"本轮到底跑没跑探测"，看 `ProbeRan`，**不要**看本字段是否为 0。
	Elapsed time.Duration
	// ProbeRan 表示本报告所描述的**调用**是否实际执行了探测流程。
	//
	// 三个必须分清的点（D-ACCEPT-07 冻结）：
	//
	//  1. 它描述的是「**当前返回的这份报告所对应的那次调用**」，
	//     不是"历史上是否曾经探测过"。缓存命中时返回的是**派生副本**，
	//     副本里本字段为 false，而缓存中原始报告对象里它仍为 true——
	//     同一个类型在不同对象中指代不同，必须配合派生关系理解，
	//     **不得**跨对象混读。
	//  2. 语义是"是否执行了**探测流程**"，**不是**"是否至少扫了一个候选"。
	//     候选数为 0 时 `probe()` 照样执行、构造报告后早退，
	//     那时本字段仍为 true（零工作量≠没执行）。
	//  3. **`false` 不等价于"缓存命中"**。它只表示本轮没有执行探测流程；
	//     例如未知 family 从未进入探测流程，`theme.go` 会合成一份报告，
	//     其零值同样是 false。**不得**据 false 反推具体原因。
	//
	// 只有两处置值：`probe()` 在构造报告时置真（唯一构造点），
	// 以及 `ResolveWithReport` 在缓存命中时对派生副本置假。
	ProbeRan bool
	// Scanned 是实际发生读取/解析尝试的候选数（缓存命中的测试用它证明没重扫）。
	Scanned int
	// Embedded 是嵌入兜底链路的诊断（B 卡 §4）。
	//
	// 系统命中时**必须**为 nil 或 Attempted=false：那证明嵌入字体没有被解析，
	// 而不是"解析了但没用上"。这是 B4 的可观测断言点。
	Embedded *EmbeddedResult
	// Coverage 是命中字体的按字形覆盖判定结果（C 卡 §3）。
	//
	// 系统全失败（走嵌入兜底）时为 nil——那时没有"系统字体覆盖如何"这回事。
	Coverage *CoverageResult
}

// EmbeddedResult 是嵌入兜底链路的诊断。
//
// 与 CandidateResult 分开而不是塞进 Candidates，是因为两条链路的语义不同：
// 系统候选是"按路径逐个试"，嵌入兜底是"一次加载 + 一次解析"。
type EmbeddedResult struct {
	// Attempted 表示是否真的走了嵌入链路（系统命中时为 false）。
	Attempted bool
	// ID 是命中后的稳定标识（形如 embedded:GoxmlSansSC-Regular）。
	ID string
	// Bytes 是嵌入字体字节数（Attempted 且加载成功时有意义）。
	Bytes int
	// Faces 是解析出的字体面数。
	Faces int
	// Err 是失败原因。
	Err error
	// Elapsed 是嵌入链路的耗时（加载 + 解析，同步）。
	Elapsed time.Duration
}

// String 返回可打印的诊断文本。
func (e *EmbeddedResult) String() string {
	if e == nil {
		return "<未尝试嵌入兜底>"
	}
	if !e.Attempted {
		return "<未尝试嵌入兜底（系统已命中）>"
	}
	s := fmt.Sprintf("embedded attempted bytes=%d faces=%d elapsed=%v", e.Bytes, e.Faces, e.Elapsed)
	if e.ID != "" {
		s += " id=" + e.ID
	}
	if e.Err != nil {
		s += " err=" + e.Err.Error()
	}
	return s
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
	if r.Embedded != nil {
		fmt.Fprintf(&b, "\n  * %s", r.Embedded)
	}
	if r.Coverage != nil {
		fmt.Fprintf(&b, "\n  * %s", r.Coverage)
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

// EmbeddedFontLoader 提供嵌入兜底字体的字节（B 卡 §3/§4）。
//
// 抽成接口的唯一理由是**可观测**：B4 要求证明「系统命中时不解析嵌入字体」，
// 而那只能用计数器注入来证明——直接调用 embedded 包就没法计数了。
type EmbeddedFontLoader interface {
	// Load 返回字体字节与其稳定标识名。不可用时返回错误（不得返回空字节 + nil）。
	Load() (data []byte, name string, err error)
}

// defaultEmbeddedLoader 读取 //go:embed 进来的子集字体。
type defaultEmbeddedLoader struct{}

func (defaultEmbeddedLoader) Load() ([]byte, string, error) {
	a, err := embedded.Load()
	if err != nil {
		return nil, "", err
	}
	return a.Font, embedded.PostScriptName, nil
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
	// Embedded 嵌入兜底字体加载器；nil 表示 //go:embed 的子集字体。
	//
	// 它只在**系统候选全部不可用**时才被调用（B 卡 §4），系统命中时不会被触碰。
	Embedded EmbeddedFontLoader
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
	if o.Embedded == nil {
		o.Embedded = defaultEmbeddedLoader{}
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

// ResolveWithReport 与 Resolve 相同，另外返回诊断报告的独立副本。
// 报告中的切片和嵌套诊断归调用方所有；修改它们不影响缓存或其他调用方。
func (p *Provider) ResolveWithReport(family string) (string, *ProbeReport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if id, ok := p.resolved[family]; ok {
		// 缓存命中：本轮**没有**执行探测流程，但历史诊断（`Candidates` /
		// `Embedded` / `Coverage`）对排查仍有价值，**必须保留**。
		//
		// 因此不能直接交出 `p.last`：那既会把「本轮没探测」说成「探测过」，
		// 又会让调用方拿到 Provider 的内部对象（改动它会污染缓存，
		// 而改的人往往以为自己拿到的是副本）。
		//
		// 做法是交出**独立的派生副本**：历史字段照抄，本轮执行状态置假。
		// 深拷贝复用 `copyProbeReport`（与 `Lazy` 侧同一手法），
		// 免得两处副本行为分叉。
		derived := copyProbeReport(p.last)
		if derived != nil {
			derived.ProbeRan = false
		}
		return id, derived, nil
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
	if rep.Err == nil {
		// C 卡 §4：系统命中但有字形缺口 → 把嵌入子集追加到集合**末尾**补链。
		faces = p.supplement(faces, rep)
		p.sets[id] = &fontSet{ID: id, Path: rep.Resolved, Faces: faces}
		p.resolved[family] = id
		rep.ID = id
		return id, copyProbeReport(rep), nil
	}

	// 系统链路失败 → 嵌入兜底（B 卡 §4 第 2 步）。
	//
	// 注意顺序：只有系统**全部**不可用时才走到这里，因此「系统命中时
	// 不解析嵌入字体」是由控制流保证的，而不是靠开关。
	sysErr := rep.Err
	embID, embFaces, embRes := p.loadEmbedded()
	rep.Embedded = embRes
	if embRes.Err == nil {
		p.sets[embID] = &fontSet{ID: embID, Path: "embedded", Faces: embFaces}
		p.resolved[family] = embID
		rep.ID = embID
		// 系统链路确实全失败了，但整体解析成功：Err 必须清空，
		// 否则调用方会把"已兜底成功"读成"失败"。
		rep.Err = nil
		return embID, copyProbeReport(rep), nil
	}

	// 两条链路都失败 → 严格失败，且错误里两条链路的原因都要在（B 卡 §6）。
	rep.Err = fmt.Errorf("%w: 系统候选 %d 个与嵌入兜底均不可用（system: %v | embedded: %v）",
		contracts.ErrFontNotFound, len(rep.Candidates), sysErr, embRes.Err)
	return "", copyProbeReport(rep), rep.Err
}

// loadEmbedded 加载并解析嵌入字体，返回标识、集合与诊断。
//
// 同步执行：B 不实现异步/懒加载（B 卡 §9 风险 3）。实测解析耗时约 1.6ms，
// 远低于系统探测的 ~110ms，因此不引入异步。
func (p *Provider) loadEmbedded() (string, []font.FontFace, *EmbeddedResult) {
	res := &EmbeddedResult{}
	start := p.opts.Now()
	defer func() { res.Elapsed = p.opts.Now().Sub(start) }()

	res.Attempted = true
	data, name, err := p.opts.Embedded.Load()
	if err != nil {
		res.Err = fmt.Errorf("加载嵌入字体失败: %w", err)
		return "", nil, res
	}
	if len(data) == 0 {
		// 空字节 + nil 错误属于伪装成功，必须转成显式错误。
		res.Err = errors.New("加载嵌入字体失败: 字节为空")
		return "", nil, res
	}
	res.Bytes = len(data)

	faces, err := opentype.ParseCollection(data)
	if err != nil {
		res.Err = fmt.Errorf("解析嵌入字体失败: %w", err)
		return "", nil, res
	}
	if len(faces) == 0 {
		res.Err = errors.New("解析嵌入字体失败: collection 解析结果为空")
		return "", nil, res
	}
	id := embeddedID(name)
	res.ID = id
	res.Faces = len(faces)
	return id, faces, res
}

// supplement 在系统集合存在字形缺口时追加嵌入子集（C 卡 §4）。
//
// # 触发条件刻意收窄
//
// **只有**覆盖判定确实发现缺口时才加载嵌入。C 卡 §1.3 实测：本机嵌入子集
// 相对系统字体**零增量**（charset.txt 的 7,540 个码位全部被 msyh 覆盖），
// 因此无条件追加只会付出解析与内存代价、换不到任何一个字形。
// 覆盖完整时不加载嵌入——B4「系统命中则不解析嵌入字体」的约束**保持不变**。
//
// # 追加在末尾，不是开头
//
// Gio 按 collection 顺序做逐字形选择（C 卡 §1.2 实测：顺序即优先级），
// 因此「系统在前、嵌入在后」= 系统有字形就用系统，缺了才补嵌入。
// 反过来会让嵌入抢先，等于把系统字体降级为备选。
//
// # 补充失败不影响整体成功
//
// 系统已经可用，嵌入只是增益。加载失败时记录诊断并原样返回，
// **不**把整体解析打成失败——那会把"系统好好的"误报成 ErrFontNotFound。
func (p *Provider) supplement(faces []font.FontFace, rep *ProbeReport) []font.FontFace {
	cov := rep.Coverage
	if cov == nil || len(cov.Missing) == 0 {
		return faces
	}
	_, embFaces, embRes := p.loadEmbedded()
	rep.Embedded = embRes
	if embRes.Err != nil {
		return faces
	}
	cov.Supplemented = true
	return append(faces, embFaces...)
}

// checkCoverage 对命中集合做准入判定（C 卡 §3）。
func checkCoverage(faces []font.FontFace) *CoverageResult {
	req := RequiredRunes()
	hit, missing := Coverage(faces, req)
	return &CoverageResult{Required: len(req), Hit: hit, Missing: missing}
}

// embeddedID 生成嵌入集合的稳定标识。
//
// 与 system: 前缀区分开，诊断时一眼能看出"这一轮用的是兜底字体"。
func embeddedID(name string) string {
	if name == "" {
		name = "unknown"
	}
	return "embedded:" + name
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

// LastReport 返回最近一次探测的诊断报告的独立副本（未探测过时为 nil）。
func (p *Provider) LastReport() *ProbeReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	return copyProbeReport(p.last)
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
	// 唯一的报告构造点：三个返回点（零候选早退 / 命中成功 / 全部失败）
	// 全都返回本对象，因此 `ProbeRan` 必须在**这里**置真——
	// 放到循环之后会漏掉零候选早退那一条。
	rep := &ProbeReport{Family: FamilyCJK, ProbeRan: true}
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
		// C 卡 §3：命中不等于可用。解析成功之后还要问一句"它真的有汉字吗"。
		//
		// B 的标准到此为止（能解析 + 非空），于是只有拉丁字体的机器会
		// "系统命中成功、诊断无错、汉字全部 notdef"。覆盖判定把这个洞堵上。
		cov := checkCoverage(faces)
		if !CoverageSufficient(cov.Hit, cov.Required) {
			rep.Candidates = append(rep.Candidates, CandidateResult{
				Path: path, Status: StatusCoverageInsufficient, Faces: len(faces),
				Err: fmt.Errorf("CJK 覆盖不足: %d/%d（缺 %q）",
					cov.Hit, cov.Required, string(cov.Missing)),
			})
			continue
		}
		rep.Candidates = append(rep.Candidates, CandidateResult{
			Path: path, Status: StatusOK, Faces: len(faces),
		})
		rep.Coverage = cov
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

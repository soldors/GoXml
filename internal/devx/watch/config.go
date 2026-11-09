// Package watch 实现 TASK-DEVX-001 的 Watch worker 核心：文件监听快照、100ms 防抖状态机、
// 代次/取消守卫、资产与有效构建输入的覆盖，以及与用户 owned 生成物区分。
//
// 边界（本轮实现范围，写在最前以免夸大）：
//   - 仅**无窗口**核心；不启动用户 GUI、不执行用户 main/init。
//   - 生成复用实现内 `internal/cli` 的 `Run(generate --check)`（由调用方注入，不在此重造）。
//   - 构建复用 `go build`（由调用方注入命令构造器，测试可替换为受控 fake）。
//   - Hydration/JSON 状态保存恢复**不在**本轮；Watch 只负责进程边界热重启。
//   - 监听用**标准库轮询快照**（不新增 fsnotify 依赖）；成本与支持范围见 Config 文档与候选决策表。
//
// 该包不 import internal/cli 的未导出符号：on-disk Manifest 格式（.goxml/generate-manifest.json）
// 与产物保护头（第二行 `// Source: <base>`）按既有文档化格式**读取**（复用口径，非重造身份）。
package watch

// Snapshot 是一次对 ProjectDir 的确定性文件快照：Key=项目相对路径（/ 分隔，稳定排序），
// Value=内容 SHA-256（hex）。用于检测 .gxml、用户 Go、go.mod/go.sum、已引用资产、有效构建输入。
type Snapshot map[string]string

// FileClass 是一个被监听文件的所有权类别。
type FileClass int

const (
	// ClassUser 用户持有的源/输入：变化必须触发重建。
	ClassUser FileClass = iota
	// ClassOwned 本工具生成、且仍由本工具持有的产物（Manifest 登记 ∧ 保护头 Source 匹配）：
	// 其变化不得触发重建（否则自激循环）。
	ClassOwned
	// ClassIgnored 明确排除的路径（.git、缓存目录、构建输出目录等）。
	ClassIgnored
)

// Config 是 Watch 核心的可注入配置。零值取保守默认。
type Config struct {
	// ProjectDir 是监听的模块根（含 go.mod）。必填。
	ProjectDir string

	// DebounceMS 是防抖窗口（§8 明确 100ms）。<=0 时取 DefaultDebounceMS。
	DebounceMS int

	// PollIntervalMS 是轮询快照间隔（标准库轮询起步）。<=0 取 DefaultPollMS。
	// 注意：这是**候选决策**（见 candidates），默认值即本轮选定，不暗加公共 flag。
	PollIntervalMS int

	// BuildTimeoutMS 是单次 generate+build 的墙钟上限（超时即取消该代次，不重启旧进程）。
	BuildTimeoutMS int

	// ExcludeDirs 是额外排除的目录名（相对或绝对路径前缀），用于避免扫描自有构建输出/缓存。
	ExcludeDirs []string

	// Hydration 是**可选 opt-in** 的跨进程状态暂存/恢复钩子（R27-A 候选）。nil 时 Watch 行为与 R26
	// 完全一致（无额外 I/O、无额外线程、顺序/输出/失败保旧/进程清理不变）。非 nil 时在“新代停旧前”
	// 请求旧代快照并原子提交；快照失败/超时/取消 => 保持旧代不切换（保旧）。
	Hydration *HydrationHook
}

// 默认定量（写入候选决策表，作为本轮**选定值**而非隐藏 flag）。
const (
	DefaultDebounceMS = 100
	DefaultPollMS     = 50
	DefaultBuildMS    = 120000 // 2min，覆盖一次目录 generate --check + go build
)

func (c Config) debounce() int {
	if c.DebounceMS > 0 {
		return c.DebounceMS
	}
	return DefaultDebounceMS
}
func (c Config) poll() int {
	if c.PollIntervalMS > 0 {
		return c.PollIntervalMS
	}
	return DefaultPollMS
}
func (c Config) buildMS() int {
	if c.BuildTimeoutMS > 0 {
		return c.BuildTimeoutMS
	}
	return DefaultBuildMS
}

# 2026-11-04 R15-A BUILD-INPUT-CACHE-SAFETY（DeepSeek）dev-log

基线 `90f62dc0df611598b67dc6c7896f99fde00e0d0d`；树 `product-r15-deepseek`，分支 `codex/product-r15-deepseek-20261104`。
本线只改 `internal/cli` 缓存键/构建输入与直接回归 + 本线 dev-log；保持 `--check` 行为与公开 CLI。

## 1. 风险调查（真实 CLI，raw/R1-*.json）

初始假设：GOFLAGS 字符串不变、仅改 `-overlay` 的 JSON/替换源或 `-modfile` 的替代模块内容，缓存仍命中。
实测发现的**边界事实**（很重要，避免夸大）：

1. **Analyzer 不消费 GOFLAGS 里的 `-overlay`**：`goxml generate` 用 go/packages 加载 VM 包，
   而 `packages.Config.Overlay` 是编译前端自有 map，不读 GOFLAGS 的 overlay；夹具里
   overlay 提供的 VM 类型在生成期"type not found"（raw/R1-gap-overlay.json）。⇒ overlay
   内容变化**不影响生成产物**（`--check` 的 `go build` 才用它，且 `--check` 在命中时也执行，
   故 overlay 变坏会被提交前检查拦下；实测 raw/R1-gap-overlay-check.json）。
2. **`-modfile` 替换依赖模块**（影响 VM 字段）已被 **K8 依赖闭包指纹**覆盖：改 alt.mod
   指向 depB 后生成**正确 miss**并报 `GXML-BIND-002`（raw/R1-gap-modfile.json）。
3. 但**键身份**（K10）只记录 GOFLAGS **字符串**，不含其指向**文件内容**：一旦某输入
   （overlay/modfile）内容影响构建，字符串不变就无键差。用变异证明：去掉新增 K11 后，
   overlay JSON / 替换源 / modfile 内容变化全部**假命中**（见 §4 变异红）。

结论：**缓存身份缺少 GOFLAGS 引用文件的内容分量**是真实缺口（可被未来/受支持的输入形态触发），
按任务"可准确建模的输入进入缓存身份"补齐最稳。

## 2. 修复：K11 BuildInputs（内容进入缓存身份）

新增 `internal/cli/build_inputs.go`：`buildInputsFingerprint(goflags, moduleRoot)`。
- 解析 GOFLAGS 中 `-overlay=`/`-modfile=`（含独立值形式），对**引用文件内容**算 SHA-256 指纹；
- overlay：把 JSON 本身 + 每个替换目标文件内容 + 映射关系一并纳入；删除语义记映射；
- 读不到/JSON 损坏/替换源缺失 ⇒ 返回 ok=false，调用方标 `dependencyUnsafe` ⇒ **保守 miss**
  （不误命中；合法工程不受影响）。

`internal/cli/cache_wire.go` 的 `cacheKeyFor`：计算 BuildInputs 并写入键；ok=false 时
`r.dependencyUnsafe=true`（沿用既有保守 miss 通道）。

`internal/cli/cache_key.go`：
- `cacheKeyInput` 增 `BuildInputs`（K11），并 `writeKeyField(h,"buildInputs",…)`；
- **`cacheFormatVersion` 7→8**（键字段结构变化，单列演进依据见下），同步 3 处版本锚点测试。

### 缓存格式演进依据（v7→v8）
- 变更点：键新增 K11 分量（`buildInputs`）。
- 旧记录处理：v7 记录 `version` 字段不符 + 键不含 K11 分量 ⇒ 键不同/校验不过 ⇒ **自然安全 miss**，
  无需迁移。验收：raw/A2-e2e.json 的 `oldV7Record`（把记录 version 改 7 后二次不命中）。
- GXML 语言版本与产物保护头不变。

## 3. 修复后真实 CLI 验收（raw/A2-e2e.json，全 PASS）

| 场景 | 期望 | 实测 |
|---|---|---|
| 仅改 overlay JSON 指向（GOFLAGS 不变） | 安全 miss | `缓存未命中` |
| 仅改 overlay 替换源内容 | 安全 miss | `缓存未命中` |
| 仅改 `-modfile` 替代模块内容 | 安全 miss | `缓存未命中` |
| 普通工程（无 overlay/modfile） | 真命中，字节/mtime/Manifest 不变 | `缓存命中`，三者不变 |
| overlay 替换源缺失 | 保守 miss（不假命中） | 不命中（生成按既有语义失败） |
| 旧 v7 记录 | 安全 miss | 不命中 |

## 4. 判别回归（internal/cli/build_inputs_r15_test.go，5 项全 PASS）
`TestBuildInputsOverlayJSONChangeSafeMiss` / `…OverlaySourceChangeSafeMiss` /
`…ModfileChangeSafeMiss` / `TestBuildInputsPlainProjectStillTrueHit`（普通工程真命中，字节/mtime/Manifest 不变）/
`TestBuildInputsFingerprintUnit`（空引用为空且 ok；有 overlay 非空；替换源变化改变指纹；缺失/损坏 ok=false）。

**变异红**：把 `cacheKeyFor` 的 `BuildInputs` 置空（等价未接线）后，三个 safe-miss 测试立即失败，
真实表现即缺口本身：`跳过 ui/home_gen.go（缓存命中）`。恢复后全绿。

## 5. 失败语义/边界
- 检查失败/读取失败仍按既有路径；不改变 `--check` 行为与公开 CLI/退出码。
- 保守 miss 只影响"能否复用缓存"，不影响合法工程生成。
- 未改 `commit.go`/clean 事务/Compiler/诊断。

## 6. 门禁
`go test ./internal/cli -count=1 -cover` = **86.4%**（≥85%）；`go build ./...` ok；`go vet` rc0；`gofmt -l` 空。

## 7. 五态
实现 done / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。

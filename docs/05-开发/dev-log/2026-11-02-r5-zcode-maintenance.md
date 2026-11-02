# 2026-11-02 维护轮开发日志（ZCode：CLI 产物维护与 --clean 可靠性）

工作树 `night-zcode-maintenance-r2`，基线 70152d5（R5-B 最终），分支 `codex/night-zcode-maintenance-r2-20261102`。

## 切片 1+2（c02f07f + 35ec7ec）：所有权一致性 + 结构化失败记账

**所有权三判据**（cleanOrphansOps）：
1. 登记源已消失（既有）
2. 保护头 `// Source:` 与登记源基名一致（不一致 = 归属歧义 → 跳过）
3. 无其它源仍存在的登记声称同一输出（存活源争议 → 跳过）

**结构化 cleanFailure**：删除/保存失败不再丢弃已完成事实——携带 deleted/skipped/failed/pending 四清单，cause Unwrap 穿透，文案逐项诚实并含恢复提示。

**实测**：5 项判别回归（头不符/争议/正对照/句柄失败/保存失败）+ 全包 rc=0（21.7s）+ vet。

**过程教训**：
- `manifestPathOverride` 包级变量被前序测试污染 → `manifestPath` 返回错误路径 → cleanOrphansOps 读到空 Manifest（0 entries）。修复：测试显式重置。这是一个测试基础设施脆弱性的实例。
- Python heredoc 中 `\n` 在特定上下文被展开为真实换行导致 Go 字符串字面量被切断——改用 Edit 工具逐处修复。
- tempProjectInRepo 创建的目录在仓库 module 内，cleanOrphansOps 的 validateManifestOutputPaths 检测到路径"属于其他 Go module"——改用 t.TempDir() + 自带 go.mod/go.sum 的独立 module 模式。

## 切片 3：维护使用说明候选

`maintenance-usage-draft.md`（交付目录）：生成/缓存/清理三阶段、工具归属判据表、用户接管三条件、部分失败恢复步骤、缓存失效边界矩阵。

## 交叉审核

- DeepSeek A 线 R5（79048af 布局装饰）：detached 语义审读 + 锁空闲窗口实测其目标测试 rc=0。**技术通过、可组合**。报告 reviews/deepseek-a-decoration-review.md。
- 我的 maintenance-r2：待非作者审。

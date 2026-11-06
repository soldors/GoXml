# R25-A 最终独立复核与接纳补正（2026-11-06）

组合基线 `02f0385ad3781be9a50c6a2fa56f15a82f620692`（C 已发布），接纳作者
`86d096d8c7a8328b13ee94d615bfdc214c4abf10` 的 Image/Go overlay 一致性修复。
此前作者提交 `38c2657`、`531be90`、`7fee57d`、`97b2c02` 的输出校验与回归事实由
本篇和各自日志共同登记；R1/R2 首败与旧复核材料保留在外部交付目录。

## 独立结论

原 R1 两个阻断已在作者 R2 上独立转绿：用户 overlay 删除资产父目录时，默认生成
与 `--check` 都在写盘前报资产诊断；Src 引用本批输出时，不再用用户 PNG 掩盖最终
候选 Go 字节。原直接替换损坏 PNG 的反例、旧 10 用例和本轮新增用例也通过。
门禁与 K12 资产指纹共用有效字节/缺失判定；不修改 Codegen 模板、键结构或输出版本。

协调者另外发现 R2 将所有 Manifest 登记都加入 `AssetOutputPaths`，没有验证保护头
和 Source。独立夹具把普通 `assets/a.png` 误登记为旧输出，R1 允许生成，R2 错报
“generated/owned Go output”。本批最小补正：当前批次输出直接登记，历史 Manifest
输出必须通过与 `targetNameOverlay` 相同的保护头和 Source 证明后才取得工具优先级。
新增 `TestR25ManifestClaimDoesNotOwnAsset` 固定该反例，保留 owned 冲突拒绝用例。
补正不改变用户文件，不把不可信登记当所有权。新增读取成本为逐 Manifest 历史项
的一次所有权读取，不增加 Go 查询或额外全局缓存。

## 验证与范围

独立组合树：CLI 全包 `go test -count=1 -cover ./internal/cli` rc0，覆盖率 86.4%；
pipeline 全包 rc0，85.0%；原反例+所有 R25 窄测 rc0。跨包 preferences、taskboard、
component-layout 三工程回归 rc0（32 秒，限定到真实存在的测试入口）。
`go build ./...`、受影响包 vet 均 rc0；最终 docgate/diagdoc 和 gofmt 结果随接纳报告。
未运行全仓产品测试，不把它写成全仓通过。

作者冻结分支原样保留；修复在协调者独立组合树完成。主库 9 项文档 WIP 与已发布
C 的字节均保留，接纳不包含 Expanded、Router 预算、ThemeRef 或字体改动。
没有原生窗口、安装或真实性能采样；Linux/macOS、硬链接原始位置、TOCTOU 与
解码器隔离依旧未验证，不扩大 ADR-0015 的可信构建目录承诺。

外部证据根：`D:/Codes/Goxml-agent-rounds/2026-11-06/product-r25/coordinator/review-repair-r2/`，
其中 `narrow-cases/` 保存所有权误拒红证及 R1 对照，`a-final-gates/` 保存补正后全门禁；
`initial-controls/` 为作者 R2 上原反例独立绿证。进程记录保留真实 argv/cwd/rc/双通道，
每个 Go 批次持公共锁并按自有 token 释放。

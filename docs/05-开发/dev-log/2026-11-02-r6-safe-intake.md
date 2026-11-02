# R6 可直接接纳部分与候选阻断（2026-11-02）

首次回收先组合现行规则测试与事实文档；随后用户明确裁决接纳Background B（含Padding外框）。Input候选仍不启用。

## 身份

- 基线455da26；布局ready经独立补强为f7f49e576b7fb1aef3faccaba0028271b40c5102，原作者521a51b。
- Input ready经真实VM/精确诊断补正为669b4e6640f728e90ce9fde0755aa079c3d9ab73，原作者b04af77。
- 首次回收时Background A9930729/Bf34af98均未批准（其后用户明确选择B）；旧97a99c9从未发布，不能用它作已发布兼容性依据。
- Input候选1925d1d技术BLOCK：MaxLen1+AppendText在quiet帧4..8回调1→6，模型持续增长。独立反例d28e5eb含故意行为红，绝不并入ready。
- WorkBuddy仍在未冻结开发状态，自检46全绿不能当整线完成。草稿消费点存在closeErrors/活动数未知/未归属仍可ok，续接清单已交，待新冻结SHA再完整审。

## 已补正事实

双Input同字段不是Analyzer禁令：原fixture缺IdentityText导致EVENT-003。真实VM共享字段/独立字段对照可生成运行；单Input缺方法也报同一缺方法诊断。ToUpper幂等但标准化显示分歧继续是现行观察，不擅改公共回调语义。

原作者两次未skip的codegen全包包含无guard的app.RunWindow测试，且未持公共锁；历史OS窗口数未知，不能回填0。共享post-commit存在，hooksPath未设不等于没有hook；历史env缺失不能证明曾自动推送，也不能编造发生过推送。新独立复核均安全选择器、公共锁与空hooks。

## 本轮验证与流程

作者分离窄测与真实反例见night-product-r6/coordinator/review；本组合只运行新增ready矩阵与文档门禁，不复跑旧R5整包或原生窗口。生产源码与455da26逐blob相同，因此不把窄测coverage当新的全包覆盖率。主仓93份WIP须三方保留，暂存区仅审核blob。

协调者创建r6-safe-intake工作树时漏显式设置GOXML_AUTO_PUSH=0（已使用空hooksPath）；该动作仅worktree add，无提交/推送，后续Git写均显式固定环境。此记录是本方过程偏差，不以要求作者遵守而省略自身留痕。

## 后续用户裁决与 Background B 接纳

用户明确选择“B：包含Padding外框，背景连成完整色块”。采用f34af98叶子静态色实现及独立精确alpha/Input/兄弟状态回归，原A保留隔离。契约与语言规范同步覆盖范围和当前实现边界，不涉及Input语义批准。

组合时发现缓存桥接必要性：旧R5格式4记录可能来自忽略Background的生成器；生成器语言版本1不变，若沿用键会假命中旧产物。因此内部缓存升5，新增v4拒复用回归并做旧CLI→新CLI迁移真实流程。该新增生产路径使原测试-only组合结论不足，重新执行受影响Codegen/CLI完整包与vet/build/docgate，不复跑其它未变包/原生窗口。

组合提交记录：ce5a2d5为初次ready组合；16ae2ef接入用户裁定B与缓存5。首轮CLI仅TestFormatVersionEvolution因旧硬编码4失败，原始日志background-checks保留；按已批准的生成行为升级同步演进断言5，另有真实v4拒复用测试，不删除门禁。

## Background B 最终组合验证

受测源提交7468244ddaf1a363ae3c65f2e7228310be87ce70（前一阶段7468244同步缓存演进锚点）；CLI完整包覆盖87.8%、Codegen完整包覆盖88.5%，均count=1，Codegen明确排除原生窗口测试；vet/build/docgate/diagdoc通过。真实旧R5 CLI生成v4缓存及缺背景产物→新CLI不加no-cache重新生成paint.Fill背景与v5记录→新CLI再次真命中，生成物字节/mtime与Manifest不变。原始字节和实际argv见coordinator/background-checks-2与cache-migration，不把历史失败覆盖。

新接纳源码为静态叶子背景实现与内部缓存5；GXML Version=1、正式Input回调算法未变。候选1925d1d仍BLOCK，反例d28e5eb不合入。WorkBuddy尚无冻结ref，不触碰其进行中草稿；续接风险清单已保存。

本方收口偏差：候选内嵌Go测试有行尾空白，被首次diff检查拦下后修正；同步缓存常量5时漏改旧硬编码4锚点，首轮CLI真实失败已保留。一次PowerShell链式命令在diff指出日志末尾空行后仍提交，现将空行清理，最终diff检查必须为0。迁移驱动首轮误猜Manifest文件名，在旧/新CLI均rc0之后读取失败，已按源码generate-manifest.json纠正并使用新目录cache-migration-2重跑；不抹掉前序记录。

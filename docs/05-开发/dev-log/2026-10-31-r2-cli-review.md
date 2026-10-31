# R2 CLI 独立审核修复

作者冻结 aafaf0c，独立树 codex/r2-cli-review；不修改作者树或主仓。

Windows大小写别名绕过2fa91e8新增所有权门禁，两个源抢同一物理产物；随后删除旧源、--clean会以rc0删除当前源产物。同源路径大小写变化也会误拒。

门禁和Manifest转移共用os.SameFile身份比较，保留登记原拼写；不使用EqualFold以免混淆大小写敏感目录不同文件。源Stat只有IsNotExist允许转移，其它错误保留并终止。新增私有带stat参数函数仅用于确定性故障回归，无公共API、flag或依赖变化。

实测：冻结CLI四步反例、两个回归判别红/修后绿，go test ./internal/cli -count=1 rc0（56.630s），go vet ./internal/cli rc0。共享proclock原子锁持有并实测释放。完整命令/rc/raw见 D:/Codes/Goxml-agent-rounds/2026-10-31/product-batch-r2/coordinator/cli-review/。

未跑窗口、全仓、race、性能或跨平台。build-ignore包名误拒另片处理。

第二片：packageDirName全量收集包名把//go:build ignore生成脚本、异平台后缀和Go忽略文件当成生产包，误拒合法工程。对仅a_view.go + zz_generate.go/zz_platform的基线557346e原pipeline覆盖验证，两项均PASS；冻结slice2原pipeline覆盖同例两项FAIL。首次基线对照夹入_ignored.go导致旧实现先取hidden失败，原件保留，纠正后日志另存，未冒称首次通过。

最小修复复用标准库go/build.Default.MatchFile，筛选后仍按原有多包规则收集，匹配失败返回指名文件的错误；不自写构建tag解析，不新增公共API、依赖或flag。新增正常CLI、构建选择、非法约束回归，已有多个真实生产包拒绝对照保持通过。修后定向9项PASS，go test ./internal/cli -count=1 rc0（46.752s），go vet ./internal/cli rc0。

身份修复补充：缺失产物不等于没有所有权。既存叶子按SameFile；叶子缺失时比较最近既存父目录身份与剩余路径，识别目录别名；剩余路径仅大小写不同且无法判定时明确失败（EqualFold只发现歧义，绝不据此认定同一文件）。因此大小写敏感目录中不同已存在文件保持区分；大小写歧义的缺失路径有界拒绝并要求原拼写，未声称支持其无条件接管。

删除产物但保留旧源时，旧65ff849覆盖实现允许跨源接管（overlay红）；新实现拒绝（绿）。组合internal/cli整包rc0（42.039s）、vet rc0；额外缺失路径歧义/普通不同路径的反向对照单测rc0。仍未扩展窗口/全仓/性能范围。

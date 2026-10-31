# BUG-023：生成输出与Manifest路径可越出module

- 级别：P1，文件边界问题。
- 状态：真实越界反例已证；首轮修复的Windows junction遗漏由交叉审核发现，最小补丁及组合接纳见[本批记录](../06-测试/2026-10-31-并行开发验证记录.md)。

## 复现与根因

合法JSON Manifest可写入`../outside_gen.go`等路径，CLI `generate --clean --check`及no-cache变体曾rc0并实际删除module外带保护头文件。仅按字符串Join、生成保护头或合法JSON判断所有权，不足以保护项目边界。

首修加入词法与真实祖先检查后，普通symlink负例通过，但Windows junction叶子在本机Go1.26.1表现为ModeIrregular；EvalSymlinks并未按普通symlink解析。junction下不存在的输出子目录仍可rc0外写，独立审核实际重现，故首修没有直接合入。

## 修复与证据

全量Manifest路径前置校验，写出/清理前核对真实祖先与nested module；最小追加修复拒绝异常/reparse祖先并检查链接解析后的祖先，正常目录及module内合法目录symlink保留。真实`mklink /J`、缺失后代目录和外部文件字节回归修前rc1、修后rc0，完整CLI包通过。见[output_path.go](../../internal/cli/output_path.go)、[clean_safety_test.go](../../internal/cli/clean_safety_test.go)和[CLI日志](../05-开发/dev-log/2026-10-31-cli.md)。这是静态文件系统边界校验，不声称已解决恶意并发替换目录的TOCTOU模型。

## 协调者接纳

对应已审核修复已按范围接纳主仓：字体3357d8d、窗口d37cc00、编译器与CLI9e81534。组合源包、相关主仓回归及构建/静态检查通过；具体覆盖与未执行项见本批验证记录。

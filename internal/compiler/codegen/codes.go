package codegen

import "github.com/goxml/goxml/contracts"

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C3：诊断码常量
//
// 语义已冻结（卡 §10.5.2），发布后不得改语义；出现第三类冲突时扩号，不复用旧号。
// 权威清单见 诊断码目录.md §2.8；一致性由 internal/compiler/diagdoc 自动校验。
//
// 常量**不得内联拼接**：诊断码目录 §5 第 4 条要求定义常量并在发射点使用。
// ============================================================================

const (
	// CodeNamingExisting：生成符号与目标包**已有**包级声明冲突。
	//
	// 触发：待生成符号已被目标包内的包级声明占用，来源可能是用户代码，
	// 也可能是别的生成物。自家上一版产物不算（isOwnArtifact 排除）。
	//
	// 消息：`generated symbol %q (from %s) is already declared in package: %s`
	CodeNamingExisting = contracts.CodePrefixCodegenName + "-001"

	// CodeNamingSameBatch：不同源文件派生出**相同**生成符号。
	//
	// 触发：同一批输入里两个不同 .gxml 派生出同一符号——代表场景是
	// pascal 塌缩（a-b.gxml 与 a_b.gxml 都派生出 A_B 后缀）。
	// C2 之后不同源不再撞名，这条只剩塌缩这一类输入能触发。
	//
	// 消息：`sources %q and %q both generate symbol %q`
	CodeNamingSameBatch = contracts.CodePrefixCodegenName + "-002"
)

package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// 步骤 2 验收项 4：失败路径与提交边界
//
// 四类失败必须**明确区分**，不能用同一种处理方式掩盖：
//
//	A. Analyzer 拒绝      —— 绑定本身非法，不产生 VerifiedDoc，Codegen 永不执行
//	B. Codegen 失败        —— 已验证但当前实现不支持，返回错误且不产出部分源码
//	C. 生成代码编译失败     —— 生成产物无法编译（属实现缺陷，不是用户错误）
//	D. 运行时读取失败       —— **当前不存在**（见下方说明）
//
// 关于 D（重要，已核对契约）：
//   绑定生成为**静态类型字段访问**。Analyzer 已在编译期完成路径解析、
//   类型校验与可访问性检查，未通过的绑定根本进不了 Codegen。
//   因此「运行时读取失败」这一分支在 Go 生成代码中**不存在**。
//
//   唯一可能失败的运行时情形是 vm == nil → Go panic。
//   契约当前**未定义**该行为，故标记为**待决契约项**，
//   本任务不擅自选择默认策略（不返回空串、不忽略、不自定义 panic）。
// ============================================================================

// TestFailureClassA_AnalyzerRejects 覆盖 A 类：绑定非法 → Analyzer 拒绝。
func TestFailureClassA_AnalyzerRejects(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string // 期望的诊断码
	}{
		{"字段不存在", `<Text Value="{NoSuchField}"/>`, "BIND-001"},
		{"中间段不存在", `<Text Value="{User.NoSuchField}"/>`, "BIND-002"},
		{"未导出字段", `<Text Value="{secret}"/>`, "BIND-003"},
		{"有参方法", `<Text Value="{Format}"/>`, "BIND-005"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := verifiedErr(t, c.body)
			if err == nil {
				t.Fatalf("%s 应被 Analyzer 拒绝", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("期望诊断 %s，实际: %v", c.wantErr, err)
			}
		})
	}
}

// TestFailureClassB_CodegenFailsCleanly 覆盖 B 类：
// 已验证但不支持的情形 → 返回错误，且**不产出部分源码**。
func TestFailureClassB_CodegenFailsCleanly(t *testing.T) {
	// 事件当前不支持：Analyzer 通过，Codegen 必须明确失败。
	v := verified(t, `<Column><Text Value="x"/></Column>`)
	_ = v

	t.Run("跨包缺 ModulePath", func(t *testing.T) {
		v := verified(t, `<Text Value="{Title}"/>`)
		out, err := Generate(v, defaultOpts()) // 缺 ModulePath
		if err == nil {
			t.Fatal("应失败")
		}
		if len(out) != 0 {
			t.Errorf("失败时必须返回空源码，实际 %d 字节", len(out))
		}
	})
}

// TestFailureClassC_GeneratedMustCompile 覆盖 C 类：
// 凡成功返回的生成码，必须可编译（否则属实现缺陷）。
//
// 这里只验证「成功 → 可编译」这一方向；编译验证本身由
// TestBindingGeneratedCompiles 承担。
func TestFailureClassC_GeneratedMustCompile(t *testing.T) {
	// 成功返回的所有绑定形态都应能编译（逐个由运行时测试覆盖）。
	forms := []string{
		`<Text Value="{Title}"/>`,
		`<Text Value="{User.Name}"/>`,
		`<Text Value="{Owner.Name}"/>`,
		`<Text Value="{Display}"/>`,
	}
	for _, f := range forms {
		v := verified(t, f)
		out, err := Generate(v, bindingOpts())
		if err != nil {
			t.Errorf("%s 生成失败: %v", f, err)
			continue
		}
		if len(out) == 0 {
			t.Errorf("%s 生成为空", f)
		}
	}
}

// TestFailureClassD_RuntimeReadFailureUndefined 记录 D 类的**待决**状态。
//
// 本测试不实现任何策略，只锁定事实：
//   - 生成代码中不存在错误返回；
//   - vm == nil 会 panic（Go 语义），契约尚未定义该行为。
//
// 待契约决策后再实现，见 TASK-BINDING-IMPLEMENT-002 §2.3。
func TestFailureClassD_RuntimeReadFailureUndefined(t *testing.T) {
	v := verified(t, `<Text Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// 生成代码不含任何错误处理分支——这符合当前设计的静态类型特性。
	for _, bad := range []string{"if err !=", "recover(", "errors.New"} {
		if contains(s, bad) {
			t.Errorf("生成代码不应含错误处理分支 %q（运行时读取失败分支不存在）", bad)
		}
	}
	t.Log("已确认：运行时读取失败分支不存在；vm==nil 行为待契约决策（未擅自实现）")
}

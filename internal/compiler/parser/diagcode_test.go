package parser

import (
	"testing"
)

// ---------------------------------------------------------------------------
// 诊断码回归保护
//
// 这些测试锁定「代码实际发出的诊断码」，目的是防止文档与代码再次漂移：
// 历史上规范写 GXML-ATTR-002、代码实际发 GXML-ATTR-003，用户按文档搜索
// 找不到任何东西。任何码的改动都必须同时更新规范与诊断码目录。
// ---------------------------------------------------------------------------

// codeOf 返回首个诊断的码。
func codeOf(t *testing.T, src string) string {
	t.Helper()
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatalf("期望诊断，实际解析成功: %s", src)
	}
	return ds[0].Code
}

func TestDiagCodeBaselineOnColumn(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Column Align="Baseline"><Text Value="a"/></Column></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Baseline on Column = %s, 规范记载为 GXML-ATTR-003", got)
	}
}

func TestDiagCodeBaselineOnGrid(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Grid Columns="*" Align="Baseline"><Text Value="a"/></Grid></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Baseline on Grid = %s", got)
	}
}

func TestDiagCodeGridChildWidth(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Grid Columns="100 *"><Text Value="a" Width="50"/></Grid></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Grid child Width = %s", got)
	}
}

func TestDiagCodeGridNoChildren(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Grid Columns="100"></Grid></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Grid no children = %s", got)
	}
}

func TestDiagCodeGridNoColumns(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Grid><Text Value="a"/></Grid></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Grid no Columns = %s", got)
	}
}

func TestDiagCodeBadColumnWeight(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Grid Columns="0*"><Text Value="a"/></Grid></Page></Goxml>`)
	if got != "GXML-ATTR-003" {
		t.Errorf("Columns=0* = %s", got)
	}
}

func TestDiagCodeDuplicateAttribute(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><Text Value="a" Value="b"/></Page></Goxml>`)
	if got != "GXML-ATTR-001" {
		t.Errorf("重复属性 = %s", got)
	}
}

func TestDiagCodeWrongRoot(t *testing.T) {
	got := codeOf(t, `<Root><Page><Text Value="a"/></Page></Root>`)
	if got != "GXML-ROOT-001" {
		t.Errorf("错误根节点 = %s", got)
	}
}

func TestDiagCodeMissingKey(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page><For Each="i" In="{Items}"><Button Text="a"/></For></Page></Goxml>`)
	if got != "GXML-SCOPE-003" {
		t.Errorf("缺少 Key = %s", got)
	}
}

func TestDiagCodeTextNode(t *testing.T) {
	got := codeOf(t, `<Goxml Version="1"><Page>裸文本<Text Value="a"/></Page></Goxml>`)
	if got != "GXML-SYNTAX-004" {
		t.Errorf("文本节点 = %s", got)
	}
}

// ---------------------------------------------------------------------------
// 唯一性：同一个码不得被两个常量占用
// ---------------------------------------------------------------------------

func TestParserCodesAreUnique(t *testing.T) {
	all := map[string]string{
		"CodeSyntax":   CodeSyntax,
		"CodeXML":      CodeXML,
		"CodeRoot":     CodeRoot,
		"CodeAttrDup":  CodeAttrDup,
		"CodeAttrCall": CodeAttrCall,
		"CodeControl":  CodeControl,
		"CodeKey":      CodeKey,
		"CodeDef":      CodeDef,
		"CodeDefName":  CodeDefName,
		"CodeTextNode": CodeTextNode,
	}
	seen := make(map[string]string, len(all))
	for name, code := range all {
		if prev, dup := seen[code]; dup {
			t.Errorf("码 %s 同时被 %s 与 %s 占用", code, prev, name)
		}
		seen[code] = name
	}
}

// TestNoAttr002 记录 GXML-ATTR-002 目前未分配。
//
// 若将来启用该编号，必须同步更新规范与诊断码目录，并改掉这个测试。
func TestNoAttr002(t *testing.T) {
	for name, code := range map[string]string{
		"CodeAttrDup":  CodeAttrDup,
		"CodeAttrCall": CodeAttrCall,
	} {
		if code == "GXML-ATTR-002" {
			t.Errorf("%s 占用了未分配的 GXML-ATTR-002", name)
		}
	}
}

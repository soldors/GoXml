# 2026-11-05 R15-B FOR-IDENTIFIER-HYGIENE（ZCode）dev-log

基线 `90f62dc0df611598b67dc6c7896f99fde00e0d0d`；树 `C:/Users/admin/.codex/worktrees/product-r15-zcode/Goxml`，
分支 `codex/product-r15-zcode-20261105`。
改动范围：internal/compiler/codegen（checkLoopVarName 增一项 + 直接回归）+ 本线 dev-log。
未改 Parser/Expander/Analyzer、Input 规则、诊断号体系、CLI；**无既有成功产物的字节规则变化**
（合法输入与基线 CLI 逐字节相同，62d467d5…；唯一"变化"是原本必编译失败的输入改为明确拒绝），
generatorOutputRevision 无需提升（登记见 handoff，交协调者裁量）。

## 机制盘点（先审实际生成代码）

- **既有机制 1（R14）**：`reserveLoopNames` 把全部 For 的 Each/Index 纳入 import taken →
  导入别名让位循环变量（`Each="layout"` → `layout_2 "gioui.org/layout"`，实证通过）。
- **既有机制 2（契约 §6.1）**：`checkLoopVarName` 拒绝
  {v, th, gtx, items, children, rec, k, ops}，Each 与 Index 都检查。
- **行事件轮询作用域**：pollCode/cleanup 用固定 `rowItems`/`rowItem`/`rowIndex`，
  用户 Each/Index 名经行作用域根映射（`renderInvocationArgs` + rowScopeRoots），
  **不进入该作用域**——用户取名与轮询无冲突面。

## 反例（真实 CLI 复现，先立红）

`<For Each="append" In="{Rows}" Key="{append.ID}">` + 行按钮：**生成成功**（rc=0），
`go build` 失败（raw/r15b-build-cli 之后的手工对照，repro/for-append）：

```
home_gen.go:86:16: invalid operation: cannot call append (variable of struct type Row): Row is not a function
```

机理：For 内联展开在**同一作用域**生成 `children = append(children, sizeWrap(...))`
（<For> 列表契约 §2 的运行时组装形态），循环变量 `append` 遮蔽内置函数——
`append` 是循环体作用域内生成代码**实际调用**的标识符，checkLoopVarName 未覆盖。
Index="append" 同理（`for append, row := range items` 后体内同调用）。

## 三类输入的区分（任务书要求）

| 类 | 名字 | 行为 |
|---|---|---|
| 生成错误→本轮改为**明确拒绝** | `append`（Each/Index） | 基线：生成成功+build 失败；修复：codegen 诊断拒绝（与既有保留名同形态），磁盘零变更 |
| 既有保留名拒绝（保持兼容） | `v`/`th`/`gtx`/`items`/`children`/`rec`/`k`/`ops` | 诊断消息逐字不变（TestForExistingReservedNamesCompatible 锁定） |
| 合法输入 | `len`/`make`/`new`（该作用域内生成代码未使用）、相近名 `appendx`/`lenn` | 生成+构建 rc0；行为与基准一致 |

## 修复（c5d83aa）

`checkLoopVarName` 增保留 `"append"`，注释写明判据：**契约 §6.1 的
「与生成代码使用的标识符冲突」**，不是内置函数一刀切——`len`/`make`/`new`
在该作用域内未被生成代码调用，保持合法（避免把合法输入推去改名）。
无语言规则变化（不新增诊断号、不改 Parser 准入），无需裁决事项。

## 验收（真实 CLI + rtProbe 真实 Router；raw/repro 在交付目录）

| 项 | 结果 |
|---|---|
| Each/Index=append 拒绝 | codegen 诊断同形态；CLI rc=2 磁盘零变更（r15b-append-rejected-2） |
| 普通渲染 + If 分支 + 行事件实参 + Key 重排/删除 | `Each="len" Index="i"` + For 内 If 双分支：真实 Router 点击第 N 行收第 N 行 ID、重排 [C,A,B] 后命中 C、删除后命中 A、分支切换不串位（SCEN for-hygiene-eachlen） |
| 合法相近名对照 | len/make/new/appendx/lenn 生成 rc0（含引用点断言 `for _, len := range items` 等） |
| 保留名诊断兼容 | children/v/items/gtx 拒绝消息逐字不变 |
| 非冲突字节稳定 | 合法 Each 页面：基线 CLI（90f62dc）vs 修复 CLI sha256 相同 62d467d5… |
| **变异红** | 移除 append 保留（可编译变异）→ TestForBuiltinAppendRejected 红（"Each=append 应被拒绝"）；复现后还原 |

## 边界与未测

- 嵌套 <For> 不支持（既有 SCOPE 限制不变）；<For> 内 <Input> 仍由 SCOPE-005 拒绝。
- 保留名清单是**静态盘点**循环体发射点（children append / items / gtx / v / th /
  行轮询固定名）得出的；若未来 For 体生成新标识符，需同步扩清单——已在
  checkLoopVarName 注释与契约判据中锚定（判据本身是「实际使用」，清单是其投影）。
- Windows race / 原生 IME / 物理焦点：延续未测。

## 门禁

codegen 全包 `-skip '^TestButtonInRealWindowRuns$' -cover`：见 handoff（锁内、台账）。
gofmt 空、vet 干净、`go build ./...` ok。

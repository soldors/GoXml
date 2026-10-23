# PIT-012：`go build ./<空目录>/...` 打印 matched no packages 却 exit 0

- 分类：工具链 / Go 构建
- 关联：`docs/06-测试/`、涉及子进程编译的两处测试

## 现象

```bash
$ mkdir -p ./rel && go build ./rel/...
 go: warning: "./rel/..." matched no packages
$ echo $?
0
```

匹配不到包时，Go 打印一行 **warning**，但**退出码是 0**。
如果这段命令被放进"证明能编译"的测试里，测试就是**永久假绿**：它什么都没编译，却宣称通过。

同一类问题的另一个变体：`go vet` / `go build ./...` **跳过 `testdata/` 目录**，
所以真实窗口探针、根目录外的临时 module 必须**单独**跑一次，否则"全仓 vet 绿"不代表它们被检查过。

## 根因

Go 的 `./x/...` 通配展开不到任何包时，按约定不视为错误（与 `rm -rf x/**` 的设计取向类似），
只给 warning。而 shell 与多数测试框架只看退出码。

## 解决

测试里使用**不带通配的显式路径**，让它不依赖"是否匹配到包"：

```go
// 不要：runGoBuild(t, dir, "./rel/...")
// 要这样：
cmd := exec.Command("go", "build", "./rel")
```

路径不存在时 `go build` 会因找不到包而给出非 0 退出码，失败不会再被静默吞掉。

若必须检查"某个目录下应当有包"，就显式断言包的存在或用 `-o` 指定产物再校验产物存在。

## 预防

- 涉及子进程编译的测试，禁止使用 `...` 通配作为被测路径。
- **遗留已于 2026-10-23 修复**：`internal/compiler/codegen/binding_compile_test.go`、
  `binding_runtime_test.go` 原先是 `"./"+filepath.ToSlash(rel)+"/..."`，
  现改为 `"./"+filepath.ToSlash(rel)`（无通配），与 `naming_samepkg_test.go` 的既有写法对齐。
  在这之前它们"绿"不等于被测产物真的编译过。
- `testdata/` 下的探针要单独 build/vet，不能指望 `./...` 覆盖。

## 数据来源

- 可复现：新建空目录后 `go build ./<dir>/...; echo $?`（本仓库 Windows / go1.26.1 实测 exit 0）
- `docs/05-开发/dev-log/2026-10-23.md` §1.1（`go build ./...` 全绿的前提是路径都被真实枚举）

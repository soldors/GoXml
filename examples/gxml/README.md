# 从 GXML 到可构建的 Go 工程

`valid/`、`invalid/` 用于 Parser/Expander 的语法示例；它们引用的演示 ViewModel 不构成完整应用。`testdata/starter/` 则是完整 CLI 入门工程的源材料，包含真实 ViewModel、GXML 与窗口入口模板。

## 已有 GoXml checkout 中的用法

Windows 示例使用 PowerShell。先安装项目要求的 Go 1.26 工具链，进入仓库根目录；依赖需要已下载或允许首次正常获取。本项目 Windows 构建使用 `CGO_ENABLED=0`。

```powershell
$env:CGO_ENABLED = '0'
New-Item -ItemType Directory -Force ./bin | Out-Null
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp/starter | Out-Null
Copy-Item ./examples/gxml/testdata/starter/vm.go ./tmp/starter/vm.go
Copy-Item ./examples/gxml/testdata/starter/home.gxml ./tmp/starter/home.gxml
./bin/goxml.exe generate ./tmp/starter/home.gxml --no-cache
go test ./tmp/starter
```

到这里只生成和编译，不打开窗口。输出为 `tmp/starter/home_gen.go`；手写源码是 `vm.go`，不要编辑生成文件。生成器需要真实 Go package，因此先复制 ViewModel 再生成。

若要手工启动窗口：

```powershell
New-Item -ItemType Directory -Force ./tmp/starter/cmd/starter | Out-Null
Copy-Item ./examples/gxml/testdata/starter/main.go.txt ./tmp/starter/cmd/starter/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp/starter/cmd/starter/main.go -Raw
$main.Replace('example.com/goxml-starter', 'github.com/goxml/goxml/tmp/starter') |
    Set-Content -Encoding utf8 ./tmp/starter/cmd/starter/main.go
go build -o ./bin/starter.exe ./tmp/starter/cmd/starter
./bin/starter.exe
```

最后一行是明确的交互运行，会打开原生窗口。关闭窗口结束程序。夜间自动测试只编译入口，不执行它；窗口外观、真实点击和呈现效果仍需人工验证。

## 独立 module

创建 `module example.com/goxml-starter`，添加与当前 GoXml 相容的依赖；本地开发可在 `go.mod` 中使用 `replace github.com/goxml/goxml => "绝对 checkout 路径"`。将 `vm.go`、`home.gxml` 放在 module 根目录，用已构建的 `goxml` 生成后，复制 `main.go.txt` 至 `cmd/starter/main.go` 即可。测试使用相同布局，在独立临时 module 中复用仓库的锁定依赖与 `go.sum`，并关闭网络下载验证。

## 每一步在保证什么

- `.gxml` 经 Parser、宏展开、Analyzer，再由只接受 VerifiedDoc 的 Codegen 生成 Go。应用运行时导入 `pkg/runtime/app`，不导入 `internal/compiler`，不运行 XML 解释器。
- `Title` 是导出的绑定字段；`Increment` 是无返回值事件处理器。它在处理事件时更新显示字符串，避免在 Layout 中每帧格式化。生成视图接入 App 的失效入口后，事件处理会请求后续帧。
- 本例只有 UI 线程修改 ViewModel。后台任务需要调用方同步数据并请求失效；`Invalidate()` 的并发安全不等于字段读写安全。
- `--no-cache` 绕过增量缓存；普通重复生成也可使用缓存。两种方式都必须保留确定的源码输出。
- 当前 `--check` 不等同于 `go build` 或 `go test`，独立构建步骤不可省略。退出码 0 成功、1 用法错误、2 生成/提交失败。
- 不要把整个 `valid/` 目录当完整应用生成；其中是语法展示，不是各自具备 ViewModel 的包。

## 自动验证

```powershell
go test -v -run '^TestStarterProject$' ./examples/gxml
```

测试使用同一份源材料，调用真实 CLI，检查两次生成字节一致；把绑定改为不存在的字段后要求 `GXML-BIND-001`，并核对旧生成文件和 Manifest 不变。最后用独立 Go 子进程编译实际生成代码和窗口 main，执行 nil ViewModel 拒绝与普通模型状态更新检查。它不运行 main，不证明原生窗口显示或点击链路，也不生成性能数据。

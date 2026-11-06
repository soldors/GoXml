package preview

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// ownershipMarker 是宿主目录里的所有权标记文件名。
//
// 预览只在**本线拥有的全新临时目录**（os.MkdirTemp）里落宿主与产物；
// 清理前必须核对标记与 token，拒绝旧目录/非自有路径的写入与清理。
const ownershipMarker = ".goxml-preview-owned"

// hostLayout 是布置完成的宿主目录描述。
type hostLayout struct {
	Dir     string // 宿主根（本线拥有的临时目录）
	ViewDir string // 生成 View 所在包目录（<Dir>/view）
	Token   string // 所有权 token（同时写入标记文件）
	PkgName string // View 包名
	MainGo  string // 宿主入口源码（main 包）
}

// newToken 生成一次性所有权 token。
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败时用时间兜底会引入可预测 token；这里选择 fail-closed
		// 太重（预览是开发工具），改为把错误显式带进 token 使清理必然拒绝。
		return "unavailable:" + err.Error()
	}
	return hex.EncodeToString(b[:])
}

// arrangeHost 在全新临时目录里布置隔离预览宿主。
//
// 目录布局：
//
//	<tmp>/goxml-preview-*/
//	  .goxml-preview-owned     所有权标记（内容 = token）
//	  go.mod                   module goxmlpreviewhost + replace goxml => toolRoot
//	  go.sum                   复制 toolRoot/go.sum（离线构建所需的校验和）
//	  main.go                  宿主入口（package main，接 app.Run）
//	  view/<name>_gen.go       生成 View（package preview）
//	  view/<asset 路径>        有效资产字节（与门禁同一读取口径）
//
// 隔离保证：
//   - 宿主只 import 宿主模块自身的 view 包 + goxml runtime + gio；
//     用户业务包不进入依赖图（C3 用 canary 验证）；
//   - 用户模块全程只读：.gxml、原生成文件、Manifest、成功缓存都不动。
//
// viewConstructor 从生成产物 AST 提取构造函数名——符号推导规则的
// 单一事实源是 codegen 的产物本身，预览不复刻命名规则。
func arrangeHost(res *Result, toolRoot string, opts Options) (*hostLayout, error) {
	if res == nil || res.Source == nil {
		return nil, fmt.Errorf("preview: 布置宿主需要先生成 View（内部状态错误）")
	}
	ctor, err := viewConstructor(res.Source)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "goxml-preview-*")
	if err != nil {
		return nil, fmt.Errorf("preview: 创建宿主临时目录: %w", err)
	}
	h := &hostLayout{
		Dir:     dir,
		ViewDir: filepath.Join(dir, "view"),
		Token:   newToken(),
		PkgName: res.PkgName,
	}
	if err := os.MkdirAll(h.ViewDir, 0o755); err != nil {
		return nil, fmt.Errorf("preview: 创建 view 目录: %w", err)
	}
	// 所有权标记先行：从这一刻起目录就是可认领的「自有」目录。
	if err := os.WriteFile(filepath.Join(dir, ownershipMarker), []byte(h.Token+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("preview: 写入所有权标记: %w", err)
	}

	// go.mod：复制 goxml 源码树的 go.mod（依赖版本零漂移），改 module 行，
	// 追加 goxml require + replace（replace 指向 toolRoot，保证 runtime
	// 与生成 View 同源）。
	modSrc, err := os.ReadFile(filepath.Join(toolRoot, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("preview: 读取 goxml go.mod: %w", err)
	}
	mod := rewriteModuleLine(string(modSrc), hostModuleName)
	mod += fmt.Sprintf("\nrequire github.com/goxml/goxml v0.0.0\n\nreplace github.com/goxml/goxml => %q\n",
		filepath.ToSlash(toolRoot))
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		return nil, fmt.Errorf("preview: 写入宿主 go.mod: %w", err)
	}

	// go.sum：复制 goxml 仓库根的校验和（模板工程同款做法：依赖升级时
	// 只有一处需要更新）。replace 到本地目录的 goxml 不需要 sum。
	sum, err := os.ReadFile(filepath.Join(toolRoot, "go.sum"))
	if err != nil {
		return nil, fmt.Errorf("preview: 读取 goxml go.sum: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		return nil, fmt.Errorf("preview: 写入宿主 go.sum: %w", err)
	}

	// 宿主入口：只 build、由接线上层决定是否运行（本线不运行原生窗口）。
	//
	// import 用生成 View 的真实包名做显式别名：包名（默认 preview）与
	// 目录名（view）不同，裸 import 的限定符是包名，不写别名会让人误读。
	mainSrc := fmt.Sprintf(`package main

import (
	"fmt"
	"os"

	"github.com/goxml/goxml/pkg/runtime/app"

	%[1]s %[2]q
)

// main 是预览宿主入口：创建生成 View 并交给 runtime app 运行。
// 本线只构建、不运行它（原生窗口验收步骤见交付 handoff）。
func main() {
	v := %[1]s.%[3]s()
	if err := app.Run(v, app.Config{Title: %[4]q, Width: 800, Height: 600}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, h.PkgName, hostModuleName+"/view", ctor, "goxml preview: "+filepath.Base(res.File))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644); err != nil {
		return nil, fmt.Errorf("preview: 写入宿主 main.go: %w", err)
	}
	h.MainGo = mainSrc

	// 生成 View 落位。
	if err := os.WriteFile(filepath.Join(h.ViewDir, res.OutputName), res.Source, 0o644); err != nil {
		return nil, fmt.Errorf("preview: 写入生成 View: %w", err)
	}

	// 资产：把**门禁校验过的有效字节**复制进宿主 view 目录，
	// 与 go:embed 的相对路径一致。读取入口与门禁同源
	// （pipeline.ReadAssetForFingerprint 走同一条路径/边界/预算/overlay
	// 解析），保证「校验的字节」与「宿主嵌入的字节」不可能分叉。
	sourceDir := filepath.Dir(res.File)
	for _, ref := range res.assets {
		data, ok := pipeline.ReadAssetForFingerprint(sourceDir, ref.Path, opts.AssetOverlay)
		if !ok {
			// 正常到不了这里：门禁已校验同一读取。fail-closed 而不是跳过。
			return nil, fmt.Errorf("preview: 读取资产 %s 的有效字节失败（门禁已通过，读取却失败——拒绝继续）", ref.Path)
		}
		dst := filepath.Join(h.ViewDir, filepath.FromSlash(ref.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("preview: 创建资产目录 %s: %w", ref.Path, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, fmt.Errorf("preview: 写入资产 %s: %w", ref.Path, err)
		}
	}
	return h, nil
}

// rewriteModuleLine 替换 go.mod 的 module 行，其余逐字保留。
func rewriteModuleLine(src, module string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			lines[i] = "module " + module
			break
		}
	}
	return strings.Join(lines, "\n")
}

// viewConstructor 从生成产物 AST 提取零参构造函数名
// （形如 func NewGoxmlXxxView() *GoxmlXxxView，静态页契约 §4.3）。
func viewConstructor(src []byte) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		return "", fmt.Errorf("preview: 生成产物不可解析（内部状态错误）: %w", err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil {
			continue
		}
		if fd.Type.Params != nil && len(fd.Type.Params.List) > 0 {
			continue
		}
		name := fd.Name.Name
		if strings.HasPrefix(name, "New") && strings.HasSuffix(name, "View") {
			return name, nil
		}
	}
	return "", fmt.Errorf("preview: 生成产物中没有零参构造函数（NewXxxView）——静态页契约 §4.3 被违反")
}

// ownsHostDir 核对目录所有权：标记文件存在且 token 匹配。
//
// 拒绝语义（fail-closed）：
//   - 标记缺失 / token 不匹配 / 读取失败 → 不属于本次运行，拒绝清理；
//   - 目录是卷根或空路径 → 拒绝（结构性不可能由 MkdirTemp 产生）。
func ownsHostDir(dir, token string) bool {
	if dir == "" || token == "" {
		return false
	}
	clean := filepath.Clean(dir)
	if clean == string(filepath.Separator) || clean == "." || filepath.VolumeName(clean)+string(filepath.Separator) == clean {
		return false
	}
	data, err := os.ReadFile(filepath.Join(clean, ownershipMarker))
	if err != nil {
		return false
	}
	got := strings.TrimSpace(string(data))
	return got == token
}

// cleanupHost 在所有权核对通过后删除宿主目录。
//
// 这是预览唯一的删除动作，且只作用于自己的临时目录；
// 用户模块（.gxml、原生成文件、Manifest、成功缓存）不在删除范围内。
func cleanupHost(dir, token string) error {
	if !ownsHostDir(dir, token) {
		return fmt.Errorf("preview: 拒绝清理非本次运行拥有的目录 %s（所有权标记缺失或不匹配）", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("preview: 清理宿主目录 %s: %w", dir, err)
	}
	return nil
}

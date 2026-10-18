.PHONY: setup lint contract-lint test test-all test-race build-windows build-linux build-darwin \
        test-golden test-e2e test-generated benchmark bench-assert verify

# 平台策略（见 docs/03-架构/adr/ADR-0007-Zero-CGO降级为Windows侧约束.md）：
#   - Windows：CGO_ENABLED=0，纯 Go，单文件静态产物。这是唯一仍强制零 CGO 的平台。
#   - Linux  ：CGO_ENABLED=1，需 build-essential 与 X11/EGL/xkb 的 -dev 包。
#   - macOS  ：CGO_ENABLED=1，需在 macOS 上构建（Xcode CLT）。
#
#   `-race` 在 all 平台都要求 CGO_ENABLED=1（runtime/race 硬约束），
#   因此 test-race 与 build-windows 必须分开，不能合并。
#
#   ⚠️ 不再提供 "三端零 CGO 交叉编译" 目标——该目标已被真机实测否决。

setup:
	go mod tidy

contract-lint:
	CGO_ENABLED=0 go vet ./contracts/...
	CGO_ENABLED=0 go test -v ./contracts/...

lint: contract-lint
	golangci-lint run ./...

# 单元测试。
#   - 使用 `go test ./...` 而非显式列举目录，避免目录尚不存在时 go 报
#     "directory prefix does not exist" 而把门禁卡死。
#   - 当前前端（contracts / parser / expander / analyzer 等）不依赖 gio，
#     CGO_ENABLED=0 安全。
test:
	CGO_ENABLED=0 go test -v -cover ./...

# 包含 pkg/ 的完整测试，需要 C 工具链（Linux/macOS）或纯 Go 平台代码（Windows）。
test-all:
	CGO_ENABLED=1 go test -v -cover ./...

# 竞态检测：必须开 CGO。仅用于具备 C 工具链的开发机 / Linux CI。
test-race:
	CGO_ENABLED=1 go test -race ./...

# ---- 平台构建 ----

# Windows：零 CGO，单文件静态产物（已验证可行）
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build ./...

# Linux：必须开 CGO，需先安装系统 -dev 包
#   sudo apt-get install -y build-essential pkg-config \
#     libx11-dev libx11-xcb-dev libxkbcommon-dev libxkbcommon-x11-dev \
#     libxcursor-dev libxfixes-dev libegl1-mesa-dev libgles2-mesa-dev \
#     libvulkan-dev libwayland-dev
build-linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build ./...

# macOS：必须在 macOS 上执行（需 Xcode Command Line Tools）
build-darwin:
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build ./...
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build ./...

# ---- 待实现目标（显式失败，避免"看起来通过"） ----

test-golden:
	@echo "未实现：Golden 目标待 TASK-CODEGEN-001 提供 testdata 后补齐" && exit 1

test-e2e:
	@echo "未实现：E2E 目标待 Parser/Analyzer/Codegen 落地后补齐" && exit 1

test-generated:
	@echo "未实现：生成代码检查待 codegen 输出稳定后补齐" && exit 1

benchmark:
	@echo "未实现：基准套件待 TASK-BENCH-001 落地后补齐" && exit 1

# 性能不变量断言（INV-PERF-1/2/3/4）
bench-assert:
	@echo "未实现：性能断言待 benchmarks 目录落地后补齐" && exit 1

# 当前可执行的完整门禁。
# 注意：不含 bench-assert（尚未实现，接入后追加，否则 verify 会永久为红）。
verify: contract-lint test

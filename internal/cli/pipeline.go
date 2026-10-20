// Package cli 的管线桩：generate 的真实接线在任务卡 1-2 实现。
//
// 抽成包级变量便于测试注入，也让 1-1 的退出码行为可以独立验证
// （不依赖编译器跑通）。
package cli

import (
	"errors"
	"fmt"
	"io"
)

// pipelineStub 是 generate 的执行阶段。
//
// 1-2 将替换为真实实现（发现 → 管线 → Commit）；当前返回未实现错误，
// 使 `goxml generate <path>` 在 1-1 阶段得到确定的「运行失败」退出码，
// 而不是静默成功。
var pipelineStub = func(opts *generateOptions, stdout, stderr io.Writer) error {
	_ = opts
	fmt.Fprintln(stderr, "generate 管线尚未接线（任务卡 1-2）；本次调用未产生任何文件")
	return errors.New("not implemented")
}

// runGeneratePipeline 执行 generate 的运行阶段。
func runGeneratePipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if pipelineStub == nil {
		return errors.New("internal: generate pipeline is nil")
	}
	return pipelineStub(opts, stdout, stderr)
}

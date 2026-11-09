package watch

// R27-A：Watch 的 opt-in 跨进程状态钩子（候选）。默认 nil 时 Watch 行为与 R26 完全一致。
//
// 线程边界：
//   - Capture 在“旧代所有者”执行（由接线方提供：向运行中的旧子进程发出快照请求并取回其**不可变 DTO**；
//     本文件不直接读旧进程内存，worker 只拿到字节）。Capture 由调用方保证在其串行点捕获。
//   - 序列化 + 原子提交在本文件（worker）内，用 devxhydration.Protocol.Snapshot（内部有界等待捕获）。
//   - 恢复：新代启动时通过 RestoreEnv 向新子进程注入其应读取的状态文件路径/会话（实际“应用”在新代的
//     UI 所有者，本层不应用模型）；新代自身用 pkg/runtime/hydration 校验+应用。
//
// 快照失败/超时/取消 => BeforeSwap 返回错误 => Watch 保持旧代不切换（保旧）。新代恢复失败由新代自报，
// 本层不将其伪装为成功（见 e2e 中真实验证）。

import (
	"context"
	"fmt"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

// CaptureOld 向旧代请求一次**已确认**的 DTO 字节（不可变）。由接线方实现（console e2e 用状态请求文件 + 就绪信号）。
type CaptureOld func(ctx context.Context, oldGen int) (payload []byte, err error)

// HydrationHook 是 Watch 的 opt-in 状态钩子。
type HydrationHook struct {
	// Proto 是会话协议（Store/Schema/AppID/CaptureTimeout）。必填。
	Proto *devxh.Protocol
	// Capture 向旧代请求快照字节。必填。
	Capture CaptureOld
	// RestoreEnv 返回新代启动时应注入的环境变量（例如指向状态文件/会话），由 ProcessManager.start 生效。
	// 可选；nil 表示不额外注入（由外部既有机制处理）。
	RestoreEnv func(gen int) []string
}

// BeforeSwap 在新代构建成功、停旧之前调用：请求**实际在服务的旧代**（oldGen）快照并原子提交。
// F-A5：oldGen 为真实已发布/在服务的代次（由 Watch 传入 w.lastPublished），**不是 gen-1**；
// oldGen==0 表示尚无旧进程（冷启动），合法：不请求快照、不提交，直接允许发布。
// 任何快照失败返回错误（保旧不切换）。
func (h *HydrationHook) BeforeSwap(ctx context.Context, newGen, oldGen int) error {
	if h == nil {
		return nil
	}
	if oldGen == 0 {
		// 冷启动：无旧进程可捕获，合法跳过（不写状态、不切换语义变化）。
		return nil
	}
	if h.Proto == nil || h.Capture == nil {
		return fmt.Errorf("hydration hook 未配置完整（Proto/Capture 必填）")
	}
	_, err := h.Proto.Snapshot(ctx, func(c context.Context) ([]byte, error) {
		return h.Capture(c, oldGen)
	}, newGen)
	if err != nil {
		return err
	}
	return nil
}

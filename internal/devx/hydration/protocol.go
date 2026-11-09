package hydration

// 会话协议（capture → serialize/commit → confirm）与线程边界。
//
// 线程所有权（A1）：
//   - **UI 所有者线程**：执行 CaptureFunc（读取活跃 DTO 并返回其不可变 JSON 字节）。这是唯一读取“正在
//     变化的 VM”的地方；Layout 内**零文件 I/O、无等待**（capture 由调用方在串行点发起，非 Layout 内）。
//   - **worker（Watch worker）**：只处理**不可变**字节——序列化 Envelope、原子写文件、确认。worker 不
//     读取活跃 VM。
//   - **恢复**：新代在启动前/UI 线程应用；本包只提供校验 + 交给调用方在其所有者的应用钩子。
//
// 快照时点语义：CaptureFunc 在其被调用的串行点捕获；**不承诺**停止前所有后续编辑已保存，不宣称
// 无损持续同步/零停机。

import (
	"context"
	"errors"
	"fmt"
	"time"

	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

// CaptureFunc 在**所有者线程**执行：返回当前状态 DTO 的显式 JSON 字节。
// 若无法安全捕获（如所有者正忙/关闭）返回 error；调用方据此“快照失败保旧不切换”。
type CaptureFunc func(ctx context.Context) (payload []byte, err error)

// ApplyFunc 在**所有者线程**执行：用已校验的 DTO 字节恢复状态。必须整体成功或整体失败（不部分应用）。
// 返回 error 表示恢复拒绝——调用方不得静默记成功，也不得部分写入模型。
type ApplyFunc func(ctx context.Context, payload []byte) error

// Protocol 编排会话协议所需的静态上下文。
type Protocol struct {
	Store *Store
	// Schema/AppID 标识本会话期望的 DTO schema 与应用。
	Schema rst.Schema
	AppID  string
	// CaptureTimeout 是等待所有者捕获的上限（超时视为快照失败 => 保旧不切换）。
	CaptureTimeout time.Duration
	// Now 注入时钟（nil 用 time.Now）。
	Now func() time.Time
}

func (p *Protocol) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Protocol) captureTimeout() time.Duration {
	if p.CaptureTimeout > 0 {
		return p.CaptureTimeout
	}
	return 2 * time.Second
}

// Snapshot 请求一次**已确认**的状态快照并原子提交。返回提交所用的 Envelope（已落盘）。
//
// 语义：
//   - capture 必须成功且在 CaptureTimeout 内返回；失败/超时/取消 => 返回错误，**不提交**（上层据此保旧）。
//   - 只有捕获成功后才序列化 + 原子写；写失败返回错误（不留下半文件，见 Store.WriteAtomic）。
//   - gen 是本代次（写入 Envelope.Generation，用于恢复端过期判定）。
//   - 返回 Envelope 表示“已确认可恢复”的确认时点；不承诺之后无编辑。
func (p *Protocol) Snapshot(ctx context.Context, capture CaptureFunc, gen int) (*rst.Envelope, error) {
	if p.Store == nil {
		return nil, errors.New("hydration: Protocol.Store 必填")
	}
	if capture == nil {
		return nil, errors.New("hydration: capture 必填")
	}
	cctx, cancel := context.WithTimeout(ctx, p.captureTimeout())
	defer cancel()

	type capRes struct {
		payload []byte
		err     error
	}
	ch := make(chan capRes, 1)
	go func() { pl, err := capture(cctx); ch <- capRes{pl, err} }()

	var payload []byte
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("hydration: 捕获失败（保旧不切换）: %w", r.err)
		}
		payload = r.payload
	case <-cctx.Done():
		// 有界等待：超时/取消 不提交，等 capture goroutine 返回（防迟到写与之竞争）。
		r := <-ch
		_ = r
		return nil, fmt.Errorf("hydration: 捕获超时/取消（保旧不切换）: %w", cctx.Err())
	}

	env, err := rst.NewEnvelope(p.Schema, p.AppID, p.Store.SessionID(), gen, p.now(), payload)
	if err != nil {
		return nil, fmt.Errorf("hydration: 构造 Envelope 失败: %w", err)
	}
	raw, err := env.Encode()
	if err != nil {
		return nil, err
	}
	if err := p.Store.WriteAtomic(raw); err != nil {
		return nil, err
	}
	return env, nil
}

// Restore 读取并完整校验本会话状态，用调用方给定的 apply 在其所有者线程应用。
// 无状态文件 => (applied=false, ErrNoState) 供冷启动。校验失败 => 返回具体错误且**不调用 apply**（不部分应用）。
// apply 失败 => 返回错误（不静默成功）。
func (p *Protocol) Restore(ctx context.Context, minGen int, apply ApplyFunc) (applied bool, err error) {
	if p.Store == nil {
		return false, errors.New("hydration: Protocol.Store 必填")
	}
	exp := rst.Expect{Schema: p.Schema, AppID: p.AppID, SessionID: p.Store.SessionID(), MinGeneration: minGen}
	env, err := p.Store.ReadVerified(exp)
	if err != nil {
		if errors.Is(err, ErrNoState) {
			return false, ErrNoState
		}
		return false, err
	}
	if apply == nil {
		return false, errors.New("hydration: apply 必填")
	}
	if aerr := apply(ctx, env.Payload); aerr != nil {
		return false, fmt.Errorf("hydration: 恢复应用失败（不部分应用）: %w", aerr)
	}
	return true, nil
}

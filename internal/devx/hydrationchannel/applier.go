package hydrationchannel

// 应用器：在**新应用进程**内，用 devx/hydration.Protocol 读取并完整校验本会话状态，再经应用提供的
// OwnerRunner 在 owner 串行点执行应用的 ApplyFunc 应用 DTO。校验失败/取消/无状态 => 不调用 apply
// （不部分应用）；apply 整体成功由应用保证（库不代回滚任意用户回调）。

import (
	"context"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

// ApplyFunc 在 owner 串行点执行：用已校验的 DTO 字节恢复应用状态（全有或全无，由应用保证）。
type ApplyFunc func(ctx context.Context, payload []byte) error

// Applier 绑定一个会话的 Protocol 与 owner。
type Applier struct {
	Proto *devxh.Protocol
	Owner OwnerRunner
	Apply ApplyFunc
}

// NewApplier 构造应用器。
func NewApplier(proto *devxh.Protocol, owner OwnerRunner, apply ApplyFunc) (*Applier, error) {
	if proto == nil || owner == nil || apply == nil {
		return nil, ErrBadRequest
	}
	return &Applier{Proto: proto, Owner: owner, Apply: apply}, nil
}

// RestoreOnOwner 读取+校验+（在 owner）应用。返回 (applied, error)：
//   - ErrNoState => 冷启动（applied=false）。
//   - 校验失败/取消 => 具体错误且**不调用** apply。
//   - owner 关闭 => ErrOwnerClosed。
func (a *Applier) RestoreOnOwner(ctx context.Context, minGen int) (bool, error) {
	// 先由 Protocol 读取并完整校验（不应用）；校验在 worker 侧安全（只读文件）。
	// 应用阶段经 owner 串行点执行。
	type res struct {
		applied bool
		err     error
	}
	// Protocol.Restore 内部会在 apply 前复查取消；我们把 apply 包裹为 owner 投递。
	applied := false
	// 用一个哨兵判断是否进入 apply：仅当校验通过才会调用下面的 applyWrapper。
	applyWrapper := func(actx context.Context, payload []byte) error {
		return a.Owner.Do(actx, func(c context.Context) error {
			if cerr := c.Err(); cerr != nil {
				return cerr // 取消后排队：不应用
			}
			if err := a.Apply(c, payload); err != nil {
				return err
			}
			applied = true
			return nil
		})
	}
	ok, err := a.Proto.Restore(ctx, minGen, applyWrapper)
	return ok && applied, err
}

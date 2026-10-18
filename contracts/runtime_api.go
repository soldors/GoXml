package contracts

import (
	"context"
	"image/color"
)

// LifecycleMount 组件挂载生命周期接口
type LifecycleMount interface {
	OnMount(ctx context.Context)
}

// LifecycleUnmount 组件卸载生命周期接口
type LifecycleUnmount interface {
	OnUnmount()
}

// ThemeProvider 强类型主题接口
type ThemeProvider interface {
	GetColor(field string) color.NRGBA
}

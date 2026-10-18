package expander

// SchemaRegistry 是后续阶段（Analyzer）查询组件契约的唯一入口。
//
// 它提供的是**已分类的结果**，不是原始 `<Defs>` 文本：调用方不应重新解析
// 组件定义，否则会出现两套理解组件声明的逻辑。
//
// 注册表同时包含两类契约：
//
//   - 内置控件（`Text`/`Button`/`Input`/`Image`）——它们的契约在编译器内部；
//   - 本文档 `<Defs>` 声明的自定义组件。
//
// 布局标签（`<Row>`/`<Column>`/`<Grid>`/`<Page>`）返回 `(nil, false)`：
// 它们只有布局属性，而布局属性已由 Parser 校验并写入 `NodeIR` 的专用字段，
// 不需要通过组件 Schema 查询。
//
// 安全性：实现返回的 `*ComponentSchema` 是只读视图。调用方不得修改，
// 否则会影响同一编译上下文内的其他消费者。
type SchemaRegistry interface {
	// Lookup 按标签名查询组件契约。
	//
	// builtin 优先于自定义组件：内置控件名是保留名
	// （`<Def Name="Text">` 会被 GXML-DEF-002 拒绝），因此两者不会冲突。
	Lookup(tag string) (*ComponentSchema, bool)
}

// registry 是 SchemaRegistry 的实现。
//
// 它持有自定义组件契约的引用，内置契约按需查询 builtinSchema。
// 不复制任何数据结构：注册表与本次 Expand 的上下文共享同一份 schema。
type registry struct {
	custom map[string]*ComponentSchema
}

// Lookup 实现 SchemaRegistry。
func (r *registry) Lookup(tag string) (*ComponentSchema, bool) {
	if tag == "" || isLayoutTag(tag) {
		return nil, false
	}
	if s, ok := builtinSchema(tag); ok {
		return s, true
	}
	if r == nil || r.custom == nil {
		return nil, false
	}
	s, ok := r.custom[tag]
	return s, ok
}

// newRegistry 构造注册表视图。
func newRegistry(custom map[string]*ComponentSchema) SchemaRegistry {
	return &registry{custom: custom}
}

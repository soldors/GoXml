# R26 Widgets：协调者补齐重复 Open 的幂等守卫

日期：2026-11-07。作者冻结 `b72428bc0c70d5e17c1cb23ddb0115fcccfcafa5` 独立复核后，在协调者隔离分支修正；主库与作者树未改。

## 缺陷和修复

`Modal.Open()` 注释约定已打开时 no-op，但每次调用都会重置 `focusSt.requested`。内容持有焦点后再次调用 Open，下一帧会把焦点夺回遮罩。该行为由真实离屏 input.Router 探针复现，未启动原生窗口。

修复提交 `4313b2d9bcfffb2b36d62e3e3cc2e50ec7ee1359`：仅在已打开时提前返回，保留关闭后重新打开的焦点转换。新增 `TestModalRepeatedOpenKeepsContentFocus`，不放宽原有断言、不改变公开签名和候选策略。

## 验证和边界

Widgets 全套 33 个顶层用例通过、0 SKIP；MinGW race 与 vet 通过。以 Go overlay 恢复作者原 `modal.go`，同一新增守卫真实 FAIL（编译成功）；去 overlay 后 PASS。

独立证据位于 `D:/Codes/Goxml-agent-rounds/2026-11-06/product-r26/coordinator/review-repair-r1/`，其中 `b-boundary-probes` 为修复前反例，`b-fix-*` 为绿、变异红和恢复绿。两者时序分开保留。

本提交不代表组件默认策略已获批准或已发布。菜单/Modal 原生滚轮、显示器 DPI、IME 未验；DataGrid 未实现。R26-A Watch 仍有独立阻断项；CLI 接线与组合验收尚未进行。

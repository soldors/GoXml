# PIT-025：用 `append` 污染切片，**无法**证明深拷贝

## 现象

要验证「函数返回的是深拷贝」，常见写法是让调用方改坏返回值，再重新读取看原件是否被污染。
当被验证字段是 slice 时，如果污染动作写成：

```go
rep.Coverage.Missing = append(rep.Coverage.Missing, '中')
```

那么**即使实现是浅拷贝（共享底层数组），测试也照样通过**。

实测（2026-10-31，`pkg/runtime/theme`）：把 `copyProbeReport` 里
`c.Missing = append([]rune(nil), r.Coverage.Missing...)` 改回 `c.Missing = r.Coverage.Missing`（浅拷贝），
`TestProviderReportOwnership` 与 `TestProviderReportConcurrentLazyObservers` **全部 PASS，一次都没红**。
同一批变异里，`Candidates` 改浅拷贝能红（两个测试都红）、整个 `copyProbeReport` 不调用也能红。

## 根因

Go 的 `append` 在**容量不足时会分配新的底层数组**并把旧元素拷过去。此时：

1. 写入发生在**新数组**上，被共享的**旧数组**毫发无损；
2. 赋值回字段后，调用方看到的是新数组，原件看到的仍是旧数组；
3. 于是「共享」这件事被 `append` 自己**掩盖**了。

只有当 `cap > len`（有剩余容量）时 `append` 才会就地写入共享数组——而这**不可依赖**：
本例中 `Missing` 由 `var out []rune` + `append` 逐元素增长而来（`fallback.go:194` 的 `Coverage`），
容量取决于元素个数与 Go 的增长策略；测试路径下它恰好为空，`append` 必然重分配。

叠加的第二重问题：断言写成

```go
if again.Coverage != nil && (again.Coverage.Hit != wantHit || len(again.Coverage.Missing) != 0) {
```

`len(...) != 0` 只能区分「空 / 非空」。若原本就非空，它恒为真、直接误报；若原本为空，
它依赖 `append` 把长度从 0 变 1——而浅拷贝下 `append` 重分配后，原件长度仍是 0 ⇒ **判据失效**。

## 处理与预防

- **污染 slice 必须就地写**，不要用 `append`：

  ```go
  if len(rep.Coverage.Missing) > 0 {
      rep.Coverage.Missing[0] = 0xFFFD   // 必然命中共享底层数组
  }
  ```

- **断言具体元素值**，不要只比 `len`。
- **fixture 必须让该字段非空**，否则 `if rep.Coverage != nil` / `if len(...) > 0` 之类的守卫会让整段分支**根本不执行**，
  测试绿得毫无意义。
- 更根本的：**写完「防污染」测试后做一次变异**（把深拷贝改回浅拷贝），确认它**真的会红**——
  「测试存在」不等于「测试有判别力」。
- 相关：手工深拷贝（如 `copyProbeReport`）还应配**结构性守卫**（反射遍历引用字段断言地址不同），
  否则将来新增 slice/map 字段会静默漏拷、且现有测试全绿。

## 数据来源

2026-10-31 代码评审（`workbuddy/code-review/out/mutation-log.json`）：
M1（不拷贝）→ FAIL；M2（`Candidates` 浅拷贝）→ 两个测试都 FAIL；**M3（`Missing` 浅拷贝）→ 全 PASS**。
变异在隔离克隆内进行，结束后 SHA 与基线一致、克隆工作树干净。

关联：[PIT-023](踩坑-023-耦合判定必须靠场景实验.md)（同一原则：别靠目测，靠让它变红）。

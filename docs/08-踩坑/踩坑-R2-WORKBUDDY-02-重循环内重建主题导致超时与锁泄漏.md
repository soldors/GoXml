# 踩坑 R2-WORKBUDDY-02：扫描循环里重建 `material.NewTheme()` 导致超时，并连带泄漏自己的锁

- 临时编号：**R2-WORKBUDDY-02**（R2 批次临时 ID，**不占全局 PIT 编号**）
- 日期：2026-10-31
- 触发场景：离屏扫描生成视图的按钮命中区（约 5000 次合成点击）

## 现象

第一版几何探针把主题构建写进了每次点击：

```go
func clickOnce(...) string {
	th := material.NewTheme()   // ← 每次点击都重建
	...
}
```

结果整包超时被中断（`Signal: SIGTERM`），并且 `heavy.lock` **没有被释放**——因为持锁进程被杀，`finally` 没跑到。

## 两个独立问题

1. **性能**：`material.NewTheme()` 会构建字体与形状资源，单次开销远大于一次 Layout；5000 次 × NewTheme 使扫描从"秒级"变成"分钟级"。
   修正：包级 `var sharedTheme = material.NewTheme()`，扫描耗时从超时降到 0.691s（`out/geom-probe.json`）。
2. **锁泄漏**：持锁进程被外部中断时，释放逻辑不会执行。事后必须**核验 token 是自己的**再删除他人/自己的锁：

   ```python
   d = json.load(open(lock_path))
   assert d['agent'] == 'workbuddy' and d['task'] == 'product-r2-example'   # 只碰自己的
   os.remove(lock_path)
   ```

   本次泄漏的锁 token `c5bf4ff3483f4b9a8e22a5967a363d10`（agent `workbuddy`、runId `geom-02`）已按此方式释放，未触碰他人锁。

## 防御

- 重循环里只构建一次重量级对象（主题、字体、解码资源）。
- 持锁跑长任务前先估时；被中断后**第一件事是检查锁是否残留**，属自己 token 才删除，并在日志里记下 token。
- 记录锁的 `runId`：事后能凭它对上"哪次运行漏了释放"。

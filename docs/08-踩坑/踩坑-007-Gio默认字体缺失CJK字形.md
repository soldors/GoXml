# PIT-007：Gio 默认字体不含 CJK 字形，中文界面渲染为空白

- 分类：国际化 / 字体
- 关联：`docs/03-架构/字体与文本渲染策略.md`、`TASK-FONT-001`

## 现象

使用 Gio 自带的 `font/gofont/goregular` 解析字形时：

```text
U+0041 'A'  -> glyphIndex=36   （存在）
U+4E2D '中' -> glyphIndex=0    （缺失）
U+6587 '文' -> glyphIndex=0    （缺失）
U+754C '界' -> glyphIndex=0    （缺失）
U+9762 '面' -> glyphIndex=0    （缺失）
U+FF0C '，' -> glyphIndex=0    （缺失）
U+3002 '。' -> glyphIndex=0    （缺失）
```

`glyphIndex == 0` 表示字体不含该字形。界面不会报错，只会渲染出空白或豆腐块——**属于静默失败**。

## 根因

Gio 的 `gofont` 集合只覆盖 Latin / Greek / Cyrillic，不包含任何 CJK 字形。中文界面必须显式提供字体。

## 解决

采用「系统字体优先 + 嵌入兜底」双轨：

1. 按平台探测常见 CJK 字体（Windows `msyh.ttc`、macOS `PingFang.ttc`、Linux `NotoSansCJK`）。
2. 系统字体全部失败时启用嵌入子集（GB2312 常用字，≤3MB）。
3. 统一 fallback 链由 `pkg/runtime/theme` 提供，生成代码不感知。

## 预防

- 验收必须包含中文渲染用例，不得只测英文。
- 字体探测失败必须有诊断，不得静默回落。
- 中文 IME 必须实测（候选框定位、组合文本），不得假设可用。
- 该工作项此前在 WBS 中完全缺失，已补为 `TASK-FONT-001`。

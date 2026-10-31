# 踩坑 PORTABLE-WORKBUDDY-02："历史目录"用子串判据，会把交付目录自身算进去

- 日期：2026-11-01
- 位置：`scripts/test-verify-examples-portable.py` 的 L1/L4
- 触发场景：断言"锁助手的默认锁路径不落在历史目录"，用子串匹配 `Goxml-agent-rounds`

## 事实

判据写成：

```python
HISTORICAL_MARKERS = ('Goxml-agent-rounds', 'Goxml-night-reports', 'wcollector')
if any(m in str(js.get('path', '')) for m in HISTORICAL_MARKERS):
    fail('L1', '默认锁路径落在历史目录: %s' % js.get('path'))
```

而隔离布局当时建在**交付目录**下：`D:\Codes\Goxml-agent-rounds\2026-11-01\parallel-product\workbuddy\work\…\隔离 仓库 布局 01\Goxml`。于是默认锁路径（确实在布局内、完全正确）被判成"落在历史目录"。

## 为什么这条更危险

- 它是**自指**的：被测对象正确，判据却因为"取证现场"的位置而判红。
- 它和真实缺陷长得一样：真正的隐式依赖也会让路径里出现 `Goxml-agent-rounds`。只凭这条红无法区分"路径真的指向历史目录"和"你把实验台搭在了历史目录里"。
- 顺带说明**另一条**检查为什么也要收窄：`staticHits` 一开始扫了 4 个脚本，其中测试驱动（`test-verify-examples-hosts.py` / `test-verify-examples-portable.py`）**本来就该**显式引用夜间公共锁路径——那是显式配置，不是隐式依赖。把它们算进"不得出现历史路径"同样会假红。

## 修法

1. 隔离布局建到**系统临时目录**（`tempfile.mkdtemp`，路径里带空格与中文），与任何 `D:\Codes\...` 前缀无关；
2. 静态检查只针对**随仓交付的两个工具文件**（`verify-examples.ps1`、`verify-examples-lock.py`），并在报告里写明被跳过的驱动文件与跳过理由——范围写清楚，避免看起来像放水。

## 通用教训

1. **"是否落在历史目录"这种否定式判据，实验台本身不能建在历史目录里**，否则判据自指。
2. 判据的**适用范围**要写进证据：哪些文件受检、哪些豁免、豁免理由是什么。没有范围说明的"零命中"说服力为零。
3. 看到"路径里出现不该出现的目录"时，先问一句：**这个路径是产物还是实验台位置**。

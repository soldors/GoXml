# 踩坑 PORTABLE-WORKBUDDY-01：PowerShell 5.1 重定向输出是 GBK，不是 UTF-8

- 日期：2026-11-01
- 位置：`scripts/test-verify-examples-portable.py`
- 触发场景：用 Python `subprocess` 捕获 `verify-examples.ps1` 的 stdout，断言"输出里包含某个中文提示"

## 事实（实测）

Windows PowerShell 5.1 的 `Write-Host` 输出被重定向成**字节流**时，用的是**控制台代码页**（简体中文环境为 GBK / cp936），不是 UTF-8。取证（618 字节的 stdout 原始字节）：

```text
hex head: 5b7665726966792d6578616d706c65735d20 b2d6bfe2b8f9 3a20 …
utf-8  -> UnicodeDecodeError
gbk    -> '[verify-examples] 仓库根: D:\Codes\...'
mbcs   -> 同上
```

`[verify-examples]` 是 ASCII，所以"包含 `GXML-BIND-001`"这类 ASCII 断言一直是绿的——**只有中文断言会假红**，而且假红的原因看起来像"脚本没打印那句话"，很容易被误判成被测代码的问题。

## 后果

首轮可迁移性验证里 L5/L6/L7/L8 四个用例全红，报的是：

> `rc=2 但信息里没有"锁助手缺失"`、`信息里缺少 "另一次运行"`

而**退出码本身是对的**（2 / 2 / 5 / 5）。也就是说：红的是判据，不是被测代码。照着这个红去改脚本会把正确的实现改坏。

## 修法

逐级回退解码，并把实际用到的编码写进证据（不让"解码成功"变成隐藏前提）：

```python
def decode_console(raw):
    candidates = ['utf-8']
    candidates.append('mbcs' if os.name == 'nt' else locale.getpreferredencoding(False))
    candidates.append('latin-1')
    for enc in candidates:
        try:
            return raw.decode(enc), enc
        except (UnicodeDecodeError, LookupError):
            continue
    return raw.decode('utf-8', 'replace'), 'utf-8/replace'
```

命令流水里记 `stdoutEncoding`。

## 通用教训

1. **"信息里包含中文片段"这类断言，先确认字节是怎么编码的**。中文判据的假红成本远高于 ASCII 判据：它指向的是"功能没做"，而真相只是"解码错了"。
2. **退出码对、文本不对时，先怀疑解码，再怀疑被测代码**。本例 4 个用例退出码全对。
3. 脚本自己写出的文件（`verify-examples-run.log`）是 UTF-8 无 BOM，与控制台输出编码**不同**——同一份信息两种编码，取证时不要混用。

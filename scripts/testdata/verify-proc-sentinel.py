# -*- coding: utf-8 -*-
r"""R6-C 自有无 GUI 父→子→孙**哨兵**（只用于进程收口自检，不是产品代码）。

为什么用 Python：不需要新二进制、不依赖 shell、也不产生窗口；三辈进程都用
`sys.executable` 启动，父/子/孙的关系由脚本自己建立。

用法（argv 均为真实参数，用于参数回环验证）：

  sentinel.py quick                         立刻退出 0
  sentinel.py nonzero <code>                立刻退出 <code>
  sentinel.py binary                        向 stdout/stderr 写混合字节：CRLF、NUL、非法 UTF-8、中文
  sentinel.py echo-args                     把 sys.argv[2:] 以 JSON 打到 stdout（参数回环）
  sentinel.py echo-env <KEY>...             把指定环境变量以 JSON 打到 stdout（环境回环）
  sentinel.py slow-print <秒>               每 100ms 打一行，持续 <秒>（超时部分输出）
  sentinel.py tree <秒>                     父：spawn 子 → 子 spawn 孙，全部 sleep <秒>
  sentinel.py detach <秒>                   父：spawn 一个"孙"后**立刻退出 0**，孙继续活
  sentinel.py grand <秒>                    叶子：sleep <秒>

约定：`tree`/`detach` 产生的所有进程都在调用方创建的 Job 里（子进程继承 job），
因此它们既可以被整树收口，也不会跑到调用方进程树之外。
"""
import json
import os
import subprocess
import sys
import time


def _sleep(seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        time.sleep(min(0.05, max(0.0, deadline - time.monotonic())))


def _spawn(mode, seconds):
    return subprocess.Popen([sys.executable, os.path.abspath(__file__), mode, str(seconds)])


def main(argv):
    if not argv:
        print('usage: sentinel.py <mode> [args]', file=sys.stderr)
        return 2
    mode = argv[0]

    if mode == 'quick':
        return 0
    if mode == 'nonzero':
        return int(argv[1]) if len(argv) > 1 else 7
    if mode == 'binary':
        # 故意写"不是纯 UTF-8、带 CRLF 与 NUL"的字节：验证 raw 原样落盘。
        sys.stdout.buffer.write(b'line1\r\nline2\r\n')
        sys.stdout.buffer.write(b'\x81\x30 invalid-utf8  \xd6\xd0\xce\xc4\n')  # GBK 的"中文"
        sys.stdout.buffer.write(b'nul-after:\x00:end\n')
        sys.stdout.buffer.flush()
        sys.stderr.buffer.write(b'stderr-\xff\xfe-bytes\n')
        sys.stderr.buffer.flush()
        return 0
    if mode == 'echo-args':
        sys.stdout.write(json.dumps(argv[1:], ensure_ascii=False) + '\n')
        sys.stdout.flush()
        return 0
    if mode == 'echo-env':
        out = {k: os.environ.get(k) for k in argv[1:]}
        sys.stdout.write(json.dumps(out, ensure_ascii=False) + '\n')
        sys.stdout.flush()
        return 0
    if mode == 'slow-print':
        seconds = float(argv[1]) if len(argv) > 1 else 5.0
        steps = int(seconds / 0.1)
        for i in range(steps):
            print('tick %03d' % i, flush=True)
            time.sleep(0.1)
        return 0
    if mode == 'tree':
        seconds = float(argv[1]) if len(argv) > 1 else 30.0
        child = _spawn('child', seconds)
        try:
            child.wait(timeout=seconds + 30)
        except subprocess.TimeoutExpired:
            child.kill()
        return 0
    if mode == 'child':
        seconds = float(argv[1]) if len(argv) > 1 else 30.0
        grand = _spawn('grand', seconds)
        try:
            grand.wait(timeout=seconds + 30)
        except subprocess.TimeoutExpired:
            grand.kill()
        return 0
    if mode == 'detach':
        # 孙照常继承 job（父已在 job 内），父立刻退出 → 制造"父退孙活"。
        _spawn('grand', float(argv[1]) if len(argv) > 1 else 30.0)
        return 0
    if mode == 'grand':
        _sleep(float(argv[1]) if len(argv) > 1 else 30.0)
        return 0

    print('unknown mode: %s' % mode, file=sys.stderr)
    return 2


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))

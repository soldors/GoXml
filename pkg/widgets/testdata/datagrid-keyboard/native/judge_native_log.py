#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""R28-B DataGrid 原生键盘验收日志自动判定（R3 双判定版）。

用法: python judge_native_log.py <native-grid-fixture-log.jsonl>

两个独立判定：
  STRUCTURAL: PASS/FAIL — 日志合规（关窗/几何/退出/运行返回等不变式）
  KEYBOARD:   始终 MANUAL-REQUIRED — 日志不能自动判定键盘因果

不变式：
  R1 sel-change 目标必须是 ROW-\d{5} 或空串
  R2 focus==gridA 的 sel-change 必须 visible==true
  R3 focus 值域 {gridA, gridB, editor, none}
  R4 unconsumed-key 不得出现在 focus==gridA
  R5 edit-delta 必须 focus==editor
  R6 scroll offset ∈ [0, rows*rowHpx - bodyHpx]（按事件自带留痕几何判定）
  R7 destroy-callback → run-return → exit 各一次且顺序正确
  R8 resize 必须记录 pxPerDp
  R10 scroll/sel-change 必须自带几何留痕（bodyHpx/rowHpx/headerHpx）
  R11 run-return 必须携带 err 字段
  R12 run-return.err 非 <nil> → STRUCTURAL FAIL（fixture 运行失败）

退出码 0=STRUCTURAL-PASS，1=STRUCTURAL-FAIL。
KEYBOARD 始终 MANUAL-REQUIRED——日志不能自动判定键盘因果（native-acceptance-plan）。
"""
import json
from pathlib import Path
import re
import sys

SEL_RE = re.compile(r"^ROW-\d{5}$")
VALID_FOCUS = {"gridA", "gridB", "editor", "none"}


def main():
    if len(sys.argv) != 2:
        print("usage: judge_native_log.py <log.jsonl>")
        return 1
    try:
        raw = Path(sys.argv[1]).read_text(encoding="utf-8").strip()
        events = [json.loads(line) for line in raw.splitlines()]
    except (OSError, ValueError) as err:
        print(f"STRUCTURAL: FAIL — 日志不可读: {err}")
        return 1
    if not events or any(not isinstance(ev, dict) for ev in events):
        print("STRUCTURAL: FAIL — 空日志或日志项不是对象")
        return 1

    fails = []
    rows = 10000
    sel_changes = 0
    unconsumed = 0
    scrolls = 0
    geom_missing = 0
    run_err = None
    lifecycle = []
    last_seq = 0

    for ev in events:
        seq = ev.get("seq", 0)
        if type(seq) is not int or seq <= last_seq:
            print("STRUCTURAL: FAIL — 日志 seq 缺失或未严格递增")
            return 1
        last_seq = seq
        kind = ev.get("event")
        focus = ev.get("focus")
        if kind == "sel-change":
            sel_changes += 1
            to = ev.get("to", "")
            if to and not SEL_RE.match(to):
                fails.append(f"R1 seq={seq}: 非法 RowID 目标 {to!r}")
            if focus == "gridA" and to and not ev.get("visible"):
                fails.append(f"R2 seq={seq}: gridA 持焦选择当帧不可见 to={to}")
            if any(k not in ev for k in ("bodyHpx", "rowHpx", "headerHpx")):
                geom_missing += 1
                fails.append(f"R10 seq={seq}: sel-change 缺几何留痕")
        if kind == "focus-change":
            to = ev.get("to")
            if to not in VALID_FOCUS:
                fails.append(f"R3 seq={seq}: 非法焦点值 {to!r}")
        if kind == "unconsumed-key":
            unconsumed += 1
            if focus == "gridA":
                fails.append(f"R4 seq={seq}: gridA 持焦时按键泄漏 name={ev.get('name')}")
        if kind == "edit-delta":
            if focus != "editor":
                fails.append(f"R5 seq={seq}: 文本输入未归属 editor focus={focus}")
        if kind == "resize":
            if ev.get("pxPerDp") in (None, 0):
                fails.append(f"R8 seq={seq}: resize 未记录 pxPerDp")
        if kind == "scroll":
            scrolls += 1
            body_h = ev.get("bodyHpx")
            row_h = ev.get("rowHpx")
            if body_h is None or row_h is None or row_h <= 0 or body_h <= 0:
                geom_missing += 1
                fails.append(f"R10 seq={seq}: scroll 缺几何留痕")
            else:
                max_off = rows * row_h - body_h
                off = ev.get("offset", 0)
                if off < 0 or off > max_off:
                    fails.append(f"R6 seq={seq}: 滚动越界 offset={off} max={max_off}")
        if kind in ("destroy-callback", "run-return", "exit"):
            lifecycle.append(kind)
        if kind == "run-return":
            run_err = ev.get("err")
            if "err" not in ev:
                fails.append(f"R11 seq={seq}: run-return 缺 err 字段")
            elif run_err != "<nil>":
                fails.append(f"R12 seq={seq}: RunWindow 非 nil 错误={run_err!r}")

    if lifecycle != ["destroy-callback", "run-return", "exit"]:
        fails.append(f"R7: 关窗/返回/退出缺失、重复或错序: {lifecycle}")
    if events[-1].get("event") != "exit":
        fails.append("R7: exit 必须是最后一条日志")
    if sel_changes == 0:
        fails.append("R-judge: 无任何选择变化——验收操作未执行或日志缺失")

    for f_ in fails:
        print("FAIL:", f_)
    print(f"events={len(events)} sel-changes={sel_changes} scrolls={scrolls} "
          f"unconsumed={unconsumed} lifecycle={lifecycle} "
          f"geom-missing={geom_missing} run-err={run_err}")

    structural_pass = len(fails) == 0
    print(f"STRUCTURAL: {'PASS' if structural_pass else 'FAIL'}")
    print("KEYBOARD: MANUAL-REQUIRED (native-acceptance-plan A/B/C/D; "
          "log cannot verify keyboard causality)")
    if not structural_pass:
        print("VERDICT: STRUCTURAL-FAIL")
        return 1
    print("VERDICT: STRUCTURAL-PASS (keyboard: manual required)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

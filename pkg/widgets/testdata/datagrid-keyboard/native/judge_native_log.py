#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""R28-B DataGrid 原生键盘验收日志自动判定。

用法: python judge_native_log.py <native-grid-fixture-log.jsonl>

依不变式自动判定（人工观察项见 native-acceptance 计划，不在本脚本范围）:
  R1 sel-change 目标必须是 ROW-\\d{5} 或空串
  R2 focus==gridA 的 sel-change 必须 visible==true（键盘/鼠标选择当帧可见）
  R3 focus 值域 {gridA, gridB, editor, none}
  R4 unconsumed-key 不得出现在 focus==gridA（持焦网格的按键必须被消费）
  R5 edit-delta 必须 focus==editor（文本输入归属焦点消费者）
  R6 scroll offset ∈ [0, rows*24 - bodyH]（按最近 resize 推导）
  R7 exit 事件存在且在 close 之后
  R8 resize 事件必须记录 pxPerDp（DPI 观察依据）

退出码 0=PASS，1=FAIL。
"""
import json
import re
import sys

SEL_RE = re.compile(r"^ROW-\d{5}$")
VALID_FOCUS = {"gridA", "gridB", "editor", "none"}
ROW_H = 24
HEADER_H = 24


def main():
    if len(sys.argv) != 2:
        print("usage: judge_native_log.py <log.jsonl>")
        return 1
    events = []
    with open(sys.argv[1], encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                events.append(json.loads(line))
    fails = []
    last_viewport_h = None
    rows = 10000
    close_seq = None
    exit_seq = None
    sel_changes = 0
    unconsumed = 0

    for ev in events:
        seq = ev.get("seq", 0)
        kind = ev.get("event")
        focus = ev.get("focus")
        if kind == "sel-change":
            sel_changes += 1
            to = ev.get("to", "")
            if to and not SEL_RE.match(to):
                fails.append(f"R1 seq={seq}: 非法 RowID 目标 {to!r}")
            if focus == "gridA" and to and not ev.get("visible"):
                fails.append(f"R2 seq={seq}: gridA 持焦选择当帧不可见 to={to}")
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
            last_viewport_h = ev.get("newH")
            if ev.get("pxPerDp") in (None, 0):
                fails.append(f"R8 seq={seq}: resize 未记录 pxPerDp")
        if kind == "scroll":
            if last_viewport_h:
                body_h = last_viewport_h - HEADER_H
                max_off = rows * ROW_H - body_h
                off = ev.get("offset", 0)
                if off < 0 or off > max_off:
                    fails.append(f"R6 seq={seq}: 滚动越界 offset={off} max={max_off}")
        if kind == "close":
            close_seq = seq
        if kind == "exit":
            exit_seq = seq

    if exit_seq is None:
        fails.append("R7: 缺少 exit 事件")
    elif close_seq is not None and exit_seq < close_seq:
        fails.append("R7: exit 先于 close")
    if sel_changes == 0:
        fails.append("R-judge: 无任何选择变化——验收操作未执行或日志缺失")
    if unconsumed == 0:
        print("note: 无 unconsumed-key 事件（未做无焦点按键泄漏观察，可接受）")

    for f_ in fails:
        print("FAIL:", f_)
    print(f"events={len(events)} sel-changes={sel_changes} unconsumed={unconsumed} "
          f"close={close_seq} exit={exit_seq}")
    if fails:
        print("VERDICT: FAIL")
        return 1
    print("VERDICT: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())

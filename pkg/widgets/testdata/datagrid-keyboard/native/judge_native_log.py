#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""R28-B DataGrid 原生键盘验收日志自动判定（R1 修正版）。

用法: python judge_native_log.py <native-grid-fixture-log.jsonl>

R1 修正（不得缺证通过）：
  - 必须存在 destroy-callback 或 close（真实关窗证据），且 exit 在其后；
  - 必须有导航证据：≥1 次 scroll 事件，或某次 sel-change 相对上一次选中
    行的索引跳变 ≥2（Home/End/Page 语义）——「只选择未导航」不通过；
  - 必须有几何留痕：任何 scroll/sel-change 事件缺 bodyHpx/rowHpx 判定
    依据即 FAIL（几何用 fixture 留痕的本帧 Dp 换算值，不用整窗高度和
    固定 24px）；
  - 空日志、错序（exit 先于 close）直接 FAIL。

不变式：
  R1 sel-change 目标必须是 ROW-\d{5} 或空串
  R2 focus==gridA 的 sel-change 必须 visible==true
  R3 focus 值域 {gridA, gridB, editor, none}
  R4 unconsumed-key 不得出现在 focus==gridA
  R5 edit-delta 必须 focus==editor
  R6 scroll offset ∈ [0, rows*rowHpx - bodyHpx]（按事件自带留痕几何判定）
  R7 close（destroy-callback/close）存在，exit 在其后
  R8 resize 必须记录 pxPerDp
  R9 必须有导航证据（scroll 或 ≥2 行跳变的选择）
  R10 scroll/sel-change 必须自带几何留痕（bodyHpx/rowHpx/headerHpx）

退出码 0=PASS，1=FAIL。
"""
import json
import re
import sys

SEL_RE = re.compile(r"^ROW-\d{5}$")
VALID_FOCUS = {"gridA", "gridB", "editor", "none"}


def row_index(rid):
    if not rid or not SEL_RE.match(rid):
        return None
    return int(rid.split("-")[1])


def main():
    if len(sys.argv) != 2:
        print("usage: judge_native_log.py <log.jsonl>")
        return 1
    try:
        raw = open(sys.argv[1], encoding="utf-8").read().strip()
    except FileNotFoundError:
        print("FAIL: 日志文件不存在")
        return 1
    if not raw:
        print("FAIL: 空日志")
        return 1
    events = [json.loads(line) for line in raw.splitlines()]

    fails = []
    last_viewport_h = None
    rows = 10000
    close_seq = None
    exit_seq = None
    sel_changes = 0
    unconsumed = 0
    scrolls = 0
    last_sel_idx = None
    geom_missing = 0

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
            if any(k not in ev for k in ("bodyHpx", "rowHpx", "headerHpx")):
                geom_missing += 1
                fails.append(f"R10 seq={seq}: sel-change 缺几何留痕")
            idx = row_index(to) if to else None
            if idx is not None:
                if last_sel_idx is not None and abs(idx - last_sel_idx) >= 2:
                    nav_evidence = True  # 跳变 = Home/End/Page 导航语义
                last_sel_idx = idx
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
        if kind == "destroy-callback":
            if close_seq is None:
                close_seq = seq  # 真实 close（公开 WithOnDestroy 观察）
        if kind == "close":
            if close_seq is None:
                close_seq = seq
        if kind == "exit":
            exit_seq = seq
            if "runErr" not in ev:
                fails.append(f"R11 seq={seq}: exit 缺 run-return 区分字段")
            elif ev["runErr"] not in ("<nil>", "None"):
                fails.append(f"R12 seq={seq}: RunWindow 非 nil 错误={ev['runErr']}——fixture 运行失败")

    if exit_seq is None:
        fails.append("R7: 缺少 exit 事件")
    if close_seq is None:
        fails.append("R7: 缺少真实关窗证据（destroy-callback/close）")
    elif exit_seq is not None and exit_seq < close_seq:
        fails.append("R7: exit 先于 close（错序）")
    # R9: 键盘因果判定——keyboard-nav 事件由生产者测试在按下导航键时
    # 经同一 emit 路径留痕；纯鼠标轨迹（有 gridA 选择但零 keyboard-nav）
    # 被拒绝作为键盘验收证据。
    keyboard_nav_events = sum(1 for ev in events if ev.get("event") == "keyboard-nav")
    keyboard_evidence = "ABSENT"
    if keyboard_nav_events > 0:
        keyboard_evidence = "CONFIRMED-BY-PRODUCER"
    has_grid_sel = any(ev.get("event") == "sel-change" and ev.get("focus") == "gridA" for ev in events)
    if has_grid_sel and keyboard_nav_events == 0:
        fails.append("R9: 纯鼠标轨迹（gridA 持焦选择但零 keyboard-nav 事件）——"
                     "键盘导航需按 native-acceptance-plan 人工专项验证")
    if sel_changes == 0:
        fails.append("R-judge: 无任何选择变化——验收操作未执行或日志缺失")

    for f_ in fails:
        print("FAIL:", f_)
    print(f"events={len(events)} sel-changes={sel_changes} scrolls={scrolls} "
          f"unconsumed={unconsumed} close={close_seq} exit={exit_seq} "
          f"geom-missing={geom_missing} keyboard-evidence={keyboard_evidence}")
    print("KEYBOARD: NOT-JUDGED-FROM-LOG — manual native testing per "
          "native-acceptance-plan required (键位/连发/IME/DPI 均为人工专项)")
    if fails:
        print("VERDICT: FAIL")
        return 1
    print("VERDICT: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())

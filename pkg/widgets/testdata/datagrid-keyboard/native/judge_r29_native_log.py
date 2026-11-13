#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""R29-B R1 DataGrid 交互扩展（N1–N6）原生验收日志结构判定器。

用法: python judge_r29_native_log.py <r29-fixture-log.jsonl>

与 R28 judge_native_log.py 同构的双判定：
  STRUCTURAL: PASS/FAIL — 模型化场景时间线与不变式判定
  SCENARIO:   始终 MANUAL-REQUIRED — 日志不能自动判定「人按场景做了」
              的因果（原生阶段由操作者确认）

STRUCTURAL 拒绝（任务要求）：
  错列       R14 click.col 必须是该帧 offX/viewWpx/colWpx 几何下可见列
  错轴       R13 axis 每事件仅单轴变化；N1 须同时出现 Y+ 与 X+；
             域钳制 0≤off≤max
  无排序     R15 sort 恰一次；after-head != before-head；头尾样本多重集
             相等；after-head 按 RowID 严格降序（反序脚本）
  错误选择集 R16 每个 sel-multi 必须与选择模型逐步一致（plain 收拢/
             Ctrl 切换/Shift 锚点范围；点击 RowID 与 lead 都按模型在
             当序下推导）——任一点不符即 FAIL
  早退       R7 destroy-callback → run-return → exit 顺序且 exit 为
             最后事件；R12 run-return.err 非 "<nil>" 即 FAIL

数据模型（与 RUNBOOK-R29 脚本一一对应；ROW-%05d，n=10000）：
  排序前 order[i]=ROW-i；P 反序后 order[i]=ROW-(9999-i)。
  Home（初始无选择）选中第 0 行并成为锚点（R28 语义）。
  lead=键盘锚点：plain/Ctrl 点击移动锚点（可在集合外——Ctrl 切换撤销
  后锚点行已被取消选择），Shift 点击锚点不动。
"""
import json
from pathlib import Path
import re
import sys

SEL_RE = re.compile(r"^ROW-\d{5}$")
N_ROWS = 10000
NCOLS = 5


def order_id(i, sorted_desc):
    return "ROW-%05d" % ((N_ROWS - 1 - i) if sorted_desc else i)


def model_click(set_a, anchor_a, lead_a, idx, action):
    """按选择模型处理一次 gridA 点击，返回 (新集合, 锚点, lead)。"""
    new_set = list(set_a)
    if action == "plain":
        new_set = [idx]
        anchor_a = idx
        lead_a = idx
    elif action == "ctrl":
        if idx in new_set:
            new_set.remove(idx)
        else:
            new_set.append(idx)
        new_set.sort()
        anchor_a = idx
        lead_a = idx
    elif action == "shift":
        if anchor_a is None:
            new_set = [idx]
            anchor_a = idx
            lead_a = idx
        else:
            lo, hi = sorted((anchor_a, idx))
            new_set = list(range(lo, hi + 1))
            # 锚点（selAnchor，Shift 扩展起点）不动；lead（selID，键盘
            # 锚点）移动到被点行——实现语义，两值在此分叉。
            lead_a = idx
    return new_set, anchor_a, lead_a


def main():
    if len(sys.argv) != 2:
        print("usage: python judge_r29_native_log.py <log.jsonl>")
        return 1
    try:
        raw = Path(sys.argv[1]).read_text(encoding="utf-8").strip()
        events = [json.loads(line) for line in raw.splitlines()]
    except (OSError, ValueError) as err:
        print("STRUCTURAL: FAIL — 日志不可读: %s" % err)
        return 1
    if not events or any(not isinstance(ev, dict) for ev in events):
        print("STRUCTURAL: FAIL — 空日志或日志项不是对象")
        return 1

    fails = []
    sorted_desc = False
    set_a, anchor_a, lead_a = [], None, None  # gridA 选择模型（行索引）
    steps_b = [(0, "plain")]                  # gridB 对照（恒等序）
    seen_sort = 0
    axis_y_plus = axis_x_plus = 0
    focus_editor_before_edit = False
    edit_chars = 0
    lifecycle = []
    last_seq = 0
    step_idx = 0
    home_pending = True
    clicked_a = 0

    # gridA 点击步骤（与 RUNBOOK-R29 脚本一一对应）：(rowIdx, action)。
    steps_a = [
        (1, "plain"),  # N2 滚后命中
        (0, "plain"),  # N3 排序后首行
        (2, "plain"),  # N4 归位
        (4, "ctrl"),
        (2, "ctrl"),
        (1, "plain"),  # N5 锚点
        (3, "shift"),
        (0, "shift"),
        (2, "plain"),
        (5, "plain"),  # N6 收尾
    ]

    for pos, ev in enumerate(events):
        seq = ev.get("seq", 0)
        if type(seq) is not int or seq <= last_seq:
            print("STRUCTURAL: FAIL — 日志 seq 缺失或未严格递增")
            return 1
        last_seq = seq
        kind = ev.get("event")
        focus = ev.get("focus")

        if kind == "destroy-callback":
            lifecycle.append("destroy-callback")
        elif kind == "run-return":
            lifecycle.append("run-return")
            if ev.get("err") != "<nil>":
                fails.append("R12 seq=%d: run-return.err=%r（fixture 运行失败）" % (seq, ev.get("err")))
        elif kind == "exit":
            lifecycle.append("exit")

        elif kind == "axis":
            offX, offY = ev.get("offX"), ev.get("offY")
            offXMax, offYMax = ev.get("offXMax"), ev.get("offYMax")
            prevX, prevY = ev.get("prevX"), ev.get("prevY")
            if not all(isinstance(v, int) for v in (offX, offY, offXMax, offYMax, prevX, prevY)):
                fails.append("R13 seq=%d: axis 缺偏移整数域" % seq)
                continue
            if offX < 0 or offX > offXMax or offY < 0 or offY > offYMax:
                fails.append("R13 seq=%d: 偏移越界 off=(%d,%d) max=(%d,%d)" % (seq, offX, offY, offXMax, offYMax))
            dx, dy = offX - prevX, offY - prevY
            if dx != 0 and dy != 0:
                fails.append("R13 seq=%d: 单事件双轴变化 dx=%d dy=%d（错轴）" % (seq, dx, dy))
            if dx > 0:
                axis_x_plus += 1
            if dy > 0:
                axis_y_plus += 1

        elif kind == "click":
            grid = ev.get("grid")
            row_id, col = ev.get("rowID"), ev.get("col")
            if not SEL_RE.match(str(row_id)):
                fails.append("R14 seq=%d: 非法 RowID %r" % (seq, row_id))
                continue
            colWpx, viewWpx = ev.get("colWpx"), ev.get("viewWpx")
            offX = ev.get("offX")
            if not all(isinstance(v, int) and v > 0 for v in (colWpx, viewWpx)):
                fails.append("R14 seq=%d: click 缺几何（colWpx/viewWpx）" % seq)
                continue
            if not isinstance(col, int) or col < 0 or col >= NCOLS:
                fails.append("R14 seq=%d: 列号越界 col=%r" % (seq, col))
                continue
            # 错列判别：col 必须是 offX 视口下的可见列。
            if not (col * colWpx < offX + viewWpx and (col + 1) * colWpx > offX):
                fails.append("R14 seq=%d: 错列 col=%d 在 offX=%d 下不可见（colWpx=%d viewWpx=%d）"
                             % (seq, col, offX, colWpx, viewWpx))
            if grid == "A":
                if step_idx >= len(steps_a):
                    fails.append("R16 seq=%d: gridA 点击超出场景步骤（多余输入）" % seq)
                    continue
                row_idx, action = steps_a[step_idx]
                expect_id = order_id(row_idx, sorted_desc)
                if row_id != expect_id:
                    fails.append("R16 seq=%d: 错行 click.rowID=%r ≠ 模型 %r（步骤 %d 行 %d %s）"
                                 % (seq, row_id, expect_id, step_idx + 1, row_idx, action))
                set_a, anchor_a, lead_a = model_click(set_a, anchor_a, lead_a, row_idx, action)
                clicked_a += 1
            elif grid == "B":
                if not steps_b:
                    fails.append("R16 seq=%d: gridB 点击超出场景步骤" % seq)
                    continue
                row_idx, _ = steps_b.pop(0)
                expect_id = order_id(row_idx, False)
                if row_id != expect_id:
                    fails.append("R16 seq=%d: gridB 错行 %r ≠ %r" % (seq, row_id, expect_id))

        elif kind == "sel-multi":
            ids, count, lead = ev.get("ids"), ev.get("count"), ev.get("lead")
            if not isinstance(ids, list) or any(not SEL_RE.match(str(i)) for i in ids):
                fails.append("R16 seq=%d: sel-multi 非法 RowID 集 %r" % (seq, ids))
                continue
            if len(set(ids)) != len(ids) or count != len(ids):
                fails.append("R16 seq=%d: 选择集重复或 count 不符 ids=%r count=%r" % (seq, ids, count))
                continue
            if home_pending:
                # Home（初始无选择）选中第 0 行：场景首个 sel-multi。
                expect = [order_id(0, sorted_desc)]
                home_pending = False
                set_a, anchor_a, lead_a = [0], 0, 0
            else:
                if step_idx > len(steps_a):
                    expect = None
                else:
                    expect = [order_id(i, sorted_desc) for i in sorted(set_a)]
                    step_idx += 1
            if expect is None or ids != expect:
                fails.append("R16 seq=%d: 错误选择集 ids=%r ≠ 模型 %r（步骤 %d）"
                             % (seq, ids, expect, step_idx))
            if lead != order_id(lead_a, sorted_desc):
                fails.append("R16 seq=%d: lead %r ≠ 模型锚点 %r"
                             % (seq, lead, order_id(lead_a, sorted_desc) if lead_a is not None else None))

        elif kind == "sort":
            seen_sort += 1
            if seen_sort > 1:
                fails.append("R15 seq=%d: 多余 sort 事件（脚本恰一次）" % seq)
                continue
            bh, bt = ev.get("before-head"), ev.get("before-tail")
            ah, at = ev.get("after-head"), ev.get("after-tail")
            n = ev.get("n")
            if n != N_ROWS:
                fails.append("R15 seq=%d: 排序行数 %r ≠ %d" % (seq, n, N_ROWS))
            if not all(isinstance(x, list) and x for x in (bh, bt, ah, at)):
                fails.append("R15 seq=%d: sort 缺行序样本" % seq)
                continue
            if ah == bh:
                fails.append("R15 seq=%d: 排序后行序未变（无排序）" % seq)
            if sorted(bh + bt) != sorted(ah + at):
                fails.append("R15 seq=%d: 头尾样本多重集不等（排序改变数据集）" % seq)
            if any(ah[i] <= ah[i + 1] for i in range(len(ah) - 1)):
                fails.append("R15 seq=%d: after-head 非严格降序（脚本为反序）" % seq)
            sorted_desc = True

        elif kind == "focus-change":
            if ev.get("to") == "editor":
                focus_editor_before_edit = True

        elif kind == "edit-delta":
            if focus != "editor":
                fails.append("R5b seq=%d: edit-delta focus=%r ≠ editor" % (seq, focus))
            else:
                edit_chars += ev.get("chars", 0)

        elif kind == "sel-change":
            if focus == "gridA" and ev.get("to") and not ev.get("visible"):
                fails.append("R2b seq=%d: gridA 持焦选择当帧不可见 to=%r" % (seq, ev.get("to")))

    # 场景完整性。
    if home_pending:
        fails.append("R16: 缺 Home 初始选择（offY=0 精确回顶路径）")
    if clicked_a != len(steps_a):
        fails.append("R16: gridA 点击步骤未走完（%d/%d）" % (min(clicked_a, len(steps_a)), len(steps_a)))
    if steps_b:
        fails.append("R16: gridB 对照步骤未执行")
    if not axis_y_plus or not axis_x_plus:
        fails.append("R13: N1 未同时出现 Y+ 与 X+ 步进（错轴）")
    if seen_sort != 1:
        fails.append("R15: sort 事件 %d 次 ≠ 1（无排序）" % seen_sort)
    if edit_chars != 2:
        fails.append("R5b: Editor 输入字符数 %d ≠ 2（脚本两字符；文本不入日志）" % edit_chars)
    if not focus_editor_before_edit:
        fails.append("R5b: Editor 输入前无 focus-change editor")
    if lifecycle != ["destroy-callback", "run-return", "exit"]:
        fails.append("R7: 生命周期序列 %r ≠ [destroy-callback, run-return, exit]" % lifecycle)
    if events[-1].get("event") != "exit":
        fails.append("R7: exit 不是最后事件（早退）")

    if fails:
        print("STRUCTURAL: FAIL")
        for f in fails:
            print("  - %s" % f)
        print("SCENARIO: MANUAL-REQUIRED — 结构判定不含因果确认")
        return 1
    print("STRUCTURAL: PASS — N1–N6 模型时间线、双轴、命中列、排序与选择集全部一致")
    print("SCENARIO: MANUAL-REQUIRED — 原生阶段需操作者按 RUNBOOK-R29 确认")
    return 0


if __name__ == "__main__":
    sys.exit(main())

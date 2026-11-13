#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""R29-B R3 DataGrid 交互扩展（N1–N6）原生验收日志结构判定器 v3。

用法:
  python judge_r29_native_log.py <log.jsonl> [--json -|路径]

双判定（与 R28 judge_native_log.py 同构）：
  STRUCTURAL: PASS/FAIL — 模型化场景时间线与不变式判定
  SCENARIO:   始终 MANUAL-REQUIRED — 「操作者按场景意图做了」的因果确认

R3 域校验与几何一致性（本版新增；先于行列推导）：
  视口四边 R14d x ∈ [0, viewWpx)（右边界 x==viewWpx 无效——像素域半开
           区间）；y ∈ [headerHpx, headerHpx+bodyHpx)（行点击不可能落在
           表头或表体之外）。
  偏移域   R14e offX ∈ [0, offXMax]、offY ∈ [0, offYMax]（命中当刻值）。
  命中几何 R14f click 的 offX/offY 为**命中当刻**值（cellAt 实际使用），
           必须等于轴时间线在本点击之前的运行值，或同帧 axis 事件的值
           （同帧滚动/缩放时二者可不同——不得混用不同时间的几何）。
  gridB    R17 gridB 单选身份：click.selIds 必为 [被点行]、lead=被点行；
           selB-change.to 必须等于模型；变更必须在下一场景步骤前留痕。

沿 R2 精确判据：
  精确列   R14a click.col 必须等于按**当帧真实几何与本地指针坐标**计算的
           列：(x + offX) // colWpx。错误但仍可见的列一律拒绝（R1 的
           「可见性」判据不足——可见列≠命中列）。
  精确行   R14b click.rowID 必须等于按 (y - headerHpx + offY) // rowHpx
           与当序推导的行（命中映射一致性）。
  快照关联 R16a click 自带点击当刻选择快照（selIds/lead），必须与选择
           模型逐步一致；每个选择变更必须在其后、下一个场景步骤前以
           sel-multi 留痕；退出时不得有未留痕变更（缺失/错序均拒绝）。
           末步点击的最终选择可核对。
  宽容     平台滚轮步长差异（轴判定只看方向/单轴/钳制域）与**未改变
           选择的真实事件**（脚本外 click 且快照=当前集 → 容忍并计数）。
  依赖现场 SCENARIO 人工确认项见 manual 清单（操作者瞄准的行、原生
           窗口存在性等）。

其余不变式沿 R1：R0 seq 严格递增；R7 生命周期三事件且 exit 末位；
R12 run-return.err 非 "<nil>" 拒；R13 轴独立（单事件单轴 + 双轴覆盖 +
钳制域）；R15 排序恰一次且行序翻转；R5b edit-delta 归属 Editor。
旧格式日志（click 无坐标/快照字段）按缺字段拒绝——判据升级后旧日志
不再可判定。
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
    """按选择模型处理一次 gridA 点击，返回 (新集合, 锚点, lead)。

    锚点（selAnchor）与 lead（selID）语义与实现一致：plain/Ctrl 移动
    锚点与 lead（lead 可在集合外——Ctrl 切换撤销）；Shift 重算区间、
    锚点不动、lead 移动到被点行。
    """
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
            lead_a = idx
    return new_set, anchor_a, lead_a


def main():
    argv = sys.argv[1:]
    json_out = None
    if "--json" in argv:
        i = argv.index("--json")
        json_out = argv[i + 1]
        argv = argv[:i] + argv[i + 2:]
    if len(argv) != 1:
        print("usage: python judge_r29_native_log.py <log.jsonl> [--json -|路径]")
        return 1
    try:
        raw = Path(argv[0]).read_text(encoding="utf-8").strip()
        events = [json.loads(line) for line in raw.splitlines()]
    except (OSError, ValueError) as err:
        print("STRUCTURAL: FAIL — 日志不可读: %s" % err)
        return 1
    if not events or any(not isinstance(ev, dict) for ev in events):
        print("STRUCTURAL: FAIL — 空日志或日志项不是对象")
        return 1

    # 预扫描：axis 事件按帧索引（同帧点击的轴事件在 click 之后才出现，
    # 命中当刻几何一致性需要整帧视角）。
    axis_by_frame = {}
    for ev0 in events:
        if ev0.get("event") == "axis" and isinstance(ev0.get("frame"), int):
            axis_by_frame[ev0["frame"]] = (ev0.get("offX"), ev0.get("offY"))

    fails = []
    items = []      # 结构化逐项结果
    manual = []     # 依赖现场观察的项（SCENARIO）

    def item(iid, ok, detail):
        items.append({"id": iid, "status": "pass" if ok else "fail", "detail": detail})
        if not ok:
            fails.append("%s: %s" % (iid, detail))

    sorted_desc = False
    set_a, anchor_a, lead_a = [], None, None
    last_seen = None        # 最近一次留痕的 gridA 选择快照（列表）
    pending_change = False  # 模型集与 last_seen 不一致，等待 sel-multi 留痕
    home_pending = True
    steps_b = [(0, "plain")]
    seen_sort = 0
    axis_y_plus = axis_x_plus = 0
    focus_editor_before_edit = False
    edit_chars = 0
    lifecycle = []
    last_seq = 0
    step_idx = 0
    clicked_a = 0
    tolerated_noop = 0
    selb_seen = ""
    row_aim_variance = []
    n_exact_col = n_exact_row = n_snapshot = 0
    n_domain = n_domain_ok = 0
    axis_run_x = axis_run_y = None   # 轴时间线运行值（本点击之前）
    selb_model = ""                  # gridB 单选身份（恒等序）
    selb_pending = False

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
                item("R12-run-return", False, "seq=%d run-return.err=%r（fixture 运行失败）" % (seq, ev.get("err")))
        elif kind == "exit":
            lifecycle.append("exit")

        elif kind == "axis":
            offX, offY = ev.get("offX"), ev.get("offY")
            offXMax, offYMax = ev.get("offXMax"), ev.get("offYMax")
            prevX, prevY = ev.get("prevX"), ev.get("prevY")
            if not all(isinstance(v, int) for v in (offX, offY, offXMax, offYMax, prevX, prevY)):
                item("R13-axis-fields", False, "seq=%d axis 缺偏移整数域" % seq)
                continue
            if offX < 0 or offX > offXMax or offY < 0 or offY > offYMax:
                item("R13-axis-clamp", False, "seq=%d 偏移越界 off=(%d,%d) max=(%d,%d)" % (seq, offX, offY, offXMax, offYMax))
            dx, dy = offX - prevX, offY - prevY
            if dx != 0 and dy != 0:
                item("R13-axis-single", False, "seq=%d 单事件双轴变化 dx=%d dy=%d（错轴）" % (seq, dx, dy))
            if dx > 0:
                axis_x_plus += 1
            if dy > 0:
                axis_y_plus += 1
            axis_run_x, axis_run_y = offX, offY

        elif kind == "click":
            grid = ev.get("grid")
            row_id, col = ev.get("rowID"), ev.get("col")
            x, y = ev.get("x"), ev.get("y")
            colWpx, viewWpx = ev.get("colWpx"), ev.get("viewWpx")
            rowHpx, headerHpx = ev.get("rowHpx"), ev.get("headerHpx")
            bodyHpx = ev.get("bodyHpx")
            offX, offY = ev.get("offX"), ev.get("offY")
            sel_ids, lead = ev.get("selIds"), ev.get("lead")
            click_frame = ev.get("frame")
            if not SEL_RE.match(str(row_id)):
                item("R14-rowid", False, "seq=%d 非法 RowID %r" % (seq, row_id))
                continue
            if not (isinstance(x, int) and isinstance(y, int)):
                # 旧格式日志（无本地坐标）：判据升级后不可判定 → 拒绝。
                item("R14a-coords", False, "seq=%d click 缺本地坐标 x/y（旧格式不可判定，拒绝）" % seq)
                continue
            if not (isinstance(sel_ids, list) and isinstance(colWpx, int) and colWpx > 0
                    and isinstance(viewWpx, int) and viewWpx > 0):
                item("R16a-snapshot", False, "seq=%d click 缺选择快照或几何（旧格式不可判定，拒绝）" % seq)
                continue

            # —— R3 域校验（先于行列推导；不合法坐标不进入推导）——
            if not (0 <= x < viewWpx):
                item("R14d-domain-x", False,
                     "seq=%d 点击坐标越界 x=%d 不在 [0,%d)（右边界/视口外像素域）"
                     % (seq, x, viewWpx))
            if not (isinstance(bodyHpx, int) and bodyHpx > 0):
                item("R14d-fields", False, "seq=%d click 缺 bodyHpx" % seq)
            elif not (headerHpx <= y < headerHpx + bodyHpx):
                item("R14d-domain-y", False,
                     "seq=%d 点击纵坐标越界 y=%d 不在 [header=%d, header+body=%d)（表头/表体四边）"
                     % (seq, y, headerHpx, headerHpx + bodyHpx))
            offXMax, offYMax = ev.get("offXMax"), ev.get("offYMax")
            if not (isinstance(offXMax, int) and offXMax >= 0 and isinstance(offYMax, int) and offYMax >= 0):
                item("R14e-fields", False, "seq=%d click 缺 offXMax/offYMax" % seq)
            elif not (0 <= offX <= offXMax and 0 <= offY <= offYMax):
                item("R14e-domain-off", False,
                     "seq=%d 偏移越域 off=(%d,%d) max=(%d,%d)" % (seq, offX, offY, offXMax, offYMax))
            # 命中当刻几何 × 轴时间线（仅 gridA——轴事件只来自 gridA；
            # gridB 无横向滚动，offX 恒 0，由偏移域校验覆盖）：允许
            # {本点击前运行值} 加 {同帧 axis 值}。
            if grid == "A":
                allowed_x = {axis_run_x} if axis_run_x is not None else set()
                allowed_y = {axis_run_y} if axis_run_y is not None else set()
                if click_frame in axis_by_frame:
                    allowed_x.add(axis_by_frame[click_frame][0])
                    allowed_y.add(axis_by_frame[click_frame][1])
                if allowed_x and offX not in allowed_x:
                    item("R14f-hit-geom", False,
                         "seq=%d 命中当刻 offX=%d 与轴时间线不一致（允许值 %s）"
                         % (seq, offX, sorted(v for v in allowed_x if v is not None)))
                if allowed_y and offY not in allowed_y:
                    item("R14f-hit-geom", False,
                         "seq=%d 命中当刻 offY=%d 与轴时间线不一致（允许值 %s）"
                         % (seq, offY, sorted(v for v in allowed_y if v is not None)))
            n_domain += 1
            tail = [i["id"] for i in items[-8:]]
            if not any(i.startswith(("R14d", "R14e", "R14f")) for i in tail):
                n_domain_ok += 1

            # 精确列：坐标 × 当帧几何（错误但仍可见的列在此被拒）。
            col_calc = (x + offX) // colWpx
            if col != col_calc:
                item("R14a-exact-col", False,
                     "seq=%d 错列 col=%d ≠ 坐标推算 %d（x=%d offX=%d colWpx=%d；两列可能均可见）"
                     % (seq, col, col_calc, x, offX, colWpx))
            elif not (0 <= col < NCOLS):
                item("R14a-exact-col", False, "seq=%d col=%d 越界" % (seq, col))
            else:
                n_exact_col += 1
            # 精确行：命中映射一致性（gridA 用当序——可能已排序；
            # gridB 恒等序，从不排序）。
            row_calc = None
            if isinstance(rowHpx, int) and rowHpx > 0 and isinstance(headerHpx, int):
                row_calc = (y - headerHpx + offY) // rowHpx
                expect_row = order_id(row_calc, sorted_desc and grid == "A")
                if not (0 <= row_calc < N_ROWS):
                    item("R14b-exact-row", False, "seq=%d 行越界 row=%d" % (seq, row_calc))
                elif row_id != expect_row:
                    item("R14b-exact-row", False,
                         "seq=%d 错行 rowID=%r ≠ 坐标推算 %r（y=%d offY=%d rowHpx=%d）"
                         % (seq, row_id, expect_row, y, offY, rowHpx))
                else:
                    n_exact_row += 1
            else:
                item("R14b-fields", False, "seq=%d click 缺 rowHpx/headerHpx" % seq)

            if grid == "A":
                if step_idx >= len(steps_a):
                    # 脚本外点击：未改变选择的真实事件 → 容忍；改变选择 → 拒。
                    if sel_ids == (last_seen if last_seen is not None else []):
                        tolerated_noop += 1
                    else:
                        item("R16-steps", False,
                             "seq=%d gridA 点击超出场景步骤且改变了选择 %r（脚本外）" % (seq, sel_ids))
                    continue
                # 场景阶段边界：上一步的选择变更必须在下一步开始前留痕
                # （缺失中段快照在此被拒——不只看退出收口）。
                if pending_change:
                    item("R16a-gap", False,
                         "seq=%d 上一步选择变更未留痕（模型 %r vs 最后快照 %r——缺 sel-multi）"
                         % (seq, [order_id(i, sorted_desc) for i in sorted(set_a)], last_seen))
                row_idx, action = steps_a[step_idx]
                set_a, anchor_a, lead_a = model_click(set_a, anchor_a, lead_a, row_idx, action)
                clicked_a += 1
                # 快照关联：点击当刻选择必须与模型一致。
                expect_ids = [order_id(i, sorted_desc) for i in sorted(set_a)]
                if sel_ids != expect_ids:
                    item("R16a-snapshot", False,
                         "seq=%d 错误选择集（click 快照）%r ≠ 模型 %r（步骤 %d 行 %d %s）"
                         % (seq, sel_ids, expect_ids, step_idx + 1, row_idx, action))
                expect_lead = order_id(lead_a, sorted_desc) if lead_a is not None else ""
                if lead != expect_lead:
                    item("R16a-lead", False,
                         "seq=%d lead %r ≠ 模型 %r（步骤 %d）" % (seq, lead, expect_lead, step_idx + 1))
                n_snapshot += 1
                if row_calc is not None and row_calc != row_idx:
                    # 瞄准行偏离脚本：SCENARIO 人工项（选择快照兜底——集合
                    # 不一致已在上方拒绝）。
                    row_aim_variance.append("步骤 %d：脚本行 %d，实点行 %d"
                                            % (step_idx + 1, row_idx, row_calc))
                step_idx += 1
                if last_seen is None or expect_ids != last_seen:
                    pending_change = True
            elif grid == "B":
                if not steps_b:
                    item("R16-steps-b", False, "seq=%d gridB 点击超出场景步骤" % seq)
                    continue
                if pending_change:
                    item("R16a-gap", False,
                         "seq=%d 上一步选择变更未留痕（缺 sel-multi）" % seq)
                steps_b.pop(0)
                # R17 gridB 单选身份：点击后选择集必为 [被点行]、lead=被点行。
                if sel_ids != [row_id]:
                    item("R17-gridb-snapshot", False,
                         "seq=%d gridB 选择快照 %r 与被点行 %r 矛盾（单选身份）"
                         % (seq, sel_ids, row_id))
                if lead != row_id:
                    item("R17-gridb-lead", False,
                         "seq=%d gridB lead %r 与被点行 %r 矛盾" % (seq, lead, row_id))
                selb_model = row_id
                selb_pending = True

        elif kind == "sel-multi":
            ids, count, lead = ev.get("ids"), ev.get("count"), ev.get("lead")
            if not isinstance(ids, list) or any(not SEL_RE.match(str(i)) for i in ids):
                item("R16-ids", False, "seq=%d sel-multi 非法 RowID 集 %r" % (seq, ids))
                continue
            if len(set(ids)) != len(ids) or count != len(ids):
                item("R16-count", False, "seq=%d 选择集重复或 count 不符 ids=%r count=%r" % (seq, ids, count))
                continue
            if home_pending:
                # Home（初始无选择）选中第 0 行：场景首个选择快照。
                expect = [order_id(0, sorted_desc)]
                if ids != expect:
                    item("R16-home", False, "seq=%d Home 初始选择 %r ≠ %r" % (seq, ids, expect))
                home_pending = False
                set_a, anchor_a, lead_a = [0], 0, 0
                last_seen = list(expect)
                pending_change = False
            else:
                if pending_change:
                    expect = [order_id(i, sorted_desc) for i in sorted(set_a)]
                    if ids != expect:
                        item("R16-sel-multi", False,
                             "seq=%d 留痕快照 %r ≠ 模型 %r（错序或错误选择集）" % (seq, ids, expect))
                    last_seen = list(expect)
                    pending_change = False
                else:
                    item("R16-sel-multi-order", False,
                         "seq=%d 多余选择快照 %r（当刻模型无未留痕变更——错序）" % (seq, ids))

        elif kind == "selB-change":
            to = ev.get("to")
            if to:
                selb_seen = to
            if to != selb_model:
                item("R17-gridb-timeline", False,
                     "seq=%d selB-change.to=%r 与 gridB 模型 %r 矛盾" % (seq, to, selb_model))
            else:
                selb_pending = False

        elif kind == "sort":
            seen_sort += 1
            if seen_sort > 1:
                item("R15-count", False, "seq=%d 多余 sort 事件（脚本恰一次）" % seq)
                continue
            bh, bt = ev.get("before-head"), ev.get("before-tail")
            ah, at = ev.get("after-head"), ev.get("after-tail")
            n = ev.get("n")
            if n != N_ROWS:
                item("R15-n", False, "seq=%d 排序行数 %r ≠ %d" % (seq, n, N_ROWS))
            if not all(isinstance(x, list) and x for x in (bh, bt, ah, at)):
                item("R15-fields", False, "seq=%d sort 缺行序样本" % seq)
                continue
            if ah == bh:
                item("R15-changed", False, "seq=%d 排序后行序未变（无排序）" % seq)
            if sorted(bh + bt) != sorted(ah + at):
                item("R15-multiset", False, "seq=%d 头尾样本多重集不等（排序改变数据集）" % seq)
            if any(ah[i] <= ah[i + 1] for i in range(len(ah) - 1)):
                item("R15-desc", False, "seq=%d after-head 非严格降序（脚本为反序）" % seq)
            sorted_desc = True

        elif kind == "focus-change":
            if ev.get("to") == "editor":
                focus_editor_before_edit = True

        elif kind == "edit-delta":
            if focus != "editor":
                item("R5b-focus", False, "seq=%d edit-delta focus=%r ≠ editor" % (seq, focus))
            else:
                edit_chars += ev.get("chars", 0)

        elif kind == "sel-change":
            if focus == "gridA" and ev.get("to") and not ev.get("visible"):
                item("R2b-visible", False, "seq=%d gridA 持焦选择当帧不可见 to=%r" % (seq, ev.get("to")))

    # 场景完整性与留痕收口。
    if home_pending:
        item("R16-home", False, "缺 Home 初始选择（offY=0 精确回顶路径）")
    if clicked_a != len(steps_a):
        item("R16-steps", False, "gridA 点击步骤未走完（%d/%d）" % (min(clicked_a, len(steps_a)), len(steps_a)))
    if steps_b:
        item("R16-steps-b", False, "gridB 对照步骤未执行")
    if not selb_seen:
        item("R16-selB", False, "gridB 选择快照缺失（N6 对照阶段）")
    elif selb_seen != order_id(0, False):
        item("R16-selB", False, "gridB 最终选择 %r ≠ %r" % (selb_seen, order_id(0, False)))
    if selb_pending:
        item("R17-gridb-timeline", False, "gridB 点击后的选择变更未留痕（缺 selB-change）")
    if pending_change or (last_seen is not None
                          and [order_id(i, sorted_desc) for i in sorted(set_a)] != last_seen):
        item("R16a-final", False,
             "退出时存在未留痕的选择变更：模型 %r vs 最后留痕 %r（缺失末次选择快照）"
             % ([order_id(i, sorted_desc) for i in sorted(set_a)], last_seen))
    if not axis_y_plus or not axis_x_plus:
        item("R13-coverage", False, "N1 未同时出现 Y+ 与 X+ 步进（错轴）")
    if seen_sort != 1:
        item("R15-count", False, "sort 事件 %d 次 ≠ 1（无排序）" % seen_sort)
    if edit_chars != 2:
        item("R5b-chars", False, "Editor 输入字符数 %d ≠ 2（脚本两字符；文本不入日志）" % edit_chars)
    if not focus_editor_before_edit:
        item("R5b-focus", False, "Editor 输入前无 focus-change editor")
    if lifecycle != ["destroy-callback", "run-return", "exit"]:
        item("R7-lifecycle", False, "生命周期序列 %r ≠ [destroy-callback, run-return, exit]" % lifecycle)
    if events[-1].get("event") != "exit":
        item("R7-exit-last", False, "exit 不是最后事件（早退）")

    manual.append("操作者瞄准行与脚本步骤的对应（结构判据由坐标命中映射+选择快照兜底）")
    manual.append("原生窗口真实存在、滚轮/修饰键为真实输入")
    if row_aim_variance:
        manual.extend(row_aim_variance)

    verdict = "PASS" if not fails else "FAIL"
    print("STRUCTURAL: %s — %s" % (
        verdict,
        "N1–N6 模型时间线、精确命中列/行（坐标×几何）、排序与选择快照留痕全部一致"
        if verdict == "PASS" else "存在 %d 项结构不符（见逐项结果）" % len(fails)))
    print("SCENARIO: MANUAL-REQUIRED — %d 项依赖现场确认" % len(manual))
    if tolerated_noop:
        print("TOLERATED: %d 个未改变选择的脚本外事件（按宽容规则计数）" % tolerated_noop)
    for f in fails:
        print("  - %s" % f)

    if json_out:
        def stage(sid, ok, detail):
            return {"id": sid, "status": "pass" if ok else "fail", "detail": detail}
        doc = {
            "verdict": verdict,
            "structural": verdict,
            "scenario": "MANUAL-REQUIRED",
            "scenario_results": [
                stage("N1-axis", axis_y_plus > 0 and axis_x_plus > 0
                      and not any(f.startswith("R13") for f in fails),
                      "Y+ ×%d、X+ ×%d（单事件单轴，域钳制）" % (axis_y_plus, axis_x_plus)),
                stage("N2-N5-exact-hit", n_exact_col > 0 and n_exact_row > 0
                      and not any(f.startswith("R14") for f in fails),
                      "精确列核对 %d 次、精确行核对 %d 次（坐标×当帧几何）" % (n_exact_col, n_exact_row)),
                stage("N3-sort", seen_sort == 1
                      and not any(f.startswith("R15") for f in fails),
                      "排序恰一次，行序翻转样本核对"),
                stage("N4-N5-snapshot", not any(f.startswith("R16a") for f in fails),
                      "选择快照关联 %d 次（click.selIds 与 sel-multi 留痕一致）" % n_snapshot),
                stage("N6-dual-editor", selb_seen == order_id(0, False)
                      and edit_chars == 2 and focus_editor_before_edit
                      and not any(f.startswith("R17") for f in fails),
                      "gridB 对照单选身份（快照+selB 时间线一致，最终 %r）；Editor 焦点交接与输入计数" % selb_seen),
                stage("R3-domain", n_domain > 0
                      and not any(f.startswith(("R14d", "R14e", "R14f")) for f in fails),
                      "域校验 %d 次（视口四边/偏移域/命中当刻几何×轴时间线）" % n_domain),
                stage("lifecycle", lifecycle == ["destroy-callback", "run-return", "exit"]
                      and events[-1].get("event") == "exit",
                      "destroy-callback → run-return → exit，exit 末位"),
            ],
            "items": items,
            "manual": manual,
            "counters": {
                "gridA_clicks": clicked_a,
                "steps_expected": len(steps_a),
                "tolerated_noop": tolerated_noop,
                "sort_events": seen_sort,
                "edit_chars": edit_chars,
                "exact_col_checks": n_exact_col,
                "exact_row_checks": n_exact_row,
                "snapshot_checks": n_snapshot,
                "domain_checks": n_domain,
                "domain_clean": n_domain_ok,
                "fails": len(fails),
            },
        }
        text = json.dumps(doc, ensure_ascii=False, indent=1)
        if json_out == "-":
            sys.stdout.write(text + "\n")
        else:
            Path(json_out).write_text(text + "\n", encoding="utf-8")
    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())

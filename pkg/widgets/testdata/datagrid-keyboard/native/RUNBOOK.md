# R28-B DataGrid 原生键盘验收 fixture — 运行手册（runbook）

**执行前提**：协调者授权的原生阶段。作者阶段不开窗、不注入按键、不执行
本 fixture。本 runbook 是操作方案，不是「原生通过」声明。

## 1. 构建二进制（作者阶段已完成并记录 SHA-256/大小；如需重编见下）

```bash
# 普通（CGO=0）
CGO_ENABLED=0 go build -tags nativewindow -c -o native-grid-fixture.test.exe ./pkg/widgets
# race（CGO=1 MinGW）
CC=D:/Codes/SQLMonitor/.tools/mingw64/bin/gcc.exe CGO_ENABLED=1 \
  go test -tags nativewindow -c -race -o native-grid-fixture-race.test.exe ./pkg/widgets
```

## 2. 运行（record-command 留痕 + 容器收口）

```bash
python scripts/record-command.py run --id native-grid-fixture \
  --jsonl <ledger> --log-dir <rawdir> \
  --cwd <worktree> --containment win-job --timeout 300 \
  --lock-path D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock \
  --lock-mode acquire --lock-agent <runner> --lock-task R28-B-NATIVE-KEYBOARD \
  -- ./native-grid-fixture.test.exe -test.run TestNativeDataGridKeyboardFixture \
     -test.v -fixture-log <abs path>/native-grid-fixture-log.jsonl
```

- containment win-job：进程对象在 Job 内，超时/失败由运行方 `taskkill /T /PID <pid> /F` 收口；
- 每帧 flush 日志，强杀不丢已记录帧；
- 操作者按下方步骤操作，结束后标题栏关闭（优雅退出，rc=0）；
- 超时前未关闭 → 容器杀进程 → judge 以已有帧判定（缺 exit 事件 → FAIL R7，如实记录）。

## 3. 操作脚本（配合 native-acceptance-plan 的 A/B/C/D 表）

| 时段 | 操作 | 日志自动判定（judge） | 人工观察 |
|---|---|---|---|
| T0 | 启动（不开键盘导航前对照已内置：gridB） | start/resize 事件 | 窗口出现、两表与 Editor 可见 |
| T1 | 点击 gridA 第 3 行 | sel-change→ROW-00002 visible=true、focus=gridA | 行高亮 |
| T2 | ↓ ×5（含按住连发 1 次） | sel-change 序列 ROW-00003..；visible=true | 滚动跟随 |
| T3 | End | ROW-09999、visible=true、scroll=max | |
| T4 | Home | ROW-00000、offset=0 | |
| T5 | PageDown/PageUp | 整页移动并钳制 | 记录实际页行数 |
| T6 | 点击 Editor，输入 abc（IME 关） | edit-delta chars=+3 focus=editor；unconsumed-key ↓（网格不吞） | 文本进 Editor |
| T7 | IME 开启，中文输入 | edit-delta focus=editor | IME 组合窗归属 Editor；网格不受影响 |
| T8 | Editor 持焦按 ↓/Home | unconsumed-key 或 editor 行为；网格选择不变 | |
| T9 | Windows 缩放 100%→150%（设置→显示） | resize 事件 pxPerDp 1→1.5 | 行高/命中一致 |
| T10 | 最大化/还原 | resize 事件 | 几何正确 |
| T11 | 拖动窗口角落缩放 | resize 事件序列 | |
| T12 | gridB（禁用对照）点击选择后按键 | 无键盘反应（鼠标选择可用） | |
| T13 | 标题栏关闭 | close → exit | rc=0 |

## 4. 判定

```bash
python pkg/widgets/testdata/datagrid-keyboard/native/judge_native_log.py <log.jsonl>
# VERDICT: PASS（不变式 R1..R8 全过）/ FAIL（逐条列出）
```

人工观察项（IME 组合窗归属、视觉、卡顿主观感受）按表记录，不并入 judge。

## 5. 终止条件

- 正常：操作者完成 T1–T13 后标题栏关闭。
- 异常：窗口无响应 >10s、日志 >60s 无 heartbeat、或出现 FAIL 级不变式
  —— 立即关闭/容器收口，保留 raw，按缺陷流程回填。

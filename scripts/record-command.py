#!/usr/bin/env python
# -*- coding: utf-8 -*-
r"""R8-C **随仓命令记录入口**：把"跑过的每条命令"变成可复核证据。

它**不重写**进程收口与 raw 捕获——那是 `verify_examples_proc.py`（R6 已合入、已复核）
的职责。这里只做三件事：

  1. 用 `run_capture` 真跑一条命令（shell=False、真实 argv 数组、局部环境、
     原始双通道字节 + SHA、PID + 创建时间、单调区间、真实 rc/timeout/cleanup）；
  2. 把这次执行的**事实**写成一行 JSONL 台账（schema 见 `evidence_schema.py`）；
  3. 把"记录成功"与"被测命令业务成功"**分开**判定，并各自给调用者明确退出码。

三个状态必须分得清（R7 复盘第 3 条）：

  * `prepared` —— 只写在 `<run-dir>/planned.json`：仅说明"打算跑什么"，
    **不能**据此声称"零启动"或"没跑过"；
  * `completed` / `recovered` —— 拿到真实结果（recovered 是事后从 `result.json` 恢复的）；
  * `unknown` —— 硬中断/崩溃导致启动结果无从得知；此时 started/rc 一律写 None，
    **不把未知记成 0**，校验器判 INCOMPLETE，绝不判通过。

退出码（调用方按此判据，不做"记录失败却包装 rc 0"）：

  0  记录完整可信 **且** 业务判据成立 **且** 收口已证 **且** 锁已释放
  2  用法/环境错误：参数自相矛盾、输出路径已存在、声明"外层持锁"却找不到锁
  3  没有真正启动（启动失败 / 被启动前拒绝 / 结果未知）
  4  跑起来了但业务判据不成立（rc 不等于 --expect-rc 等）
  5  超时（rc 是我们强制给的，不是进程的自然退出码）
  6  收口未被证明（contained/activeAfter/cleanup 有问题）——取证可用，但别当"干净跑完"
  7  记录本身不完整（台账行写坏、raw 哈希与磁盘不符、自相矛盾）
  8  公共锁被他人持有：本次**没有发起**任何子进程
  9  自己取的锁没能释放：取证可用，但必须人工处理（禁止按年龄删锁）

用法：

  python record-command.py run --id <id> --jsonl <file> --log-dir <dir> [--cwd D]
      [--expect-rc N] [--expect-timeout] [--timeout SEC] [--env K=V]... [--env-key K]...
      [--go-env] [--lock-path P --lock-mode acquire|external] [--report P]
      [--args-file P | --] <cmd> <args...>

  python record-command.py recover --run-dir <dir> --jsonl <file> [--report P]

跨宿主传参保真：PowerShell 对空串/引号/尾反斜杠的转义不可靠，
推荐把 argv 写成 JSON 数组文件后传 `--args-file`（本工具直接 json.load，
不经 shell，也不再二次转义）。
"""
import argparse
import json
import math
import os
import signal
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import verify_examples_proc as proc  # noqa: E402  （复用，不重写）
import evidence_schema as schema  # noqa: E402

LOCK_HELPER = os.path.join(HERE, 'verify-examples-lock.py')

EXIT_OK = 0
EXIT_USAGE = 2
EXIT_NOT_STARTED = 3
EXIT_BUSINESS_FAIL = 4
EXIT_TIMEOUT = 5
EXIT_CONTAINMENT_UNKNOWN = 6
EXIT_RECORD_INCOMPLETE = 7
EXIT_LOCK_BUSY = 8
EXIT_LOCK_NOT_RELEASED = 9

# 环境白名单：只记这些键的**值**，外加我们改动的差异；绝不 dump 整个父环境。
ENV_ALLOWLIST = ('CGO_ENABLED', 'GOMAXPROCS', 'GOPROXY', 'GOSUMDB', 'GOTOOLCHAIN',
                 'GOFLAGS', 'GOOS', 'GOARCH', 'GOPATH', 'GOCACHE', 'GOMODCACHE',
                 'GOENV', 'GOTMPDIR', 'GOWORK', 'TMP', 'TEMP')

# 只有这些 issue 才算"记录本身不可信"；其余（超时/未启动/收口未知）是**证据不完整**，
# 记录仍然可信，二者不得混为一谈。
CONTRADICTION_CODES = frozenset((
    'bad-type', 'missing-field', 'bad-phase', 'missing-schema', 'unknown-schema',
    'not-an-object', 'rc-null-source-mismatch', 'rc-int-source-mismatch',
    'unknown-rc-source', 'forced-rc-without-timeout', 'timeout-rc-source-mismatch',
    'not-started-but-claims-rc', 'not-started-but-claims-pid', 'not-started-but-timed-out',
    'started-without-pid', 'rc-out-of-range', 'time-order', 'bad-start-time',
    'bad-end-time', 'unknown-phase-claims-facts', 'unknown-phase-claims-ok',
    'business-pass-contradiction', 'business-pass-rc-mismatch', 'business-pass-not-started',
    'record-ok-with-reasons', 'raw-bytes-mismatch', 'raw-sha-mismatch', 'raw-sha-bad-type',
    'raw-file-missing', 'missing-recovery-source',
))


class UsageError(Exception):
    def __init__(self, message):
        Exception.__init__(self, message)
        self.message = message


def load_lock_helper():
    import importlib.util
    spec = importlib.util.spec_from_file_location('verify_examples_lock_for_record', LOCK_HELPER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def utc_now():
    return proc.utc()


# --------------------------------------------------------------------------- 台账 IO


def read_jsonl_ids(path):
    """读已有台账里的 id（坏行跳过，不修、不删）。"""
    ids = set()
    if not os.path.isfile(path):
        return ids
    with open(path, 'rb') as fh:
        raw = fh.read()
    for line in raw.split(b'\n'):
        if not line.strip():
            continue
        try:
            obj = json.loads(line.decode('utf-8'))
        except (ValueError, UnicodeDecodeError):
            continue
        if isinstance(obj, dict) and isinstance(obj.get('id'), str):
            ids.add(obj['id'])
    return ids


def next_unique_id(path, base):
    existing = read_jsonl_ids(path)
    if base not in existing:
        return base
    for n in range(2, 1000):
        cand = '%s-%d' % (base, n)
        if cand not in existing:
            return cand
    raise UsageError('台账里已有 999 个同名 id：%s' % base)


def append_jsonl(path, obj):
    """追加一行。**永不截断、永不覆盖**；上一行缺换行时补一个换行并如实记录。

    返回 `(line, repaired_newline)`。
    """
    parent = os.path.dirname(os.path.abspath(path))
    if parent:
        os.makedirs(parent, exist_ok=True)
    if os.path.isdir(path):
        raise UsageError('--jsonl 指向目录：%s' % path)
    repaired = False
    if os.path.isfile(path) and os.path.getsize(path) > 0:
        with open(path, 'rb') as fh:
            fh.seek(-1, os.SEEK_END)
            if fh.read(1) != b'\n':
                # 上一行是被打断的半行：补换行让它成为一条**可被判红的完整行**，
                # 绝不在它后面接着写（那会把两条记录粘成一行假 JSON）。
                repaired = True
    line = json.dumps(obj, ensure_ascii=False, sort_keys=True,
                      separators=(',', ':')).encode('utf-8')
    # Windows 上 os.open 默认是**文本模式**：'\n' 会被翻译成 '\r\n'。
    # 台账要求字节级一致（R6 的 raw 捕获也是这个口径），因此必须显式 O_BINARY。
    flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND | getattr(os, 'O_BINARY', 0)
    fd = os.open(path, flags, 0o644)
    try:
        os.write(fd, (b'\n' if repaired else b'') + line + b'\n')
        os.fsync(fd)
    finally:
        os.close(fd)
    return line, repaired


def is_final_result(loaded, planned=None, case_dir=None):
    """`result.json` 是不是**最终那一版**。

    `run_capture` 启动时先写一份带 `phase='prepared'` 的 result.json（"打算跑什么"），
    跑完再写最终版（**没有** phase 键）。硬终止留下的往往是 prepared 版——
    拿它当结果会凭空造出"已跑过"的事实，所以这里必须区分。
    """
    if not (isinstance(loaded, dict) and loaded.get('format') == proc.RAW_FORMAT
            and loaded.get('caseId') == 'c1' and loaded.get('phase') is None):
        return False
    if planned is not None and (loaded.get('argv') != planned.get('argv')
                                or loaded.get('cwd') != planned.get('cwd')):
        return False
    if case_dir is not None and os.path.normcase(os.path.abspath(
            loaded.get('caseDir') or '')) != os.path.normcase(os.path.abspath(case_dir)):
        return False
    return True


def reparse_last_line(path):
    """写完立刻按**字节**回读最后一行并 parse（JSONL 完成时的逐行复核）。"""
    try:
        with open(path, 'rb') as fh:
            raw = fh.read()
    except OSError as exc:
        return None, 'reread-failed: %s' % exc
    lines = [ln for ln in raw.split(b'\n') if ln.strip()]
    if not lines:
        return None, 'empty-after-write'
    try:
        return json.loads(lines[-1].decode('utf-8')), None
    except (ValueError, UnicodeDecodeError) as exc:
        return None, 'bad-json-after-write: %s' % exc


def exclusive_dir(parent, base):
    """独占目录：已存在就顺延 `-2`/`-3`，**不覆盖**任何历史字节。"""
    os.makedirs(parent, exist_ok=True)
    for n in range(1, 1000):
        name = base if n == 1 else '%s-%d' % (base, n)
        path = os.path.join(parent, name)
        try:
            os.makedirs(path, exist_ok=False)
            return path
        except FileExistsError:
            continue
    raise UsageError('无法为 %s 分配独占目录（已有 999 个）' % base)


def write_exclusive(path, obj):
    """独占写入：路径已存在即报错（不覆盖用户报告/他人产物）。"""
    with open(path, 'x', encoding='utf-8', newline='\n') as fh:
        json.dump(obj, fh, ensure_ascii=False, indent=2, sort_keys=True)
    return path


# --------------------------------------------------------------------------- 记录构建


def env_block(overrides, extra_keys):
    child = proc._child_env(overrides)
    keys = list(ENV_ALLOWLIST) + [k for k in extra_keys if k not in ENV_ALLOWLIST]
    allow = {}
    for k in keys:
        v = child.get(k)
        if v is not None:
            allow[k] = v
    return {
        'allowlist': allow,
        'allowlistKeys': sorted(allow),
        'overrides': dict(overrides or {}),
        'keysTotal': len(child),
        'digest': None,  # 由 run_capture 的 envDigest 回填
    }


def business_block(rc, timed_out, started, expect_rc, expect_timeout):
    return {
        'pass': bool(started is True and type(rc) is int and rc == expect_rc
                     and bool(timed_out) is bool(expect_timeout)),
        'expectRc': expect_rc,
        'expectTimedOut': bool(expect_timeout),
        'observedRc': rc if type(rc) is int else None,
        'observedTimedOut': bool(timed_out),
        'started': started,
        'criterion': 'started is True and rc == expectRc and timedOut == expectTimedOut',
    }


def raw_block(result, case_dir):
    out = {}
    for key in ('stdout', 'stderr'):
        spec = result.get(key) or {}
        path = spec.get('path') or os.path.join(case_dir, '%s.raw' % key)
        out[key] = {'path': path, 'bytes': spec.get('bytes'), 'sha256': spec.get('sha256')}
    return out


def build_record(*, rec_id, requested_id, phase, argv, cwd, result, env, lock_info,
                 planned_path, case_dir, business, interrupted=None, recovered_from=None,
                 repaired_newline=False):
    """把一次执行的事实组装成台账行。**不补造时间**：取不到就写 None。"""
    rec = {
        'schema': schema.SCHEMA_VERSION,
        'id': rec_id,
        'idRequested': requested_id,
        'phase': phase,
        'recordOk': False,
        'recordReasons': [],
        'argv': list(argv),
        'cwd': cwd,
        'plannedPath': planned_path,
        'caseDir': case_dir,
        'resultPath': result.get('resultPath') if result else None,
        'business': business,
        'env': env,
        'lock': lock_info,
        'tool': {
            'name': 'record-command.py',
            'path': os.path.abspath(__file__),
            'schemaModule': os.path.join(HERE, 'evidence_schema.py'),
            'schemaVersion': schema.SCHEMA_VERSION,
            'procHelper': os.path.join(HERE, 'verify_examples_proc.py'),
            'procRawFormat': proc.RAW_FORMAT,
            'python': sys.executable,
            'pid': os.getpid(),
        },
        'pid': result.get('pid') if result else None,
        'pidCreationTimeUtc': result.get('pidCreationTimeUtc') if result else None,
    }
    if interrupted:
        rec['interrupted'] = interrupted
    if recovered_from:
        rec['recoveredFrom'] = recovered_from
    if repaired_newline:
        rec['jsonlRepairedNewline'] = True

    if phase == schema.PHASE_UNKNOWN:
        rec['launchOutcome'] = 'unknown'
        rec['started'] = None
        rec['rc'] = None
        rec['rcSource'] = None
        return rec

    for key in ('startedUtc', 'endedUtc', 'elapsedSec', 'started', 'rc', 'rcSource',
                'timedOut', 'stillRunning', 'containment', 'containmentReason',
                'refused', 'refusalReason', 'contained', 'activeAfter', 'activeKnown',
                'closeErrors', 'cleanupError', 'failClosed', 'restrictedSafe',
                'reapedSurvivors', 'infraOk', 'infraReasons', 'ok', 'okReasons'):
        rec[key] = result.get(key)
    rec['infraOk'] = result.get('ok')
    rec['infraReasons'] = result.get('okReasons')
    rec['raw'] = raw_block(result, case_dir)
    if env.get('digest') is None and result.get('envDigest'):
        env['digest'] = result['envDigest']
    rec['env'] = env
    return rec


def preflight_record_ok(rec):
    """落盘前的自证：这一行序列化后能被严格 parse，且结构/自洽/raw 都过关。"""
    line = json.dumps(rec, ensure_ascii=False, sort_keys=True, separators=(',', ':'))
    try:
        parsed = json.loads(line)
        err = None
    except ValueError as exc:
        parsed, err = None, 'pre-serialize-bad-json: %s' % exc
    return compute_record_ok(rec, parsed, err, written=None)


def compute_record_ok(rec, parsed_obj, parsed_error, written=None):
    """记录是否**可信**（与"执行是否完整"是两件事）。

    `written` 是**真正写进文件的那一版**快照：台账不可覆写，所以内存里随后补上的
    recordOk 不会回到文件里；比对必须用快照，否则会恒报"回读不一致"。
    """
    reasons = []
    if parsed_error:
        reasons.append('ledger-line-unparsable: %s' % parsed_error)
    if written is not None and parsed_obj is not None and parsed_obj != written:
        # 回读到的内容与写出内容不一致（极端 IO 情况），如实记下来。
        reasons.append('ledger-line-reread-mismatch')
    if rec.get('phase') == schema.PHASE_UNKNOWN:
        reasons.append('launch-outcome-unknown')
    _status, issues = schema.validate_record(rec)
    for issue in issues:
        if issue['severity'] == schema.SEVERITY_FAIL:
            reasons.append('contradiction:%s' % issue['code'])
    for issue in schema.validate_raw(rec):
        if issue['severity'] == schema.SEVERITY_FAIL:
            reasons.append('raw:%s' % issue['code'])
    return (reasons == []), reasons


# --------------------------------------------------------------------------- 信号


class InterruptState(object):
    def __init__(self):
        self.count = 0
        self.last = None
        self.names = []

    def install(self):
        for name in ('SIGINT', 'SIGTERM', 'SIGBREAK', 'SIGHUP'):
            signum = getattr(signal, name, None)
            if signum is None:
                continue
            try:
                signal.signal(signum, self._handler)
            except (ValueError, OSError, AttributeError):
                continue

    def _handler(self, signum, frame):
        self.count += 1
        self.last = signum
        self.names.append(signum)
        if self.count >= 2:
            # 二次中断：不再保证有界收口，交回默认行为（此时 finally/落盘不承诺执行）。
            try:
                signal.signal(signum, signal.SIG_DFL)
            except (ValueError, OSError):
                pass
            raise KeyboardInterrupt()
        sys.stderr.write('[record-command] 收到信号 %s：先让本次执行有界收口并落盘；'
                         '再中断一次将放弃收口（结果可能变成 unknown）。\n' % signum)
        sys.stderr.flush()


# --------------------------------------------------------------------------- run


def parse_env_pairs(pairs):
    overrides = {}
    for pair in pairs or []:
        if '=' not in pair:
            raise UsageError('--env 需要 K=V 形式：%r' % pair)
        k, v = pair.split('=', 1)
        if not k:
            raise UsageError('--env 的键不能为空：%r' % pair)
        overrides[k] = v
    return overrides


def resolve_argv(args):
    if args.args_file:
        if args.cmd:
            raise UsageError('--args-file 与命令行 argv 不能同时给')
        with open(args.args_file, 'r', encoding='utf-8') as fh:
            loaded = json.load(fh)
        if not isinstance(loaded, list) or not all(isinstance(x, str) for x in loaded):
            raise UsageError('--args-file 必须是 JSON 字符串数组')
        if not loaded:
            raise UsageError('--args-file 里的 argv 为空')
        return [str(x) for x in loaded]
    argv = list(args.cmd or [])
    if argv and argv[0] == '--':
        argv = argv[1:]
    if not argv:
        raise UsageError('缺少要记录的命令（用 `--` 之后的 argv，或 --args-file）')
    return argv


def resolve_lock(args, lockmod):
    """锁：**不默认暗中绕锁**。要用锁必须显式给 --lock-path 并挑明模式。"""
    mode = args.lock_mode or 'none'
    if mode != 'none' and not args.lock_path:
        raise UsageError('--lock-mode %s 必须同时给 --lock-path' % mode)
    if mode == 'none' and args.lock_path:
        raise UsageError('给了 --lock-path 却没给 --lock-mode：请显式挑明 acquire / external')
    if args.lock_token and mode != 'external':
        raise UsageError('--lock-token 仅用于 external 模式')
    info = {'mode': mode, 'path': args.lock_path, 'token': None, 'acquired': False,
            'released': None, 'releaseError': None, 'holder': None, 'agent': args.lock_agent,
            'task': args.lock_task, 'runId': args.lock_run_id}
    lock = lockmod.Lock(args.lock_path) if args.lock_path else None

    if mode == 'acquire':
        try:
            holder = lock.acquire(agent=args.lock_agent, task=args.lock_task,
                                  run_id=args.lock_run_id or None)
        except lockmod.LockHeldError as exc:
            info['busy'] = True
            info['holder'] = exc.holder
            info['staleCandidate'] = exc.stale_candidate
            return None, info, lock
        info['token'] = holder['token']
        info['acquired'] = True
        info['runId'] = holder['runId']
        return lock, info, lock

    if mode == 'external':
        # 外层说自己已经持锁：这里**只核对**，不碰锁文件。没有锁文件就是环境不符合声明。
        holder = lock.read()
        if not isinstance(holder, dict):
            raise UsageError('声明外层已持锁，但 %s 不存在（不能默认暗中运行）' % args.lock_path)
        if not args.lock_token or args.lock_token != holder.get('token'):
            raise UsageError('external 模式必须提供与外层持有者匹配的 --lock-token')
        info['holder'] = {'agent': holder.get('agent'), 'task': holder.get('task'),
                          'runId': holder.get('runId'), 'hostPid': holder.get('hostPid'),
                          'acquiredAt': holder.get('acquiredAt')}
        info['externalConfirmed'] = True
        return None, info, None
    return None, info, None


def cmd_run(args):
    argv = resolve_argv(args)
    if (not args.id or args.id in ('.', '..')
            or any(c in args.id for c in ('/', '\\', ':', '\x00'))):
        raise UsageError('--id 必须是单个目录名，不得为 . / .. 或含路径分隔符')
    lockmod = load_lock_helper()
    lock, lock_info, lock_obj = resolve_lock(args, lockmod)
    if lock_info.get('busy'):
        emit({'ok': False, 'exitCode': EXIT_LOCK_BUSY, 'lockBusy': True,
              'lockHolder': lock_info.get('holder'),
              'recorded': False, 'note': '公共锁被他人持有：本次没有发起任何子进程'},
             EXIT_LOCK_BUSY)
        return EXIT_LOCK_BUSY

    # 所有已取锁后的普通异常都必须归还本 token；硬 kill 不作 finally 保证。
    try:
        return _cmd_run_owned(args, argv, lock_info, lock_obj)
    finally:
        if lock_obj is not None and lock_obj.acquired:
            lock_info['released'] = bool(lock_obj.release())
            lock_info['releaseError'] = lock_obj.release_error or None


def _cmd_run_owned(args, argv, lock_info, lock_obj):

    jsonl = os.path.abspath(args.jsonl)
    if args.report and os.path.normcase(os.path.realpath(args.report)) == os.path.normcase(os.path.realpath(jsonl)):
        raise UsageError('--report 与 --jsonl 不得指向同一路径')
    if os.path.isdir(jsonl):
        raise UsageError('--jsonl 指向目录：%s' % jsonl)
    if args.report and os.path.lexists(args.report):
        raise UsageError('--report 路径已存在，拒绝覆盖：%s' % args.report)
    if not (math.isfinite(args.timeout) and args.timeout > 0):
        raise UsageError('--timeout 必须为正数：%r' % args.timeout)

    overrides = parse_env_pairs(args.env)
    if args.go_env:
        merged = dict(proc.GO_ENV_FIXED)
        merged.update(overrides)
        overrides = merged
    env = env_block(overrides, args.env_key or [])

    cwd = os.path.abspath(args.cwd) if args.cwd else os.getcwd()
    if not os.path.isdir(cwd):
        raise UsageError('--cwd 不是目录：%s' % cwd)

    requested_id = args.id
    rec_id = next_unique_id(jsonl, requested_id)
    run_dir = exclusive_dir(os.path.abspath(args.log_dir), rec_id)
    case_dir = os.path.join(run_dir, 'c1')

    planned = {
        'schema': schema.SCHEMA_VERSION,
        'phase': schema.PHASE_PREPARED,
        'id': rec_id,
        'idRequested': requested_id,
        'argv': argv,
        'argvSource': 'args-file' if args.args_file else 'cli',
        'cwd': cwd,
        'env': env,
        'lock': {k: v for k, v in lock_info.items() if k != 'holder'},
        'business': {'expectRc': args.expect_rc, 'expectTimedOut': bool(args.expect_timeout)},
        'timeoutSec': args.timeout,
        'containment': args.containment,
        'jsonl': jsonl,
        'runDir': run_dir,
        'plannedUtc': utc_now(),
        'note': 'prepared 只说明"打算跑什么"，不得据此声称零启动或已跑完',
    }
    planned_path = os.path.join(run_dir, 'planned.json')
    write_exclusive(planned_path, planned)

    istate = InterruptState()
    istate.install()

    result = None
    phase = schema.PHASE_COMPLETED
    recovered_from = None
    interrupted = None
    try:
        result = proc.run_capture(argv, 'c1', run_dir, cwd=cwd, timeout=args.timeout,
                                  env_overrides=overrides, containment=args.containment)
    except BaseException as exc:  # 硬中断：尽力从 result.json 恢复事实
        interrupted = {'signal': istate.last, 'signalCount': istate.count,
                       'exception': '%s: %s' % (type(exc).__name__, exc)}
        salvage = os.path.join(case_dir, 'result.json')
        try:
            with open(salvage, 'r', encoding='utf-8') as fh:
                loaded = json.load(fh)
            if is_final_result(loaded, planned, case_dir):
                result = loaded
                recovered_from = os.path.relpath(salvage, run_dir)
                phase = schema.PHASE_RECOVERED
            else:
                # 只找到 prepared 版：说明连"有没有跑起来"都不知道
                result = None
                phase = schema.PHASE_UNKNOWN
        except (OSError, ValueError):
            result = None
            phase = schema.PHASE_UNKNOWN
    else:
        if istate.count:
            interrupted = {'signal': istate.last, 'signalCount': istate.count,
                           'note': '软中断：本次执行已按有界收口完成并落盘'}

    if phase == schema.PHASE_UNKNOWN:
        business = {'pass': False, 'expectRc': args.expect_rc,
                    'expectTimedOut': bool(args.expect_timeout), 'observedRc': None,
                    'observedTimedOut': None, 'started': None,
                    'criterion': 'launch outcome unknown'}
        rec = build_record(rec_id=rec_id, requested_id=requested_id, phase=phase, argv=argv,
                           cwd=cwd, result=None, env=env, lock_info=lock_info,
                           planned_path=planned_path, case_dir=case_dir, business=business,
                           interrupted=interrupted)
    else:
        rc = result.get('rc')
        started = result.get('started')
        business = business_block(rc, result.get('timedOut'), started,
                                  args.expect_rc, bool(args.expect_timeout))
        rec = build_record(rec_id=rec_id, requested_id=requested_id, phase=phase, argv=argv,
                           cwd=cwd, result=result, env=env, lock_info=lock_info,
                           planned_path=planned_path, case_dir=case_dir, business=business,
                           interrupted=interrupted, recovered_from=recovered_from)

    # 台账**不可覆写**，所以可信度必须在写之前算完：先自证这一行序列化后能被严格
    # parse，且结构/自洽/raw 三项都过关；写完只做"回读复核"，发现问题也不能改行。
    rec['recordOk'], rec['recordReasons'] = preflight_record_ok(rec)
    written_snapshot = json.loads(json.dumps(rec, sort_keys=True))
    _line, repaired = append_jsonl(jsonl, rec)
    parsed, perr = reparse_last_line(jsonl)
    reread_ok = (perr is None and parsed == written_snapshot)
    if not reread_ok:
        rec['recordOk'] = False
        rec['recordReasons'] = list(rec['recordReasons']) + [
            'reread-mismatch: %s' % (perr or 'content differs')]
    verdict_path = os.path.join(run_dir, 'verdict.json')
    write_exclusive(verdict_path, {'id': rec_id, 'phase': rec['phase'],
                                   'recordOk': rec['recordOk'],
                                   'recordReasons': rec['recordReasons'],
                                   'business': rec['business'],
                                   'infraOk': rec.get('infraOk'),
                                   'jsonlRepairedNewline': bool(repaired),
                                   'rereadOk': bool(reread_ok),
                                   'reparseError': perr})

    exit_code = decide_exit(rec, lock_info)
    summary = {
        'ok': exit_code == EXIT_OK,
        'exitCode': exit_code,
        'id': rec_id,
        'phase': rec['phase'],
        'recordOk': rec['recordOk'],
        'recordReasons': rec['recordReasons'],
        'businessPass': rec['business'].get('pass'),
        'infraOk': rec.get('infraOk'),
        'rc': rec.get('rc'),
        'rcSource': rec.get('rcSource'),
        'timedOut': rec.get('timedOut'),
        'started': rec.get('started'),
        'pid': rec.get('pid'),
        'elapsedSec': rec.get('elapsedSec'),
        'caseDir': case_dir,
        'resultPath': rec.get('resultPath'),
        'plannedPath': planned_path,
        'verdictPath': verdict_path,
        'jsonl': jsonl,
        'lockReleased': lock_info.get('released'),
        'interrupted': interrupted,
    }

    # 锁：只有**自己取的**才释放，且只按 token。
    if lock_obj is not None and lock_info.get('acquired'):
        released = lock_obj.release()
        lock_info['released'] = bool(released)
        lock_info['releaseError'] = lock_obj.release_error or None
        if not released and exit_code == EXIT_OK:
            exit_code = EXIT_LOCK_NOT_RELEASED
            summary['ok'] = False
        summary['lockReleased'] = bool(released)
        summary['lockReleaseError'] = lock_obj.release_error or None
    summary['exitCode'] = exit_code
    if args.report:
        write_exclusive(args.report, dict(summary, record=rec))
    emit(summary, exit_code)
    return exit_code


def decide_exit(rec, lock_info):
    # 结果未知（硬终止/恢复不出来）排在最前：它**没有**"跑过"这回事，
    # 也不该被 recordOk=False 盖成"记录不完整"——两者含义不同。
    if rec['phase'] == schema.PHASE_UNKNOWN:
        return EXIT_NOT_STARTED
    if rec['recordOk'] is not True:
        return EXIT_RECORD_INCOMPLETE
    if rec.get('started') is not True:
        return EXIT_NOT_STARTED
    if rec.get('timedOut') is True and rec['business'].get('expectTimedOut') is not True:
        # 调用方**没有**声明"期望超时" → 超时就是最该报的事实。
        # 已经声明了（--expect-timeout）→ 让位给收口判定（6），调用方才看得到
        # "超时的同时收口也没被证明"，而不是被 5 这一个码盖住。
        return EXIT_TIMEOUT
    if rec['business'].get('pass') is not True:
        return EXIT_BUSINESS_FAIL
    if rec.get('infraOk') is not True:
        return EXIT_CONTAINMENT_UNKNOWN
    # 只有"取过锁且释放**明确失败**"才报 9；`released` 为 None（还没到释放那一步）
    # 不算失败，避免把顺序问题误报成锁泄漏。
    if lock_info.get('acquired') and lock_info.get('released') is False:
        return EXIT_LOCK_NOT_RELEASED
    return EXIT_OK


def emit(obj, rc):
    sys.stdout.write(json.dumps(obj, ensure_ascii=False, sort_keys=True) + '\n')
    sys.stdout.flush()
    return rc


# --------------------------------------------------------------------------- recover


def cmd_recover(args):
    if args.report and os.path.normcase(os.path.realpath(args.report)) == os.path.normcase(os.path.realpath(args.jsonl)):
        raise UsageError('--report 与 --jsonl 不得指向同一路径')
    if args.report and os.path.lexists(args.report):
        raise UsageError('--report 路径已存在，拒绝覆盖：%s' % args.report)
    run_dir = os.path.abspath(args.run_dir)
    planned_path = os.path.join(run_dir, 'planned.json')
    if not os.path.isfile(planned_path):
        raise UsageError('--run-dir 里没有 planned.json：%s' % run_dir)
    with open(planned_path, 'r', encoding='utf-8') as fh:
        planned = json.load(fh)

    jsonl = os.path.abspath(args.jsonl)
    existing = read_jsonl_ids(jsonl)
    base = planned.get('id') or os.path.basename(run_dir)
    if base in existing:
        emit({'ok': True, 'exitCode': EXIT_OK, 'skipped': True, 'id': base,
              'note': '台账里已有该 id，不重复记录（也不覆写）'}, EXIT_OK)
        return EXIT_OK
    rec_id = next_unique_id(jsonl, base)

    # 事实只认 run_capture 自己写的 result.json；拿不到就写 unknown，**不补造时间**。
    # 本入口只会启动 c1；不能扫描并认领碰巧存在的其它 case。
    expected_case_dir = os.path.join(run_dir, 'c1')
    candidate = os.path.join(expected_case_dir, 'result.json')
    result_path = candidate if os.path.isfile(candidate) else None
    result = None
    if result_path:
        try:
            with open(result_path, 'r', encoding='utf-8') as fh:
                loaded = json.load(fh)
            if is_final_result(loaded, planned, expected_case_dir):
                result = loaded
        except (OSError, ValueError):
            result = None

    env = planned.get('env') or {}
    lock_info = planned.get('lock') or {'mode': 'none'}
    business_spec = planned.get('business') or {}
    expect_rc = business_spec.get('expectRc', 0)
    expect_timeout = bool(business_spec.get('expectTimedOut'))

    if result is None:
        phase = schema.PHASE_UNKNOWN
        business = {'pass': False, 'expectRc': expect_rc, 'expectTimedOut': expect_timeout,
                    'observedRc': None, 'observedTimedOut': None, 'started': None,
                    'criterion': 'launch outcome unknown'}
        rec = build_record(rec_id=rec_id, requested_id=base, phase=phase,
                           argv=planned.get('argv') or [], cwd=planned.get('cwd') or os.getcwd(),
                           result=None, env=env, lock_info=lock_info,
                           planned_path=planned_path, case_dir=run_dir, business=business,
                           recovered_from=None)
    else:
        case_dir = result.get('caseDir') or os.path.dirname(result_path)
        # 保留捕获时的 bytes/SHA；validate_raw 必须把当前字节与历史承诺比较。
        # 重新计算并覆盖承诺会把事后篡改重新认证成成功证据。
        business = business_block(result.get('rc'), result.get('timedOut'),
                                  result.get('started'), expect_rc, expect_timeout)
        rec = build_record(rec_id=rec_id, requested_id=base, phase=schema.PHASE_RECOVERED,
                           argv=result.get('argv') or planned.get('argv') or [],
                           cwd=result.get('cwd') or planned.get('cwd') or os.getcwd(),
                           result=result, env=env, lock_info=lock_info,
                           planned_path=planned_path, case_dir=case_dir, business=business,
                           recovered_from=os.path.relpath(result_path, run_dir))

    rec['recordOk'], rec['recordReasons'] = preflight_record_ok(rec)
    written_snapshot = json.loads(json.dumps(rec, sort_keys=True))
    _line, repaired = append_jsonl(jsonl, rec)
    parsed, perr = reparse_last_line(jsonl)
    reread_ok = (perr is None and parsed == written_snapshot)
    if not reread_ok:
        rec['recordOk'] = False
        rec['recordReasons'] = list(rec['recordReasons']) + [
            'reread-mismatch: %s' % (perr or 'content differs')]
    verdict_path = os.path.join(run_dir, 'verdict-recovered.json')
    if not os.path.exists(verdict_path):
        write_exclusive(verdict_path, {'id': rec_id, 'phase': rec['phase'],
                                       'recordOk': rec['recordOk'],
                                       'recordReasons': rec['recordReasons'],
                                       'business': rec['business'],
                                       'infraOk': rec.get('infraOk'),
                                       'jsonlRepairedNewline': bool(repaired),
                                       'reparseError': perr})
    lock_info = {'mode': (lock_info or {}).get('mode', 'none'), 'acquired': False,
                 'released': None}
    exit_code = decide_exit(rec, lock_info)
    summary = {'ok': exit_code == EXIT_OK, 'exitCode': exit_code, 'id': rec_id,
               'phase': rec['phase'], 'recordOk': rec['recordOk'],
               'recordReasons': rec['recordReasons'],
               'businessPass': rec['business'].get('pass'), 'infraOk': rec.get('infraOk'),
               'rc': rec.get('rc'), 'rcSource': rec.get('rcSource'),
               'started': rec.get('started'), 'caseDir': rec.get('caseDir'),
               'plannedPath': planned_path, 'jsonl': jsonl, 'recovered': True}
    if args.report:
        write_exclusive(args.report, dict(summary, record=rec))
    emit(summary, exit_code)
    return exit_code


# --------------------------------------------------------------------------- CLI


def build_parser():
    ap = argparse.ArgumentParser(prog='record-command.py',
                                 description='随仓命令记录入口（复用 R6 进程收口/raw 捕获）')
    sub = ap.add_subparsers(dest='action')

    run = sub.add_parser('run', help='跑一条命令并落一行台账')
    run.add_argument('--id', default='run', help='台账 id（已存在则顺延 -2/-3）')
    run.add_argument('--jsonl', required=True, help='台账 JSONL 路径（只追加，不覆盖）')
    run.add_argument('--log-dir', required=True, help='独占输出根目录')
    run.add_argument('--report', default=None, help='本次判定 JSON（路径已存在即报错）')
    run.add_argument('--cwd', default=None)
    run.add_argument('--timeout', type=float, default=1800.0)
    run.add_argument('--expect-rc', type=int, default=0, help='业务判据期望退出码')
    run.add_argument('--expect-timeout', action='store_true',
                     help='业务判据期望"超时"（与 --expect-rc 一起判定）')
    run.add_argument('--env', action='append', default=[], help='子进程局部环境 K=V（可重复）')
    run.add_argument('--env-key', action='append', default=[], help='额外记录的环境键（只读）')
    run.add_argument('--go-env', action='store_true', help='并入 Go 六项固定环境')
    run.add_argument('--containment', default='auto', choices=('auto', 'win-job', 'none'))
    run.add_argument('--lock-path', default=None)
    run.add_argument('--lock-mode', default=None, choices=('acquire', 'external'))
    run.add_argument('--lock-token', default=None,
                     help='external 模式的外层持有者 token（只校验，不放入子进程环境）')
    run.add_argument('--lock-agent', default='workbuddy')
    run.add_argument('--lock-task', default='record-command')
    run.add_argument('--lock-run-id', default=None)
    run.add_argument('--args-file', default=None, help='argv 的 JSON 数组文件（跨宿主保真）')
    run.add_argument('cmd', nargs=argparse.REMAINDER, help='`--` 之后的真实 argv')

    rc = sub.add_parser('recover', help='从 planned.json/result.json 恢复一条台账行')
    rc.add_argument('--run-dir', required=True)
    rc.add_argument('--jsonl', required=True)
    rc.add_argument('--report', default=None)
    return ap


def main(argv):
    ap = build_parser()
    args = ap.parse_args(argv)
    if not args.action:
        ap.print_usage(sys.stderr)
        return EXIT_USAGE
    try:
        if args.action == 'run':
            return cmd_run(args)
        return cmd_recover(args)
    except UsageError as exc:
        sys.stderr.write('usage error: %s\n' % exc.message)
        return EXIT_USAGE
    except FileExistsError as exc:
        sys.stderr.write('output path exists, refusing to overwrite: %s\n' % exc)
        return EXIT_USAGE


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))

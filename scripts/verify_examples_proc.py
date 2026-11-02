# -*- coding: utf-8 -*-
r"""R6-C 私有**完整 raw 捕获**与结果落盘（只依赖标准库，随仓提供）。

本模块解决 R5 非阻断 P2 的一半：选择器每个 case 只留 `outTail`（尾部 1200 字符），
失败后无法复核真实输出。这里把每个 case 变成**可复核证据**：

  * `stdout.raw` / `stderr.raw` 按**原始字节**落盘，保留 CRLF、NUL、非 UTF-8、超时部分输出；
  * SHA-256 对**落盘字节**计算；解码只用于 `displayTail`（人读），不改写 raw；
  * 每个 case 一个**独占目录**，已存在则顺延 `-2`/`-3`，**绝不覆盖历史字节**；
  * `result.json` 记录真实 argv / cwd / 起止 / rc（或 null）+ raw 路径 / 字节 / SHA；
  * 没有真正启动时 pid 与 rc 一律为空（不伪造）。

进程整树收口在 `verify_examples_proctree.py`（Windows Job Object）。本模块在**能力不可用**
时 fail-closed：`failClosed=True` + `error` 说明原因，不静默退化成"只杀父 PID"。

跨平台：非 Windows 不支持整树收口（`tree_available()` 返回 False 并给出原因）；
显式 `containment='none'` 时走便携路径（超时只终止直接子进程），并在结果里标明。
"""
import datetime
import hashlib
import importlib.util
import json
import os
import subprocess
import sys
import time

RAW_FORMAT = 'r6c-2'

CONTAINMENT_WIN_JOB = 'win-job'
CONTAINMENT_NONE = 'none'
CONTAINMENT_REFUSED = 'refused'

GO_ENV_FIXED = {
    'CGO_ENABLED': '0',
    'GOMAXPROCS': '2',
    'GOPROXY': 'off',
    'GOSUMDB': 'off',
    'GOTOOLCHAIN': 'local',
    'GOFLAGS': '-mod=readonly',
}


def compute_ok(result):
    """基础设施成功的**唯一**判据；返回 (ok, reasons)。

    复核缺口（协调者 2026-11-02 快照，逐条独立毒化验证过）：
      * `closeErrors` 非空但 `activeAfter==0` 曾被放绿；
      * `activeAfter is None`（活动数未知）曾被 `in (0, None)` 放绿；
      * `activeAfter is False` 因 `False == 0` 被放绿；
      * `contained is False`（归属未证）根本没被消费。
    因此这里对每个字段**单独**要求，且要求**类型**：
      contained 必须**是** True；activeAfter 必须**是** int 且等于 0；
      stillRunning 必须**是** False（未知 None 不算已退出）；closeErrors 必须是空 list。

    注意口径：`ok` 只表示"基础设施可信、进程树已收口"，**不是**业务成功。
    子进程非零退出（如 rc=7）时 `ok` 仍为 True——上层选择器必须自己核 rc/timedOut。
    """
    reasons = []
    if result.get('refused') is True:
        reasons.append('refused:%s' % result.get('refusalReason'))
    if result.get('started') is not True:
        reasons.append('not-started')
    if result.get('rc') is None:
        reasons.append('rc-unobserved')
    if result.get('failClosed') is not False:
        reasons.append('fail-closed')
    if result.get('stillRunning') is not False:
        reasons.append('exit-unknown-or-running')
    if result.get('timedOut') is not False:
        # 超时后那个 rc 是**我们**给的（forced-on-timeout），不是进程自己的退出码。
        # 它不能算"基础设施 ok"，否则上层可以把 rc=8 当成被测程序的真实退出码。
        reasons.append('timed-out')
    if result.get('cleanupError') is not None:
        reasons.append('cleanup-error')
    if result.get('contained') is not True:
        reasons.append('not-contained')
    active = result.get('activeAfter')
    if not (type(active) is int and active == 0):
        reasons.append('active-after-not-confirmed-zero')
    if result.get('activeKnown') is False:
        reasons.append('active-count-query-failed')
    if result.get('closeErrors') != []:
        reasons.append('close-errors')
    if result.get('restrictedSafe') is False:
        reasons.append('not-restricted-safe')
    return (reasons == []), reasons


def _resolve_containment(requested):
    """返回 (mode, reason)。**不支持就在启动前拒绝**，绝不先跑再标不可靠。

    `auto` 只走能力可用的整树收口；非 Windows 或缺 Job Object 时返回 refused，
    调用方据此**一次 Popen / CreateProcess 都不发**。
    `none` 是显式声明"本次不要收口"（仅供非受限的自检用），会被标 `restrictedSafe=False`，
    受限选择器不得使用。
    """
    if requested == CONTAINMENT_NONE:
        return CONTAINMENT_NONE, 'explicit-no-containment'
    if requested not in ('auto', CONTAINMENT_WIN_JOB):
        return CONTAINMENT_REFUSED, 'unknown-containment:%r' % (requested,)
    if os.name != 'nt':
        return CONTAINMENT_REFUSED, 'unsupported-platform: process-tree containment is Windows-only (os.name=%r)' % os.name
    mod, reason = _load_tree()
    if mod is None:
        return CONTAINMENT_REFUSED, 'tree-unavailable: %s' % reason
    return CONTAINMENT_WIN_JOB, reason

_TREE_MODULE = None
_TREE_ERROR = None


def _load_tree():
    global _TREE_MODULE, _TREE_ERROR
    if _TREE_MODULE is not None or _TREE_ERROR is not None:
        return _TREE_MODULE, _TREE_ERROR
    if os.name != 'nt':
        _TREE_ERROR = 'process-tree containment is Windows-only (os.name=%r)' % os.name
        return None, _TREE_ERROR
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                        'verify_examples_proctree.py')
    try:
        spec = importlib.util.spec_from_file_location('verify_examples_proctree', path)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        ok, reason = mod.available()
        if not ok:
            _TREE_ERROR = reason
            return None, reason
        _TREE_MODULE = mod
        return mod, 'job-object'
    except Exception as exc:  # 缺文件 / ctypes 异常都算"能力不可用"
        _TREE_ERROR = '%s: %s' % (type(exc).__name__, exc)
        return None, _TREE_ERROR


def tree_available():
    """(bool, reason)：整树收口能不能用。调用方应把 reason 写进报告。"""
    mod, reason = _load_tree()
    return (mod is not None), reason


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds')


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def decode_for_display(raw, limit=1200):
    """只用于人读的尾部展示；**不**用于判据，也**不**回写 raw。

    混合编码是既定事实：PS 脚本自己的 Write-Host 走控制台代码页（简体中文环境 cp936），
    而 go / goxml 子进程的原样输出是 UTF-8。这里逐级回退，失败也不抛。
    """
    text = None
    enc = None
    for cand in ('utf-8', 'mbcs', 'latin-1'):
        try:
            text = raw.decode(cand)
            enc = cand
            break
        except (UnicodeDecodeError, LookupError):
            continue
    if text is None:
        text, enc = raw.decode('utf-8', 'replace'), 'replace'
    return {'encoding': enc, 'tail': text[-limit:], 'decodedChars': len(text)}


def _unique_case_dir(log_dir, case_id):
    """独占目录：`<log_dir>/<case_id>`；已存在则顺延 `-2`/`-3`。绝不覆盖历史字节。"""
    base = os.path.abspath(log_dir)
    os.makedirs(base, exist_ok=True)
    for n in range(1, 1000):
        name = case_id if n == 1 else '%s-%d' % (case_id, n)
        path = os.path.join(base, name)
        try:
            os.makedirs(path, exist_ok=False)
            return path, name
        except FileExistsError:
            continue
    raise RuntimeError('无法为 case %s 分配独占目录（已有 999 个）' % case_id)


def _child_env(overrides):
    """构造子进程局部环境：父环境不变（复制后改副本），None 表示删除该键。"""
    env = dict(os.environ)
    for k, v in (overrides or {}).items():
        if v is None:
            env.pop(k, None)
        else:
            env[k] = str(v)
    return env


def _write_result(case_dir, result):
    path = os.path.join(case_dir, 'result.json')
    with open(path, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(result, fh, ensure_ascii=False, indent=2, sort_keys=True)
    return path


def _hash_streams(case_dir, names=('stdout.raw', 'stderr.raw')):
    out = {}
    for name in names:
        p = os.path.join(case_dir, name)
        if os.path.isfile(p):
            out[name] = {'path': p, 'bytes': os.path.getsize(p), 'sha256': sha256_file(p)}
        else:
            out[name] = {'path': p, 'bytes': 0, 'sha256': None, 'missing': True}
    return out


def run_capture(argv, case_id, log_dir, cwd=None, timeout=1800.0, env_overrides=None,
                containment='auto', fault=None):
    """跑一条命令，留下**完整可复核**的结果（同时写 `<case_dir>/result.json`）。

    返回字段（判据都在这里，不在日志文本里）：

      caseId / caseDir / argv / cwd / envDigest / startedUtc / endedUtc / elapsedSec
      pid / pidCreationTimeUtc / started / rc / rcSource / timedOut / stillRunning
      containment / contained / failClosed / assignError / cleanupError / activeAfter
      reapedSurvivors / closeErrors / stdout{path,bytes,sha256} / stderr{...} / displayTail
      ok  ← 真的启动 + 拿到退出码 + 未 fail-closed + 无清理错误 + 树内已无活动进程

    `fault` 只用于**安全故障注入**（自检与上层消费点验证用）：
      {'assign_fail': True} 归属失败；{'terminate_fail': True} 清理失败。
    """
    argv = [str(a) for a in argv]
    case_dir, case_name = _unique_case_dir(log_dir, case_id)
    out_path = os.path.join(case_dir, 'stdout.raw')
    err_path = os.path.join(case_dir, 'stderr.raw')
    env = _child_env(env_overrides)
    env_digest = hashlib.sha256(
        json.dumps({k: env[k] for k in sorted(env)}, ensure_ascii=False,
                   sort_keys=True).encode('utf-8')
    ).hexdigest()

    result = {
        'format': RAW_FORMAT,
        'caseId': case_name,
        'caseDir': case_dir,
        'argv': argv,
        'cwd': os.path.abspath(cwd) if cwd else os.getcwd(),
        'envDigest': env_digest,
        'envKeys': len(env),
        'timeoutSec': timeout,
        'startedUtc': utc(),
        'endedUtc': None,
        'elapsedSec': None,
        'pid': None,
        'pidCreationTimeUtc': None,
        'started': False,
        'rc': None,
        'rcSource': None,
        'timedOut': False,
        'stillRunning': False,
        'containment': CONTAINMENT_NONE,
        'containmentReason': None,
        'refused': False,
        'refusalReason': None,
        'restrictedSafe': True,
        'contained': False,
        'activeKnown': None,
        'failClosed': False,
        'assignError': None,
        'cleanupError': None,
        'activeAfter': None,
        'reapedSurvivors': False,
        'closeErrors': [],
        'error': None,
        'ok': False,
        'okReasons': None,
    }

    # 先把"打算跑什么"落盘：即使随后异常，也能看到真实 argv/cwd/环境指纹。
    _write_result(case_dir, dict(result, phase='prepared'))

    t0 = time.monotonic()
    try:
        mode, reason = _resolve_containment(containment)
        result['containment'] = mode
        result['containmentReason'] = reason
        result['refused'] = (mode == CONTAINMENT_REFUSED)
        if mode == CONTAINMENT_REFUSED:
            # 关键：能力不足时**启动前拒绝**，不产生任何子进程（Popen/CreateProcess 调用数 0）。
            result['refusalReason'] = reason
            result['failClosed'] = True
            result['error'] = 'refused before launch: %s' % reason
        elif mode == CONTAINMENT_WIN_JOB:
            mod, why = _load_tree()
            if mod is None:  # 与 _resolve_containment 竞态兜底
                result['failClosed'] = True
                result['error'] = 'process-tree containment unavailable: %s' % why
            else:
                mod.run(result, argv, result['cwd'], env, out_path, err_path, timeout, fault)
        else:
            _run_portable(result, argv, result['cwd'], env, out_path, err_path, timeout)
    except Exception as exc:  # 未预料的异常也不丢证据
        result['error'] = '%s: %s' % (type(exc).__name__, exc)
        result['failClosed'] = True
    finally:
        result['elapsedSec'] = round(time.monotonic() - t0, 3)
        result['endedUtc'] = utc()
        streams = _hash_streams(case_dir)
        result['stdout'], result['stderr'] = streams['stdout.raw'], streams['stderr.raw']
        merged = b''
        for p in (out_path, err_path):
            if os.path.isfile(p):
                with open(p, 'rb') as fh:
                    merged += fh.read()
        result['displayTail'] = decode_for_display(merged)
        result['ok'], result['okReasons'] = compute_ok(result)
        result['resultPath'] = _write_result(case_dir, result)

    return result


def _run_portable(result, argv, cwd, env, out_path, err_path, timeout):
    """便携路径：直写文件（超时也能保住部分输出），但**不做整树收口**。

    只允许显式 `containment='none'` 进入（`auto` 永远不会走到这里）。
    结果里标 `restrictedSafe=False`，`compute_ok` 必定拒绝——受限选择器不得使用。
    """
    result['containment'] = CONTAINMENT_NONE
    result['restrictedSafe'] = False
    result['failClosed'] = True
    result['contained'] = False
    result['activeKnown'] = False
    result['error'] = 'no process-tree containment on this platform (fail-closed)'
    with open(out_path, 'wb') as so, open(err_path, 'wb') as se:
        proc = subprocess.Popen(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=so, stderr=se, close_fds=True)
        result['started'] = True
        result['pid'] = proc.pid
        try:
            proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            result['timedOut'] = True
            proc.kill()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                result['stillRunning'] = True
        if result['stillRunning']:
            result['rc'] = None
            result['rcSource'] = 'not-exited'
        else:
            result['rc'] = proc.returncode
            result['rcSource'] = 'forced-on-timeout' if result['timedOut'] else 'observed'

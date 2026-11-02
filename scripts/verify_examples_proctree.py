# -*- coding: utf-8 -*-
r"""R6-C 私有 **Windows 进程树收口**（Job Object，只依赖标准库 + ctypes）。

职责单一：给定 argv/env/输出文件，把进程**整树**跑完并收口，结果写回调用方给的 dict。
捕获/落盘/结果 JSON 由 `verify_examples_proc.py` 负责，本文件只管进程生命周期。

三条硬约束（对应任务书片 2）：

  1. **创建与归属之间没有逃逸窗口**：`CREATE_SUSPENDED` 建进程 → `AssignProcessToJobObject`
     → `ResumeThread`。归属失败时**绝不 resume**，改用自持 handle `TerminateProcess`
     并在有界时间内 `WaitForSingleObject` 确认，避免"挂起的孤儿"。
  2. **父退孙活也要收口**：`WaitForSingleObject(进程句柄)` 返回只代表父进程结束；
     之后必须查 `ActiveProcesses`，非 0 就 `TerminateJobObject` 并有界等到 0。
  3. **不静默退化**：任何关键调用失败都置 `failClosed=True` 并记录错误码；
     清理失败写 `cleanupError`，由上层（选择器）给出非零退出，不报成功。

只处理**本次自建**进程：不 `taskkill /IM`、不按镜像名杀、不碰他人 PID 或锁。
非 Windows 平台：`available()` 返回 False，调用方据此 fail-closed。
"""
import ctypes
import os
import subprocess
import time

from ctypes import wintypes as _wt

JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000
JOB_OBJECT_EXTENDED_LIMIT_INFORMATION = 9
JOB_OBJECT_BASIC_ACCOUNTING_INFORMATION = 1
CREATE_SUSPENDED = 0x00000004
CREATE_UNICODE_ENVIRONMENT = 0x00000400
CREATE_NO_WINDOW = 0x08000000
STARTF_USESTDHANDLES = 0x00000100
HANDLE_FLAG_INHERIT = 0x00000001
WAIT_OBJECT_0 = 0x00000000
WAIT_TIMEOUT = 0x00000102
WAIT_FAILED = 0xFFFFFFFF
STILL_ACTIVE = 259
FILE_ATTRIBUTE_NORMAL = 0x00000080
GENERIC_WRITE = 0x40000000
OPEN_ALWAYS = 4
INVALID_HANDLE_VALUE = ctypes.c_void_p(-1).value

# 有界等待参数（毫秒）——固定值，避免"等一会儿再说"式的无限期挂起。
ASSIGN_FAIL_TERMINATE_WAIT_MS = 10000
REAP_DEADLINE_SEC = 10.0
POST_TERMINATE_WAIT_MS = 5000
# 父进程退出后，Job 记账到 0 可能有几毫秒滞后（本轮实测 ok-quick 立刻读到 1）。
# 先有界等归零，再判断"是否真有活口"，否则干净进程也会被当成 survivor。
SETTLE_DEADLINE_SEC = 2.0
SETTLE_STEP_SEC = 0.02


def available():
    """返回 (bool, reason)。非 Windows 明确不支持，不冒称已实现。"""
    if os.name != 'nt':
        return False, 'process-tree containment is Windows-only (os.name=%r)' % os.name
    try:
        _ = _k32
    except Exception as exc:  # pragma: no cover - 仅在异常环境下触发
        return False, 'kernel32 unavailable: %s: %s' % (type(exc).__name__, exc)
    return True, 'job-object'


class STARTUPINFOW(ctypes.Structure):
    _fields_ = [('cb', _wt.DWORD), ('lpReserved', _wt.LPWSTR), ('lpDesktop', _wt.LPWSTR),
                ('lpTitle', _wt.LPWSTR), ('dwX', _wt.DWORD), ('dwY', _wt.DWORD),
                ('dwXSize', _wt.DWORD), ('dwYSize', _wt.DWORD),
                ('dwXCountChars', _wt.DWORD), ('dwYCountChars', _wt.DWORD),
                ('dwFillAttribute', _wt.DWORD), ('dwFlags', _wt.DWORD),
                ('wShowWindow', _wt.WORD), ('cbReserved2', _wt.WORD),
                ('lpReserved2', ctypes.POINTER(ctypes.c_byte)),
                ('hStdInput', _wt.HANDLE), ('hStdOutput', _wt.HANDLE), ('hStdError', _wt.HANDLE)]


class PROCESS_INFORMATION(ctypes.Structure):
    _fields_ = [('hProcess', _wt.HANDLE), ('hThread', _wt.HANDLE),
                ('dwProcessId', _wt.DWORD), ('dwThreadId', _wt.DWORD)]


class JOBOBJECT_BASIC_LIMIT_INFORMATION(ctypes.Structure):
    _fields_ = [('PerProcessUserTimeLimit', ctypes.c_int64),
                ('PerJobUserTimeLimit', ctypes.c_int64),
                ('LimitFlags', _wt.DWORD),
                ('MinimumWorkingSetSize', ctypes.c_size_t),
                ('MaximumWorkingSetSize', ctypes.c_size_t),
                ('ActiveProcessLimit', _wt.DWORD),
                ('Affinity', ctypes.c_size_t),
                ('PriorityClass', _wt.DWORD),
                ('SchedulingClass', _wt.DWORD)]


class IO_COUNTERS(ctypes.Structure):
    _fields_ = [('ReadOperationCount', ctypes.c_uint64),
                ('WriteOperationCount', ctypes.c_uint64),
                ('OtherOperationCount', ctypes.c_uint64),
                ('ReadTransferCount', ctypes.c_uint64),
                ('WriteTransferCount', ctypes.c_uint64),
                ('OtherTransferCount', ctypes.c_uint64)]


class JOBOBJECT_EXTENDED_LIMIT_INFORMATION(ctypes.Structure):
    _fields_ = [('BasicLimitInformation', JOBOBJECT_BASIC_LIMIT_INFORMATION),
                ('IoInfo', IO_COUNTERS),
                ('ProcessMemoryLimit', ctypes.c_size_t),
                ('JobMemoryLimit', ctypes.c_size_t),
                ('PeakProcessMemoryUsed', ctypes.c_size_t),
                ('PeakJobMemoryUsed', ctypes.c_size_t)]


class JOBOBJECT_BASIC_ACCOUNTING_INFORMATION(ctypes.Structure):
    _fields_ = [('TotalUserTime', ctypes.c_int64), ('TotalKernelTime', ctypes.c_int64),
                ('ThisPeriodTotalUserTime', ctypes.c_int64),
                ('ThisPeriodTotalKernelTime', ctypes.c_int64),
                ('TotalPageFaultCount', _wt.DWORD), ('TotalProcesses', _wt.DWORD),
                ('ActiveProcesses', _wt.DWORD), ('TotalTerminatedProcesses', _wt.DWORD)]


class SECURITY_ATTRIBUTES(ctypes.Structure):
    _fields_ = [('nLength', _wt.DWORD), ('lpSecurityDescriptor', _wt.LPVOID),
                ('bInheritHandle', _wt.BOOL)]


class FILETIME(ctypes.Structure):
    _fields_ = [('dwLowDateTime', _wt.DWORD), ('dwHighDateTime', _wt.DWORD)]


def _load_k32():
    k = ctypes.WinDLL('kernel32', use_last_error=True)
    k.CreateJobObjectW.restype = _wt.HANDLE
    k.CreateJobObjectW.argtypes = [_wt.LPVOID, _wt.LPCWSTR]
    k.SetInformationJobObject.restype = _wt.BOOL
    k.SetInformationJobObject.argtypes = [_wt.HANDLE, ctypes.c_int, _wt.LPVOID, _wt.DWORD]
    k.QueryInformationJobObject.restype = _wt.BOOL
    k.QueryInformationJobObject.argtypes = [_wt.HANDLE, ctypes.c_int, _wt.LPVOID, _wt.DWORD,
                                            ctypes.POINTER(_wt.DWORD)]
    k.AssignProcessToJobObject.restype = _wt.BOOL
    k.AssignProcessToJobObject.argtypes = [_wt.HANDLE, _wt.HANDLE]
    k.TerminateJobObject.restype = _wt.BOOL
    k.TerminateJobObject.argtypes = [_wt.HANDLE, _wt.UINT]
    k.CreateProcessW.restype = _wt.BOOL
    k.CreateProcessW.argtypes = [_wt.LPCWSTR, _wt.LPWSTR, _wt.LPVOID, _wt.LPVOID, _wt.BOOL,
                                 _wt.DWORD, _wt.LPVOID, _wt.LPCWSTR,
                                 ctypes.POINTER(STARTUPINFOW), ctypes.POINTER(PROCESS_INFORMATION)]
    k.ResumeThread.restype = _wt.DWORD
    k.ResumeThread.argtypes = [_wt.HANDLE]
    k.WaitForSingleObject.restype = _wt.DWORD
    k.WaitForSingleObject.argtypes = [_wt.HANDLE, _wt.DWORD]
    k.GetExitCodeProcess.restype = _wt.BOOL
    k.GetExitCodeProcess.argtypes = [_wt.HANDLE, ctypes.POINTER(_wt.DWORD)]
    k.TerminateProcess.restype = _wt.BOOL
    k.TerminateProcess.argtypes = [_wt.HANDLE, _wt.UINT]
    k.CloseHandle.restype = _wt.BOOL
    k.CloseHandle.argtypes = [_wt.HANDLE]
    k.CreateFileW.restype = _wt.HANDLE
    k.CreateFileW.argtypes = [_wt.LPCWSTR, _wt.DWORD, _wt.DWORD, _wt.LPVOID, _wt.DWORD,
                              _wt.DWORD, _wt.HANDLE]
    k.SetHandleInformation.restype = _wt.BOOL
    k.SetHandleInformation.argtypes = [_wt.HANDLE, _wt.DWORD, _wt.DWORD]
    k.GetProcessTimes.restype = _wt.BOOL
    k.GetProcessTimes.argtypes = [_wt.HANDLE, ctypes.POINTER(FILETIME), ctypes.POINTER(FILETIME),
                                  ctypes.POINTER(FILETIME), ctypes.POINTER(FILETIME)]
    return k


_k32 = _load_k32() if os.name == 'nt' else None


def filetime_to_iso(high, low):
    ticks = (int(high) << 32) | int(low)
    if ticks == 0:
        return None
    import datetime
    return (datetime.datetime(1601, 1, 1, tzinfo=datetime.timezone.utc)
            + datetime.timedelta(microseconds=ticks / 10.0)).isoformat(timespec='microseconds')


def _open_out(path):
    """以**可继承**句柄打开输出文件：子进程直接写文件，没有管道死锁与丢字节风险。"""
    sa = SECURITY_ATTRIBUTES(ctypes.sizeof(SECURITY_ATTRIBUTES), None, True)
    h = _k32.CreateFileW(path, GENERIC_WRITE, 0x00000001 | 0x00000002,  # share read|write
                         ctypes.byref(sa), OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, None)
    if not h or h == INVALID_HANDLE_VALUE:
        raise OSError('CreateFileW(%s) failed: %d' % (path, ctypes.get_last_error()))
    _k32.SetHandleInformation(h, HANDLE_FLAG_INHERIT, HANDLE_FLAG_INHERIT)
    return h


def _open_null():
    """stdin 也必须是**可继承**的：否则 venv 的 python.exe 启动器会往子进程 stderr 写
    `warning: Making stdin inheritable failed`，无端污染 raw（本轮实测踩到）。"""
    sa = SECURITY_ATTRIBUTES(ctypes.sizeof(SECURITY_ATTRIBUTES), None, True)
    h = _k32.CreateFileW('NUL', 0x80000000, 0x00000001 | 0x00000002,
                         ctypes.byref(sa), OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, None)
    if h and h != INVALID_HANDLE_VALUE:
        _k32.SetHandleInformation(h, HANDLE_FLAG_INHERIT, HANDLE_FLAG_INHERIT)
    return h


def _active(job):
    """返回活动进程数，**查询失败返回 None（未知）**。

    调用方绝不能把 None 当 0：`(active or 0) > 0` 这种写法会把"看不见"当成"没有"，
    于是父退孙活也判成收口完成（协调者复核点名之一）。
    """
    info = JOBOBJECT_BASIC_ACCOUNTING_INFORMATION()
    got = _wt.DWORD(0)
    if not _k32.QueryInformationJobObject(job, JOB_OBJECT_BASIC_ACCOUNTING_INFORMATION,
                                          ctypes.byref(info), ctypes.sizeof(info),
                                          ctypes.byref(got)):
        return None
    return int(info.ActiveProcesses)


def _settle(job, deadline_sec=SETTLE_DEADLINE_SEC):
    """有界等到 Job 活动数归零，返回 (value, waited_ms)。value=None 表示查询失败。

    父进程刚退出时活动数可能仍读到 1（记账滞后），直接判"还有活口"会把干净进程
    误标成 survivor，也会白跑一次整树终止。这里先给一个有界窗口再下结论。
    """
    t0 = time.monotonic()
    last = None
    while True:
        last = _active(job)
        if last == 0:
            break
        if time.monotonic() - t0 >= deadline_sec:
            break
        time.sleep(SETTLE_STEP_SEC)
    return last, int((time.monotonic() - t0) * 1000)


def classify_wait(code):
    """把 WaitForSingleObject 的返回值分类；**不**把非 timeout 一律当"已退出"。"""
    if code == WAIT_OBJECT_0:
        return 'object0'
    if code == WAIT_TIMEOUT:
        return 'timeout'
    if code == WAIT_FAILED:
        return 'failed'
    return 'other-0x%08x' % (code & 0xFFFFFFFF)


def validate_timeout(timeout):
    """返回 (ok, ms, reason)。`0` / 负数 / 非有限值一律**启动前拒绝**。

    旧实现把 0 变成 `0xFFFFFFFF`（INFINITE），等于"永远等下去"，与"有界超时"直接冲突。
    """
    if isinstance(timeout, bool):
        return False, None, 'timeout must be numeric, not bool'
    try:
        val = float(timeout)
    except (TypeError, ValueError, OverflowError):
        return False, None, 'timeout not numeric: %r' % (timeout,)
    if val != val or val in (float('inf'), float('-inf')):
        return False, None, 'timeout not finite: %r' % (timeout,)
    if val <= 0:
        return False, None, 'timeout must be > 0 seconds (got %r); infinite wait is not allowed' % (timeout,)
    # DWORD 0xFFFFFFFF is INFINITE; larger integers wrap at the WinAPI boundary.
    if val > 0xFFFFFFFE / 1000.0:
        return False, None, 'timeout exceeds finite DWORD wait budget'
    ms = int(round(val * 1000.0))
    if not 1 <= ms <= 0xFFFFFFFE:
        return False, None, 'timeout must round to a finite DWORD wait budget: %r' % (timeout,)
    return True, ms, None


def _env_block(env):
    items = sorted(((k, v) for k, v in env.items() if k and '=' not in k),
                   key=lambda kv: kv[0].upper())
    return ctypes.create_unicode_buffer(''.join('%s=%s\0' % (k, v) for k, v in items) + '\0')


def run(result, argv, cwd, env, out_path, err_path, timeout, fault=None):
    """在 Job Object 里跑 argv，把真实观察写进 result（形如 proc.run_capture 的字段）。"""
    fault = fault or {}
    # 启动前先验参：拒绝无限等待/非法超时，一个进程都不建。
    t_ok, wait_ms, t_why = validate_timeout(timeout)
    if not t_ok:
        result['failClosed'] = True
        result['error'] = 'refused before launch: %s' % t_why
        return
    result['containment'] = 'win-job'
    result['waitBudgetMs'] = wait_ms
    job = _k32.CreateJobObjectW(None, None)
    if not job:
        result['failClosed'] = True
        result['error'] = 'CreateJobObjectW failed: %d' % ctypes.get_last_error()
        return

    h_out = h_err = h_in = None
    pi = PROCESS_INFORMATION()
    try:
        info = JOBOBJECT_EXTENDED_LIMIT_INFORMATION()
        info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        if not _k32.SetInformationJobObject(job, JOB_OBJECT_EXTENDED_LIMIT_INFORMATION,
                                            ctypes.byref(info), ctypes.sizeof(info)):
            result['failClosed'] = True
            result['error'] = 'SetInformationJobObject failed: %d' % ctypes.get_last_error()
            return

        h_out = _open_out(out_path)
        h_err = _open_out(err_path)
        h_in = _open_null()

        si = STARTUPINFOW()
        si.cb = ctypes.sizeof(STARTUPINFOW)
        si.dwFlags = STARTF_USESTDHANDLES
        si.hStdInput = h_in
        si.hStdOutput = h_out
        si.hStdError = h_err

        flags = CREATE_SUSPENDED | CREATE_UNICODE_ENVIRONMENT | CREATE_NO_WINDOW
        env_block = _env_block(env)
        cmdline = ctypes.create_unicode_buffer(subprocess.list2cmdline(argv))
        started = _k32.CreateProcessW(None, cmdline, None, None, True, flags,
                                      ctypes.cast(env_block, ctypes.c_void_p), cwd,
                                      ctypes.byref(si), ctypes.byref(pi))
        if not started:
            # 真实启动失败：pid / rc 一律留空，不伪造。
            result['failClosed'] = True
            result['error'] = 'CreateProcessW failed: %d' % ctypes.get_last_error()
            return

        result['started'] = True
        result['pid'] = int(pi.dwProcessId)
        created = FILETIME()
        exited, kernel, user = FILETIME(), FILETIME(), FILETIME()
        identity_ok = bool(_k32.GetProcessTimes(pi.hProcess, ctypes.byref(created), ctypes.byref(exited),
                                               ctypes.byref(kernel), ctypes.byref(user)))
        identity_ok = identity_ok and (created.dwHighDateTime != 0 or created.dwLowDateTime != 0)
        if identity_ok:
            result['pidCreationTimeUtc'] = filetime_to_iso(created.dwHighDateTime,
                                                           created.dwLowDateTime)
        else:
            result['failClosed'] = True
            result['error'] = 'GetProcessTimes failed: %d' % ctypes.get_last_error()

        # ---- 归属：进程仍 SUSPENDED，一行用户代码都没跑过 → 无"先跑再归属"的逃逸窗口
        if fault.get('assign_fail'):
            assigned = 0
            try:
                ctypes.set_last_error(87)  # ERROR_INVALID_PARAMETER
            except AttributeError:  # pragma: no cover
                pass
        else:
            assigned = _k32.AssignProcessToJobObject(job, pi.hProcess)

        if not assigned:
            result['assignError'] = ctypes.get_last_error()
            result['contained'] = False
            result['failClosed'] = True
            # 绝不 resume：用自持 handle 终止 + 有界等待，避免留下"挂起的孤儿"。
            # 三个结果**分别**取证：终止是否成功 / 等待是否归零 / 退出码是否可读。
            # 任何一项没拿到都不能写成"已退出"。
            term_ok = bool(_k32.TerminateProcess(pi.hProcess, 4))
            waited = _k32.WaitForSingleObject(pi.hProcess, ASSIGN_FAIL_TERMINATE_WAIT_MS)
            wait_kind = classify_wait(waited)
            code = _wt.DWORD(0)
            got = bool(_k32.GetExitCodeProcess(pi.hProcess, ctypes.byref(code)))
            exit_known = bool(got and code.value != STILL_ACTIVE)
            if wait_kind == 'timeout':
                result['stillRunning'] = True    # 句柄未 signaled → 仍活着（已实测，不是猜测）
            elif exit_known:
                result['stillRunning'] = False
            else:
                result['stillRunning'] = None    # 未知：查询失败，不得当已退出
            result['exitKnown'] = exit_known
            result['assignFailureTerminate'] = {
                'terminateOk': term_ok,
                'waitedMs': ASSIGN_FAIL_TERMINATE_WAIT_MS,
                'waitResult': wait_kind,
                'exitQueryOk': got,
                'exitCode': int(code.value) if exit_known else None,
                'exitKnown': exit_known,
                'verdict': ('proven-exited' if exit_known and wait_kind == 'object0'
                            else ('proven-alive' if wait_kind == 'timeout' else 'unknown')),
            }
            return

        result['contained'] = True
        if not identity_ok:
            # Already assigned, still suspended: close our own tree without running user code.
            if not _k32.TerminateJobObject(job, 5):
                result['cleanupError'] = 'TerminateJobObject failed: %d' % ctypes.get_last_error()
        elif _k32.ResumeThread(pi.hThread) in (0xFFFFFFFF,):
            result['failClosed'] = True
            result['error'] = 'ResumeThread failed: %d' % ctypes.get_last_error()
            _k32.TerminateJobObject(job, 5)
        else:
            waited = _k32.WaitForSingleObject(pi.hProcess, wait_ms)
            kind = classify_wait(waited)
            result['waitResult'] = kind
            result['timedOut'] = (kind == 'timeout')
            if kind == 'failed':
                # 等待本身失败：既不能当"已退出"，也不能当"超时"。
                result['failClosed'] = True
                result['error'] = 'WaitForSingleObject failed: %d' % ctypes.get_last_error()
                result['stillRunning'] = None
            elif kind not in ('object0', 'timeout'):
                result['failClosed'] = True
                result['error'] = 'WaitForSingleObject unexpected result: %s' % kind

        # ---- 收口：父进程结束 ≠ 整树结束。还有活动进程（或活动数未知）就整树终止并有界等待归零
        terminated = False
        if result['timedOut']:
            active = _active(job)          # 父仍活着：不必等归零
            settle_ms = 0
        else:
            active, settle_ms = _settle(job)
        result['activeInitial'] = active
        result['activeSettleMs'] = settle_ms
        result['activeKnown'] = (active is not None)
        result['activeBeforeReap'] = active
        # 未知(None)必须当作"可能还有活口"去收口，不能当 0 放过。
        need_reap = result['timedOut'] or active is None or active > 0
        result['reapAttempted'] = bool(need_reap)
        if need_reap:
            # survivor 只指"父已退出但树里仍有活口"，不含"父自己被超时杀掉"。
            if active and not result['timedOut']:
                result['reapedSurvivors'] = True
            if fault.get('terminate_fail'):
                result['cleanupError'] = 'injected: TerminateJobObject failed (1234)'
            elif not _k32.TerminateJobObject(job, 8):
                result['cleanupError'] = 'TerminateJobObject failed: %d' % ctypes.get_last_error()
            else:
                terminated = True
            deadline = time.monotonic() + REAP_DEADLINE_SEC
            last = None
            while time.monotonic() < deadline:
                last = _active(job)
                if last == 0:
                    break
                time.sleep(0.05)
            if last is None:
                result['activeKnown'] = False
                result['cleanupError'] = ((result['cleanupError'] or '') +
                                          ' active-count-query-failed-after-reap').strip()
            elif last != 0:
                result['cleanupError'] = ((result['cleanupError'] or '') +
                                          ' active-processes-still-%s-after-%ss'
                                          % (last, REAP_DEADLINE_SEC)).strip()
            # 终止之后再等一次句柄：之前读只能读到 STILL_ACTIVE，拿不到真实退出码。
            post = _k32.WaitForSingleObject(pi.hProcess, POST_TERMINATE_WAIT_MS)
            result['postTerminateWait'] = classify_wait(post)

        code = _wt.DWORD(0)
        got = bool(_k32.GetExitCodeProcess(pi.hProcess, ctypes.byref(code)))
        result['exitKnown'] = bool(got and code.value != STILL_ACTIVE)
        if not got:
            # 查询失败 ≠ 已退出：明确写"未知"，交给上层拒绝。
            result['stillRunning'] = None
            result['rcSource'] = 'exit-query-failed'
            result['failClosed'] = True
        elif code.value == STILL_ACTIVE:
            result['stillRunning'] = True
            result['rcSource'] = 'not-exited'
        elif result['timedOut'] and terminated:
            # 超时后由**我们**终止：如实分开记录强制码与观测码。
            result['forcedExitCode'] = int(code.value)
            result['rc'] = 8
            result['rcSource'] = 'forced-on-timeout'
            result['stillRunning'] = False
        elif result['timedOut']:
            # 超时但清理失败：这个码是进程自己退出来的，不能标成"我们强制"。
            result['rc'] = int(code.value)
            result['rcSource'] = 'observed-after-failed-terminate'
            result['stillRunning'] = False
        else:
            result['rc'] = int(code.value)
            result['rcSource'] = 'observed'
            result['stillRunning'] = False

        result['activeAfter'] = _active(job)
        if result['activeAfter'] is None:
            result['activeKnown'] = False

    finally:
        for name, h in (('stdout', h_out), ('stderr', h_err), ('stdin', h_in),
                        ('thread', pi.hThread), ('process', pi.hProcess)):
            if h and not _k32.CloseHandle(h):
                result['closeErrors'].append('%s:%d' % (name, ctypes.get_last_error()))
        # KILL_ON_JOB_CLOSE 是最后一道保险；正常路径前面已主动收口并实测 ActiveProcesses==0。
        if job and not _k32.CloseHandle(job):
            result['closeErrors'].append('job:%d' % ctypes.get_last_error())

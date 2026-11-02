# -*- coding: utf-8 -*-
r"""R6-C 消费点**安全 mock** 测试：不启动任何真实进程，不制造孤儿。

专治协调者复核（2026-11-02 快照）点出的四类缺口，每条都**单独毒化一个字段**、其余保持健康：

  A. `compute_ok` 逐字段：closeErrors / activeAfter(未知/布尔/字符串) / contained /
     activeKnown / stillRunning / cleanupError / failClosed / refused / started / rc
  B. 能力不足**启动前拒绝**：非 Windows 的 `auto`、未知 containment 取值 → Popen 调用数必须 0
  C. Job 侧未知不当成功（FakeK32）：活动数查询失败、Wait 失败、
     归属失败后 Terminate/Wait/GetExit 结果未知、timeout=0 拒绝启动

用法：
  python test-verify-examples-proc-consumer.py --work <目录> --out <report.json>
"""
import argparse
import ctypes
import io
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import verify_examples_proc as proc  # noqa: E402
import verify_examples_selcase as selcase  # noqa: E402

ROWS = []


def check(name, cond, observed=''):
    ok = bool(cond)
    ROWS.append({'check': name, 'ok': ok, 'state': 'pass' if ok else 'fail', 'observed': observed})
    if not ok:
        print('FAIL %-44s %s' % (name, observed))
    return ok


def healthy(**over):
    """健康形状：基础设施成功。毒化测试只改其中**一个**键。"""
    base = {
        'refused': False, 'refusalReason': None,
        'started': True, 'rc': 0, 'failClosed': False,
        'stillRunning': False, 'cleanupError': None, 'timedOut': False,
        'contained': True, 'activeAfter': 0, 'activeKnown': True,
        'closeErrors': [], 'restrictedSafe': True,
    }
    base.update(over)
    return base


# 每个毒化形状：(名字, 覆盖字段, 必须出现的 okReasons 片段)
POISONS = [
    ('close-errors-only', {'closeErrors': ['job:5']}, 'close-errors'),
    ('active-none', {'activeAfter': None}, 'active-after-not-confirmed-zero'),
    ('active-false', {'activeAfter': False}, 'active-after-not-confirmed-zero'),
    ('active-true', {'activeAfter': True}, 'active-after-not-confirmed-zero'),
    ('active-str', {'activeAfter': '0'}, 'active-after-not-confirmed-zero'),
    ('active-one', {'activeAfter': 1}, 'active-after-not-confirmed-zero'),
    ('active-known-false', {'activeKnown': False}, 'active-count-query-failed'),
    ('contained-false', {'contained': False}, 'not-contained'),
    ('contained-none', {'contained': None}, 'not-contained'),
    ('still-running-none', {'stillRunning': None}, 'exit-unknown-or-running'),
    ('timed-out', {'timedOut': True}, 'timed-out'),
    ('still-running-true', {'stillRunning': True}, 'exit-unknown-or-running'),
    ('cleanup-error', {'cleanupError': 'synthetic Close/Wait failure'}, 'cleanup-error'),
    ('failclosed', {'failClosed': True}, 'fail-closed'),
    ('refused', {'refused': True, 'refusalReason': 'unsupported-platform'}, 'refused'),
    ('not-started', {'started': False}, 'not-started'),
    ('rc-unknown', {'rc': None}, 'rc-unobserved'),
    ('restricted-unsafe', {'restrictedSafe': False}, 'not-restricted-safe'),
]


def old_ok(result):
    """**原样**复刻复核快照里的 ok 表达式（`snapshot/verify_examples_proc.py:241-248`）。

    保留它是为了可复核地重现缺口：同一组毒化形状喂给旧公式，看它放绿了哪几条。
    这不是"另一个实现"，而是同一段判据的逐字抄写。
    """
    return bool(
        result.get('started')
        and result.get('rc') is not None
        and not result.get('failClosed')
        and not result.get('stillRunning')
        and result.get('cleanupError') is None
        and result.get('activeAfter') in (0, None)
    )


def test_ok_fieldwise():
    ok, reasons = proc.compute_ok(healthy())
    check('ok.healthy-control', ok is True and reasons == [], 'ok=%r reasons=%r' % (ok, reasons))
    old_pass = []
    for name, over, must in POISONS:
        r = healthy(**over)
        ok, reasons = proc.compute_ok(r)
        hit = any(must in x for x in reasons)
        check('ok.poison.%s' % name, ok is False and hit,
              'ok=%r reasons=%r need=%r' % (ok, reasons, must))
        if old_ok(r):
            old_pass.append(name)
    # 旧公式对这几种形状会放绿 —— 与复核报告的四条缺口一一对应
    check('ok.old-formula-reproduces-gaps',
          set(old_pass) >= {'close-errors-only', 'active-none', 'active-false', 'contained-false'},
          'oldFormulaPassed=%r' % (old_pass,))
    # 非零业务退出不属于基础设施失败（口径不能被改掉）
    ok, reasons = proc.compute_ok(healthy(rc=7))
    check('ok.nonzero-is-not-infra-failure', ok is True and reasons == [], 'rc=7 -> %r %r' % (ok, reasons))


class PopenSpy(object):
    """把 Popen 换掉：只计数，绝不启动真实进程。"""

    def __init__(self):
        self.calls = 0
        self.argv = []

    def __call__(self, *a, **kw):
        self.calls += 1
        self.argv.append(a[0] if a else kw.get('args'))
        raise AssertionError('Popen 不应被调用（能力不足必须启动前拒绝）')


def test_case_gate():
    """选择器的最终判据：基础设施可信 AND 业务成立；缺字段一律不放行。"""
    check('gate.infra-ok-business-true', selcase.case_passed({'infraOk': True}, True) is True, '')
    check('gate.infra-fail-business-true', selcase.case_passed({'infraOk': False}, True) is False,
          'infraOk=False 但业务 True 时不得放行')
    check('gate.infra-missing', selcase.case_passed({}, True) is False, '缺 infraOk 字段不得放行')
    check('gate.infra-int-one-rejected', selcase.case_passed({'infraOk': 1}, True) is False,
          '整数 1 不是 True（严格判定）')
    check('gate.infra-string-true', selcase.case_passed({'infraOk': 'true'}, True) is False,
          "字符串 'true' 不是 True")
    check('gate.business-false', selcase.case_passed({'infraOk': True}, False) is False, '')
    check('gate.business-none', selcase.case_passed({'infraOk': True}, None) is False, 'None 不算成立')


def test_refuse_before_launch(work):
    real_popen = proc.subprocess.Popen
    real_os = proc.os
    real_tree, real_err = proc._TREE_MODULE, proc._TREE_ERROR
    spy = PopenSpy()
    try:
        proc.subprocess.Popen = spy

        # 1) 非 Windows + auto：必须 refused，Popen 0 次
        class PosixOS(object):
            name = 'posix'
            environ = os.environ
            path = os.path
        proc.os = PosixOS()
        mode, why = proc._resolve_containment('auto')
        check('refuse.nonwindows-auto-mode', mode == proc.CONTAINMENT_REFUSED, '%s / %s' % (mode, why))
        proc.os = real_os
        proc._TREE_MODULE, proc._TREE_ERROR = None, 'forced-unavailable-for-test'
        r = proc.run_capture([sys.executable, '-c', 'print(1)'], 'refuse-auto',
                             os.path.join(work, 'cases'), containment='auto', timeout=5)
        check('refuse.auto-popen-zero', spy.calls == 0, 'popenCalls=%d' % spy.calls)
        check('refuse.auto-not-started', r['started'] is False and r['pid'] is None and r['rc'] is None,
              'started=%r pid=%r rc=%r' % (r['started'], r['pid'], r['rc']))
        check('refuse.auto-ok-false', r['ok'] is False and r['refused'] is True,
              'ok=%r refused=%r reasons=%r' % (r['ok'], r['refused'], r['okReasons']))
        check('refuse.auto-reason-surfaced',
              'tree-unavailable' in (r['refusalReason'] or '') or 'forced-unavailable' in (r['error'] or ''),
              'refusalReason=%r error=%r' % (r['refusalReason'], r['error']))

        # 2) 未知 containment 取值：refused，Popen 0 次
        mode, why = proc._resolve_containment('job-object-maybe')
        check('refuse.unknown-mode', mode == proc.CONTAINMENT_REFUSED, '%s / %s' % (mode, why))
        r = proc.run_capture([sys.executable, '-c', 'print(1)'], 'refuse-unknown',
                             os.path.join(work, 'cases'), containment='bogus', timeout=5)
        check('refuse.unknown-popen-zero', spy.calls == 0, 'popenCalls=%d' % spy.calls)
        check('refuse.unknown-ok-false', r['ok'] is False and r['refused'] is True, 'ok=%r' % (r['ok'],))

        # 3) 显式 'none'：允许启动，但必须标非受限、且 ok 必定 False
        r = proc.run_capture([sys.executable, '-c', 'print(1)'], 'explicit-none',
                             os.path.join(work, 'cases'), containment='none', timeout=30)
        check('portable.explicit-none-runs', spy.calls == 1, 'popenCalls=%d' % spy.calls)
        check('portable.not-restricted-safe',
              r['restrictedSafe'] is False and r['containment'] == proc.CONTAINMENT_NONE,
              'restrictedSafe=%r containment=%r' % (r['restrictedSafe'], r['containment']))
        check('portable.ok-false', r['ok'] is False and 'not-restricted-safe' in (r['okReasons'] or []),
              'ok=%r reasons=%r' % (r['ok'], r['okReasons']))
    finally:
        proc.subprocess.Popen = real_popen
        proc.os = real_os
        proc._TREE_MODULE, proc._TREE_ERROR = real_tree, real_err


# --------------------------- FakeK32：不创建真实进程 ---------------------------

class FakeK32(object):
    """只模拟 kernel32 的返回值；CreateProcessW 不真启动，返回合成 pid/handle。"""

    def __init__(self, query_active=None, query_fail=False, process_wait=None,
                 assign_ok=True, terminate_job_ok=True, terminate_process_ok=True,
                 exit_query_ok=True, exit_code=0, job_create_ok=True, identity_ok=True):
        self.query_active = list(query_active or [0])
        self.query_fail = query_fail
        self.process_wait = list(process_wait or [0])
        self.assign_ok = assign_ok
        self.terminate_job_ok = terminate_job_ok
        self.terminate_process_ok = terminate_process_ok
        self.exit_query_ok = exit_query_ok
        self.exit_code = exit_code
        self.job_create_ok = job_create_ok
        self.identity_ok = identity_ok
        self.calls = {'CreateJobObjectW': 0, 'CreateProcessW': 0, 'WaitForSingleObject': 0,
                      'TerminateJobObject': 0, 'TerminateProcess': 0, 'Query': 0}
        self.ACCOUNTING = None  # run_tree 注入：与 tree 模块同一个 ctypes 结构类

    def _next(self, seq, default=0):
        if not seq:
            return default
        return seq.pop(0)

    def CreateJobObjectW(self, a, b):
        self.calls['CreateJobObjectW'] += 1
        return 777 if self.job_create_ok else 0

    def SetInformationJobObject(self, *a):
        return 1

    def CreateFileW(self, *a):
        return 1001

    def SetHandleInformation(self, *a):
        return 1

    def CreateProcessW(self, app, cmd, pa, ta, inherit, flags, env, cwd, si, pi):
        self.calls['CreateProcessW'] += 1
        info = pi._obj
        info.dwProcessId = 4242
        info.hProcess = 2001
        info.hThread = 2002
        return 1

    def GetProcessTimes(self, *a):
        if not self.identity_ok:
            return 0
        a[1]._obj.dwHighDateTime = 1
        return 1

    def AssignProcessToJobObject(self, job, h):
        return 1 if self.assign_ok else 0

    def ResumeThread(self, h):
        return 0

    def WaitForSingleObject(self, h, ms):
        self.calls['WaitForSingleObject'] += 1
        if h == 2001:                     # 进程句柄：按脚本
            return self._next(self.process_wait, 0)
        return 0                          # 其余（post-terminate）按已 signaled

    def TerminateJobObject(self, job, code):
        self.calls['TerminateJobObject'] += 1
        return 1 if self.terminate_job_ok else 0

    def TerminateProcess(self, h, code):
        self.calls['TerminateProcess'] += 1
        return 1 if self.terminate_process_ok else 0

    def GetExitCodeProcess(self, h, ptr):
        if not self.exit_query_ok:
            return 0
        ctypes.cast(ptr, ctypes.POINTER(ctypes.c_ulong)).contents.value = self.exit_code
        return 1

    def QueryInformationJobObject(self, job, cls, ptr, size, got):
        self.calls['Query'] += 1
        if self.query_fail:
            return 0
        n = self._next(self.query_active, 0)
        ctypes.cast(ptr, ctypes.POINTER(self.ACCOUNTING)).contents.ActiveProcesses = n
        return 1

    def CloseHandle(self, h):
        return 1


WAIT_OBJECT0, WAIT_TIMEOUT_V, WAIT_FAILED_V = 0x0, 0x102, 0xFFFFFFFF


def run_tree(tree, fake, timeout=5, fault=None):
    """用假 kernel32 直接驱动 tree.run，不创建任何真实进程。"""
    fake.ACCOUNTING = tree.JOBOBJECT_BASIC_ACCOUNTING_INFORMATION
    tree._k32 = fake
    os.makedirs(os.path.join(WORK, 'fake'), exist_ok=True)
    result = {
        'closeErrors': [], 'contained': False, 'failClosed': False, 'stillRunning': False,
        'cleanupError': None, 'activeAfter': None, 'activeKnown': None, 'started': False,
        'rc': None, 'rcSource': None, 'timedOut': False, 'restrictedSafe': True,
        'refused': False, 'pid': None,
    }
    out = os.path.join(WORK, 'fake', 'stdout.raw')
    err = os.path.join(WORK, 'fake', 'stderr.raw')
    tree.run(result, ['fake.exe'], WORK, dict(os.environ), out, err, timeout, fault)
    result['ok'], result['okReasons'] = proc.compute_ok(result)
    return result


def test_fake_kernel(work):
    tree, why = proc._load_tree()
    if tree is None:
        check('fakek32.tree-unavailable', False, 'reason=%r' % (why,))
        return
    real = tree._k32
    try:
        # a) 活动数查询失败：不能当 0 放过，必须继续收口并在结果里暴露
        f = FakeK32(query_fail=True, process_wait=[WAIT_OBJECT0], exit_code=0)
        r = run_tree(tree, f)
        check('fakek32.query-fail-active-unknown',
              r['activeKnown'] is False and r['activeAfter'] is None, 'activeKnown=%r' % (r['activeKnown'],))
        check('fakek32.query-fail-still-reaps', f.calls['TerminateJobObject'] >= 1,
              'TerminateJobObject=%d' % f.calls['TerminateJobObject'])
        check('fakek32.query-fail-ok-false', r['ok'] is False, 'ok=%r reasons=%r' % (r['ok'], r['okReasons']))

        # b) WaitForSingleObject 失败：既不是"已退出"也不是"超时"
        f = FakeK32(process_wait=[WAIT_FAILED_V])
        r = run_tree(tree, f)
        check('fakek32.wait-failed-classified', r.get('waitResult') == 'failed',
              'waitResult=%r' % (r.get('waitResult'),))
        check('fakek32.wait-failed-failclosed', r['failClosed'] is True, 'failClosed=%r err=%r'
              % (r['failClosed'], r.get('error')))
        check('fakek32.wait-failed-ok-false', r['ok'] is False, 'ok=%r' % (r['ok'],))

        # c) timeout=0 / 负数 / 非数值：启动前拒绝，一个进程都不建
        for label, bad in (('zero', 0), ('negative', -1), ('nan', float('nan')), ('str', 'x')):
            f = FakeK32()
            r = run_tree(tree, f, timeout=bad)
            check('fakek32.timeout-%s-refused' % label,
                  f.calls['CreateJobObjectW'] == 0 and f.calls['CreateProcessW'] == 0,
                  'job=%d proc=%d' % (f.calls['CreateJobObjectW'], f.calls['CreateProcessW']))
            check('fakek32.timeout-%s-failclosed' % label,
                  r['failClosed'] is True and r['started'] is False and r['ok'] is False,
                  'failClosed=%r started=%r ok=%r' % (r['failClosed'], r['started'], r['ok']))

        # d1) 归属失败 + 终止失败 + 等待超时 → 只能"证明仍活着"，绝不能写成已退出
        f = FakeK32(assign_ok=False, terminate_process_ok=False, exit_query_ok=False,
                    process_wait=[WAIT_TIMEOUT_V])
        r = run_tree(tree, f)
        aft = r.get('assignFailureTerminate') or {}
        check('fakek32.assign-alive-verdict', aft.get('verdict') == 'proven-alive', 'aft=%r' % (aft,))
        check('fakek32.assign-alive-not-exited',
              r['stillRunning'] is True and r['exitKnown'] is False and r['rc'] is None,
              'stillRunning=%r exitKnown=%r rc=%r' % (r['stillRunning'], r['exitKnown'], r['rc']))
        check('fakek32.assign-alive-ok-false', r['ok'] is False, 'ok=%r reasons=%r'
              % (r['ok'], r['okReasons']))
        check('fakek32.assign-terminate-failure-recorded', aft.get('terminateOk') is False,
              'terminateOk=%r' % (aft.get('terminateOk'),))

        # d2) 归属失败 + 句柄已 signaled 但退出码查不出来 → 明确"未知"，不是已退出
        f = FakeK32(assign_ok=False, process_wait=[WAIT_OBJECT0], exit_query_ok=False)
        r = run_tree(tree, f)
        aft = r.get('assignFailureTerminate') or {}
        check('fakek32.assign-unknown-verdict', aft.get('verdict') == 'unknown', 'aft=%r' % (aft,))
        check('fakek32.assign-unknown-not-exited',
              r['stillRunning'] is None and r['exitKnown'] is False and r['rc'] is None,
              'stillRunning=%r exitKnown=%r rc=%r' % (r['stillRunning'], r['exitKnown'], r['rc']))
        check('fakek32.assign-unknown-ok-false', r['ok'] is False, 'ok=%r reasons=%r'
              % (r['ok'], r['okReasons']))

        # e) 归属失败但三项证据齐全 → proven-exited，且仍 fail-closed 不 ok
        f = FakeK32(assign_ok=False, process_wait=[WAIT_OBJECT0], exit_code=4)
        r = run_tree(tree, f)
        aft = r.get('assignFailureTerminate') or {}
        check('fakek32.assign-proven-exited', aft.get('verdict') == 'proven-exited',
              'aft=%r' % (aft,))
        check('fakek32.assign-proven-not-ok', r['ok'] is False and r['contained'] is False,
              'ok=%r contained=%r' % (r['ok'], r['contained']))

        # f) 退出码查询失败（正常路径）：rcSource 明确为查询失败，不冒充已退出
        f = FakeK32(exit_query_ok=False)
        r = run_tree(tree, f)
        check('fakek32.exit-query-failed-source',
              r['rcSource'] == 'exit-query-failed' and r['rc'] is None, 'rcSource=%r rc=%r'
              % (r['rcSource'], r['rc']))
        check('fakek32.exit-query-failed-ok-false', r['ok'] is False, 'ok=%r' % (r['ok'],))
    finally:
        tree._k32 = real


WORK = None


def main():
    global WORK
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()
    WORK = os.path.abspath(args.work)
    os.makedirs(os.path.join(WORK, 'cases'), exist_ok=True)

    test_ok_fieldwise()
    test_case_gate()
    test_refuse_before_launch(WORK)
    test_fake_kernel(WORK)

    failed = [r for r in ROWS if r['ok'] is False]
    report = {
        'generatedUtc': proc.utc(), 'python': sys.executable, 'work': WORK,
        'nativeProcessesStarted': 0,
        'method': 'PURE_MOCK: compute_ok 逐字段毒化 + Popen 计数 + FakeK32（CreateProcessW 未真调用）',
        'checkCount': len(ROWS), 'failedCount': len(failed),
        'status': 'PASS' if not failed else 'FAIL', 'checks': ROWS,
    }
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or '.', exist_ok=True)
    with io.open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2)
    print(json.dumps({'status': report['status'], 'checks': report['checkCount'],
                      'failed': report['failedCount']}, ensure_ascii=False))
    for f in failed:
        print('  FAILED: %s (%s)' % (f['check'], f['observed']))
    return 0 if not failed else 1


if __name__ == '__main__':
    sys.exit(main())

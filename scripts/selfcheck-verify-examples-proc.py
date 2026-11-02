# -*- coding: utf-8 -*-
r"""R6-C 进程收口/raw 捕获的**真实自检**（纯 Python，不给 Go 覆盖率造数）。

它不"检查 finally 是否存在"，而是真的起进程、真的超时、真的制造"父退孙活"，
并对**可观察事实**断言：rc、timedOut、activeAfter、raw 字节、SHA、pid 存活、见证进程。

用法：
  python selfcheck-verify-examples-proc.py --work <空目录或新建目录> --out <report.json>

覆盖的分支（每项都是一条断言，不是"看一眼日志"）：
  ok-quick / nonzero / binary / timeout-tree / detach-survivors / startup-fail
  assign-fail（注入）/ terminate-fail（注入）/ argv-roundtrip / env-roundtrip
  no-overwrite / artifact-on-failure / witness-unaffected / platform-declared
"""
import argparse
import ctypes
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
SENTINEL = os.path.join(HERE, 'testdata', 'verify-proc-sentinel.py')

sys.path.insert(0, HERE)
import verify_examples_proc as proc  # noqa: E402


def alive(pid):
    """按 pid 判断进程是否还活着（只用于核对**我们自己**起过的 pid）。"""
    if pid is None:
        return None
    if os.name != 'nt':
        try:
            os.kill(pid, 0)
            return True
        except OSError:
            return False
    from ctypes import wintypes
    k32 = ctypes.WinDLL('kernel32', use_last_error=True)
    k32.OpenProcess.restype = wintypes.HANDLE
    k32.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    k32.WaitForSingleObject.restype = wintypes.DWORD
    k32.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    k32.CloseHandle.argtypes = [wintypes.HANDLE]
    h = k32.OpenProcess(0x00100000, False, int(pid))  # SYNCHRONIZE
    if not h:
        return False
    try:
        return k32.WaitForSingleObject(h, 0) == 0x102  # WAIT_TIMEOUT = 仍在跑
    finally:
        k32.CloseHandle(h)


class Checker(object):
    def __init__(self):
        self.rows = []

    def check(self, name, cond, observed=''):
        """`observed` 一律写**观测到的事实**（如 `rc=8`），不写失败结论。

        踩过的坑：早先把"解码后 raw 被改写"当 detail 传进来，条件成立时报告里就出现
        `PASS … | 解码后 raw 被改写`——结论与文案自相矛盾。判据只有 `ok`，文案只描述观测。
        """
        ok = bool(cond)
        self.rows.append({'check': name, 'ok': ok, 'state': 'pass' if ok else 'fail',
                          'observed': observed})
        if not ok:
            print('FAIL %-32s %s' % (name, observed))
        return ok

    def skip(self, name, reason):
        """跳过要写明原因：跳过不等于通过，报告里单独计数。"""
        self.rows.append({'check': name, 'ok': None, 'skipped': reason})

    @property
    def failed(self):
        return [r for r in self.rows if r['ok'] is False]

    @property
    def skipped(self):
        return [r for r in self.rows if r['ok'] is None]


def sha(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for c in iter(lambda: fh.read(1 << 20), b''):
            h.update(c)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True, help='自检工作目录（会在其下建 cases/）')
    ap.add_argument('--out', required=True, help='报告 JSON 路径')
    args = ap.parse_args()

    py = sys.executable
    work = os.path.abspath(args.work)
    os.makedirs(work, exist_ok=True)
    log_dir = os.path.join(work, 'cases')
    ck = Checker()
    report = {'started': proc.utc(), 'python': py, 'sentinel': SENTINEL,
              'platform': os.name, 'work': work}

    # 真判断，不是恒真：能力探测必须自己说"能用"，且理由要是 job-object。
    _tree_ok, _tree_reason = proc.tree_available()
    ck.check('platform-declared', _tree_ok is True and _tree_reason == 'job-object',
             'tree_available=%r reason=%r containment=%s'
             % (_tree_ok, _tree_reason,
                proc.CONTAINMENT_WIN_JOB if os.name == 'nt' else proc.CONTAINMENT_NONE))
    if os.name != 'nt':
        # 明确不支持：只记录，不冒充通过。
        report['status'] = 'UNSUPPORTED-PLATFORM'
        report['checks'] = ck.rows
        with open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
            json.dump(report, fh, ensure_ascii=False, indent=2)
        return 3

    # 见证进程：**不在**被测 job 里，任何收口都不该影响它。
    # 它的收口放进**外层 finally**：中间任何断言/IO 异常都不能让它继续存活 120 秒。
    witness = subprocess.Popen([py, SENTINEL, 'grand', '120'])
    report['witnessPid'] = witness.pid

    def run(case_id, argv, **kw):
        return proc.run_capture(argv, case_id, log_dir, **kw)

    try:
        # 1) 正常退出
        r = run('ok-quick', [py, SENTINEL, 'quick'], timeout=20)
        ck.check('ok-quick.rc', r['rc'] == 0, 'rc=%r' % r['rc'])
        ck.check('ok-quick.ok', r['ok'] is True, 'ok=%r failClosed=%r err=%r' % (r['ok'], r['failClosed'], r['error']))
        ck.check('ok-quick.contained', r['contained'] is True and r['containment'] == proc.CONTAINMENT_WIN_JOB,
                 'containment=%r contained=%r' % (r['containment'], r['contained']))
        ck.check('ok-quick.activeAfter', r['activeAfter'] == 0, 'activeAfter=%r' % (r['activeAfter'],))
        ck.check('ok-quick.emptyStdout', r['stdout']['bytes'] == 0
                 and r['stdout']['sha256'] == hashlib.sha256(b'').hexdigest(),
                 'bytes=%r sha=%r' % (r['stdout']['bytes'], r['stdout']['sha256']))

        # 2) 非零退出：rc 是**观测值**，harness 仍然 ok
        r = run('nonzero', [py, SENTINEL, 'nonzero', '7'], timeout=20)
        ck.check('nonzero.rc', r['rc'] == 7, 'rc=%r' % r['rc'])
        ck.check('nonzero.ok', r['ok'] is True, 'ok=%r cleanupError=%r' % (r['ok'], r['cleanupError']))

        # 3) 混合原始字节：CRLF / NUL / 非法 UTF-8 都必须原样保留，raw 未被解码改写
        r = run('binary', [py, SENTINEL, 'binary'], timeout=20)
        raw = open(r['stdout']['path'], 'rb').read()
        eraw = open(r['stderr']['path'], 'rb').read()
        ck.check('binary.crlf', raw.count(b'\r\n') == 2, 'CRLF=%d' % raw.count(b'\r\n'))
        ck.check('binary.nul', b'nul-after:\x00:end' in raw, 'nul-count=%d' % raw.count(b'\x00'))
        ck.check('binary.invalid-utf8', b'\x81\x30 invalid-utf8' in raw,
                 'invalid-seq=%d' % raw.count(b'\x81\x30'))
        ck.check('binary.gbk-bytes', b'\xd6\xd0\xce\xc4' in raw,
                 'gbk-seq=%d' % raw.count(b'\xd6\xd0\xce\xc4'))
        ck.check('binary.stderr-raw', b'stderr-\xff\xfe-bytes' in eraw,
                 'stderr-seq=%d' % eraw.count(b'stderr-'))
        ck.check('binary.sha-matches-bytes', r['stdout']['sha256'] == sha(r['stdout']['path'])
                 and r['stdout']['bytes'] == len(raw),
                 'disk-sha=%s bytes=%d' % (sha(r['stdout']['path'])[:12], len(raw)))
        # 展示解码绝不能改写 raw：解码后重新哈希，必须与记录的 SHA 相同
        ck.check('binary.display-not-authoritative', sha(r['stdout']['path']) == r['stdout']['sha256'],
                 'sha-after-display=%s' % sha(r['stdout']['path'])[:12])
        ck.check('binary.display-tail-decoded', bool(r['displayTail']['tail']),
                 'encoding=%r' % r['displayTail']['encoding'])

        # 4) 超时整树：父→子→孙全部 sleep，超时后必须 activeAfter==0
        t0 = time.monotonic()
        r = run('timeout-tree', [py, SENTINEL, 'tree', '30'], timeout=1.5)
        dt = time.monotonic() - t0
        ck.check('timeout.timedOut', r['timedOut'] is True, 'timedOut=%r rc=%r' % (r['timedOut'], r['rc']))
        ck.check('timeout.forcedRc', r['rc'] == 8 and r['rcSource'] == 'forced-on-timeout',
                 'rc=%r src=%r' % (r['rc'], r['rcSource']))
        ck.check('timeout.activeAfter0', r['activeAfter'] == 0, 'activeAfter=%r' % (r['activeAfter'],))
        ck.check('timeout.bounded', dt < 20, '实测 %.2fs' % dt)
        # 超时是被**我们**终止的：rc 是强制码，不能算基础设施 ok；但记录本身必须完整可信。
        ck.check('timeout.not-ok', r['ok'] is False and 'timed-out' in (r['okReasons'] or []),
                 'ok=%r reasons=%r' % (r['ok'], r['okReasons']))
        ck.check('timeout.record-trustworthy',
                 r['rcSource'] == 'forced-on-timeout' and r['activeAfter'] == 0
                 and r['cleanupError'] is None and r['contained'] is True,
                 'rcSource=%r activeAfter=%r cleanupError=%r' % (r['rcSource'], r['activeAfter'], r['cleanupError']))

        # 5) 父退孙活：父 rc 0，孙仍在 job 里 → 必须被收口，不能把"父退出"当整树完成
        r = run('detach-survivors', [py, SENTINEL, 'detach', '30'], timeout=20)
        ck.check('detach.parentRc0', r['rc'] == 0, 'rc=%r' % r['rc'])
        ck.check('detach.reapedSurvivors', r['reapedSurvivors'] is True, 'reaped=%r' % r['reapedSurvivors'])
        ck.check('detach.activeAfter0', r['activeAfter'] == 0, 'activeAfter=%r' % (r['activeAfter'],))
        ck.check('detach.ok', r['ok'] is True, 'ok=%r cleanupError=%r' % (r['ok'], r['cleanupError']))

        # 6) 启动失败：没有 pid、没有 rc，不能伪造
        missing = os.path.join(work, 'no-such-exe-xyz.exe')
        r = run('startup-fail', [missing], timeout=10)
        ck.check('startup.started', r['started'] is False, 'started=%r' % r['started'])
        ck.check('startup.noFakePid', r['pid'] is None, 'pid=%r' % r['pid'])
        ck.check('startup.noFakeRc', r['rc'] is None, 'rc=%r' % r['rc'])
        ck.check('startup.failClosed', r['failClosed'] is True and r['ok'] is False,
                 'failClosed=%r ok=%r' % (r['failClosed'], r['ok']))

        # 7) 归属失败（安全注入）：不 resume、按自持 handle 终止、有界确认、不留下挂起孤儿
        r = run('assign-fail', [py, SENTINEL, 'grand', '30'], timeout=10, fault={'assign_fail': True})
        ck.check('assign.containedFalse', r['contained'] is False, 'contained=%r' % r['contained'])
        ck.check('assign.errorReported', bool(r['assignError']), 'assignError=%r' % r['assignError'])
        ck.check('assign.failClosed', r['failClosed'] is True and r['ok'] is False,
                 'failClosed=%r ok=%r' % (r['failClosed'], r['ok']))
        aft = r.get('assignFailureTerminate') or {}
        ck.check('assign.terminatedBounded',
                 aft.get('verdict') == 'proven-exited' and aft.get('terminateOk') is True
                 and aft.get('exitKnown') is True and aft.get('waitResult') == 'object0',
                 'terminate=%r' % (aft,))
        # 三项证据都拿到才算"已退出"；查询失败只能是未知，不能写成已退出。
        ck.check('assign.exitProven', r.get('stillRunning') is False and r.get('exitKnown') is True,
                 'stillRunning=%r exitKnown=%r' % (r.get('stillRunning'), r.get('exitKnown')))
        ck.check('assign.notAliveAfter', alive(r['pid']) is False, 'pid=%r alive=%r' % (r['pid'], alive(r['pid'])))

        # 8) 清理失败（安全注入）：cleanupError 必须暴露、ok 必须为 False
        r = run('terminate-fail', [py, SENTINEL, 'slow-print', '3'], timeout=0.5,
                fault={'terminate_fail': True})
        ck.check('cleanup.errorSurfaced', bool(r['cleanupError']), 'cleanupError=%r' % (r['cleanupError'],))
        ck.check('cleanup.notOk', r['ok'] is False, 'ok=%r' % (r['ok'],))

        # 9) 参数回环：空串 / 空格 / 中文 / 引号 / 尾反斜杠
        tricky = ['', 'a b', '中文', 'q"q', 'C:\\tail\\']
        r = run('argv-roundtrip', [py, SENTINEL, 'echo-args'] + tricky, timeout=20)
        got = json.loads(open(r['stdout']['path'], 'rb').read().decode('utf-8').strip() or 'null')
        ck.check('argv.roundtrip', got == tricky, 'got=%r' % (got,))

        # 10) 环境回环：只在子进程局部传递，父环境不变
        parent_before = dict(os.environ)
        over = {'GOXML_R6_EMPTY': '', 'GOXML_R6_SPACE': 'a b', 'GOXML_R6_CN': '中文',
                'GOXML_R6_QUOTE': 'q"q\\', 'GOXML_R6_NL': 'a\nb'}
        r = run('env-roundtrip', [py, SENTINEL, 'echo-env'] + sorted(over), timeout=20,
                env_overrides=dict(over, **proc.GO_ENV_FIXED))
        env_got = json.loads(open(r['stdout']['path'], 'rb').read().decode('utf-8').strip() or 'null')
        ck.check('env.values', env_got == over, 'got=%r' % (env_got,))
        ck.check('env.parentUnchanged', dict(os.environ) == parent_before,
                 '父环境键数 %d -> %d' % (len(parent_before), len(os.environ)))
        ck.check('env.notLeakedToParent', 'GOXML_R6_CN' not in os.environ,
                 'parent-has-key=%r' % ('GOXML_R6_CN' in os.environ))

        # Go 六项固定值真的到了子进程
        r = run('env-go-six', [py, SENTINEL, 'echo-env'] + sorted(proc.GO_ENV_FIXED), timeout=20,
                env_overrides=dict(proc.GO_ENV_FIXED))
        six = json.loads(open(r['stdout']['path'], 'rb').read().decode('utf-8').strip() or 'null')
        ck.check('env.goSix', six == proc.GO_ENV_FIXED, 'got=%r' % (six,))

        # 11) 不覆盖历史字节：同 case_id 再来一次 → 新目录，旧 result.json 字节不变
        first = run('repeat', [py, SENTINEL, 'quick'], timeout=20)
        old_bytes = open(first['resultPath'], 'rb').read()
        old_sha = hashlib.sha256(old_bytes).hexdigest()
        second = run('repeat', [py, SENTINEL, 'nonzero', '3'], timeout=20)
        ck.check('repeat.newDir', second['caseId'] == 'repeat-2' and second['caseDir'] != first['caseDir'],
                 'second=%r' % (second['caseId'],))
        old_now = hashlib.sha256(open(first['resultPath'], 'rb').read()).hexdigest()
        ck.check('repeat.oldBytesIntact', old_now == old_sha,
                 'old-sha-now=%s (was %s)' % (old_now[:12], old_sha[:12]))

        # 12) 失败也留证据：raw 与 result.json 都在
        r = run('artifact-on-failure', [py, SENTINEL, 'nonzero', '9'], timeout=20)
        ck.check('failure.artifacts', os.path.isfile(r['resultPath'])
                 and os.path.isfile(r['stdout']['path']) and os.path.isfile(r['stderr']['path']),
                 'result=%r' % (r['resultPath'],))

        # 13) 见证进程全程不受影响；其收口在 finally，异常时也有界收口
        ck.check('witness.alive', witness.poll() is None, 'poll=%r' % witness.poll())

    except BaseException as exc:  # 记录异常，但**不覆盖**已有断言结果
        report['exception'] = '%s: %s' % (type(exc).__name__, exc)
        ck.check('harness.completed', False, 'exception=%s: %s' % (type(exc).__name__, exc))
    finally:
        # 自持对象、按自身身份收口、有界等待；清理失败单独记录，不掩盖原始故障。
        w_alive_before = witness.poll() is None
        cleanup_err = None
        try:
            if w_alive_before:
                witness.terminate()
            witness.wait(timeout=10)
        except subprocess.TimeoutExpired:
            witness.kill()
            try:
                witness.wait(timeout=10)
            except subprocess.TimeoutExpired as exc:
                cleanup_err = 'witness did not exit after kill: %s' % exc
        except Exception as exc:
            cleanup_err = '%s: %s' % (type(exc).__name__, exc)
        report['witnessCleanup'] = {
            'aliveBeforeCleanup': w_alive_before,
            'exitCode': witness.poll(),
            'error': cleanup_err,
            'waitedBoundedSec': 10,
        }
        ck.check('witness.killed', witness.poll() is not None,
                 'poll=%r cleanupError=%r' % (witness.poll(), cleanup_err))
        if cleanup_err:
            ck.check('witness.cleanupBounded', False, 'error=%s' % cleanup_err)

    failed = ck.failed
    skipped = ck.skipped
    report['status'] = 'PASS' if not failed else 'FAIL'
    report['checks'] = ck.rows
    report['checkCount'] = len(ck.rows)
    report['failedCount'] = len(failed)
    report['skippedCount'] = len(skipped)
    report['ended'] = proc.utc()
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or '.', exist_ok=True)
    with open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2)
    print(json.dumps({'status': report['status'], 'checks': report['checkCount'],
                      'failed': report['failedCount'], 'skipped': report['skippedCount']},
                     ensure_ascii=False))
    for f in failed:
        print('  FAILED: %s (%s)' % (f['check'], f['observed']))
    return 0 if not failed else 1


if __name__ == '__main__':
    sys.exit(main())

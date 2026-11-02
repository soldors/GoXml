# -*- coding: utf-8 -*-
r"""R8-C 命令记录入口（`record-command.py`）的**真实**自检与闭环。

真起进程、真超时、真造"父退孙活"、真中断后恢复；断言都落在**可观察事实**上
（退出码、台账字段、raw 字节与 SHA、目录是否独占、锁的归属）。进程树与 raw 捕获
一律复用 `verify_examples_proc.py`，本文件不重建进程树实现。

关键口径：**记录成功**与**被测命令业务成功**分别断言：
  * 非零退出 → 退出码 4，但 `recordOk` 仍为 True（证据完整可信）；
  * 超时 → 退出码 5，rc 是强制码，`timedOut` 必须 True，校验器判 INCOMPLETE；
  * 启动失败 → 退出码 3，pid/rc 为 None，不伪造。

用法：
  python test-record-command.py --work <空目录>
退出码：0 全绿 / 1 有红。
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
sys.path.insert(0, HERE)

RECORD = os.path.join(HERE, 'record-command.py')
VERIFY = os.path.join(HERE, 'verify-evidence.py')
SENTINEL = os.path.join(HERE, 'testdata', 'verify-proc-sentinel.py')

ROWS = []


def check(name, cond, observed=''):
    ok = bool(cond)
    ROWS.append({'check': name, 'ok': ok, 'state': 'pass' if ok else 'fail', 'observed': observed})
    if not ok:
        print('FAIL %-46s %s' % (name, observed))
    return ok


def check_eq(name, actual, expected):
    return check(name, actual == expected, 'got=%r want=%r' % (actual, expected))


def skip(name, reason):
    ROWS.append({'check': name, 'ok': None, 'skipped': reason})
    print('SKIP %-46s %s' % (name, reason))


def sha_bytes(data):
    return hashlib.sha256(data).hexdigest()


def read_bytes(path):
    with open(path, 'rb') as fh:
        return fh.read()


def ledger_lines(path):
    """逐行 parse；坏行单独计数，**不**静默丢掉。"""
    out, bad = [], []
    if not os.path.isfile(path):
        return out, bad
    for i, raw in enumerate(read_bytes(path).split(b'\n'), start=1):
        if not raw.strip():
            continue
        try:
            out.append(json.loads(raw.decode('utf-8')))
        except (ValueError, UnicodeDecodeError) as exc:
            bad.append((i, str(exc)))
    return out, bad


def find_line(lines, rec_id):
    for obj in lines:
        if obj.get('id') == rec_id:
            return obj
    return None


def run_record(args_list, timeout=120):
    argv = [sys.executable, RECORD] + list(args_list)
    proc = subprocess.run(argv, cwd=REPO, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          timeout=timeout)
    summary = None
    text = proc.stdout.decode('utf-8', 'replace')
    for line in reversed([x for x in text.splitlines() if x.strip()]):
        try:
            summary = json.loads(line)
            break
        except ValueError:
            continue
    return proc.returncode, summary, text, proc.stderr.decode('utf-8', 'replace')


def run_verify(paths, extra=None):
    argv = [sys.executable, VERIFY]
    for p in paths:
        argv += ['--jsonl', p]
    argv += extra or []
    proc = subprocess.run(argv, cwd=REPO, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          timeout=120)
    return proc.returncode, proc.stdout.decode('utf-8', 'replace'), proc.stderr.decode('utf-8', 'replace')


def write_args_file(path, argv):
    with open(path, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(argv, fh, ensure_ascii=False)
    return path


# --------------------------------------------------------------------------- 用例


def case_success(work, ledger, log_dir, py):
    rc, s, _out, err = run_record(['run', '--id', 'ok-quick', '--jsonl', ledger,
                                   '--log-dir', log_dir, '--', py, SENTINEL, 'quick'])
    check_eq('success.exit', rc, 0)
    check_eq('success.businessPass', s and s['businessPass'], True)
    check_eq('success.recordOk', s and s['recordOk'], True)
    check_eq('success.infraOk', s and s['infraOk'], True)
    check_eq('success.rc', s and s['rc'], 0)
    check_eq('success.rcSource', s and s['rcSource'], 'observed')
    line = find_line(ledger_lines(ledger)[0], 'ok-quick')
    check('success.line-present', line is not None, 'err=%r' % err[:200])
    if line:
        check_eq('success.line-phase', line['phase'], 'completed')
        check_eq('success.line-started', line['started'], True)
        check('success.raw-empty-but-hashed',
              line['raw']['stdout']['bytes'] == 0
              and line['raw']['stdout']['sha256'] == hashlib.sha256(b'').hexdigest(),
              'raw=%r' % (line['raw']['stdout'],))
        check('success.pid-present', isinstance(line.get('pid'), int) and line['pid'] > 0,
              'pid=%r' % line.get('pid'))
        check('success.pid-creation-time', isinstance(line.get('pidCreationTimeUtc'), str),
              'pidCreationTimeUtc=%r' % line.get('pidCreationTimeUtc'))
        check('success.elapsed-positive', isinstance(line.get('elapsedSec'), (int, float))
              and line['elapsedSec'] >= 0, 'elapsedSec=%r' % line.get('elapsedSec'))
        check('success.env-not-dumped', isinstance(line['env'].get('digest'), str)
              and line['env'].get('keysTotal', 0) > 0
              and len(line['env'].get('allowlist', {})) < 40,
              'envKeys=%r allow=%d' % (line['env'].get('keysTotal'),
                                       len(line['env'].get('allowlist', {}))))


def case_nonzero(work, ledger, log_dir, py):
    """记录成功 ≠ 业务成功：rc=7 必须原样保留，退出码 4，但 recordOk 仍 True。"""
    rc, s, _out, err = run_record(['run', '--id', 'nonzero', '--jsonl', ledger,
                                   '--log-dir', log_dir, '--', py, SENTINEL, 'nonzero', '7'])
    check_eq('nonzero.exit', rc, 4)
    check_eq('nonzero.recordOk', s and s['recordOk'], True)
    check_eq('nonzero.businessPass', s and s['businessPass'], False)
    check_eq('nonzero.rc-preserved', s and s['rc'], 7)
    line = find_line(ledger_lines(ledger)[0], 'nonzero')
    check('nonzero.line-kept', line is not None and line['rc'] == 7, 'err=%r' % err[:200])
    # 同一条命令换个**明示**的期望判据就应通过：判据归判据，事实归事实
    rc2, s2, _o2, _e2 = run_record(['run', '--id', 'nonzero-expect', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--expect-rc', '7',
                                    '--', py, SENTINEL, 'nonzero', '7'])
    check_eq('nonzero.expect-rc-exit', rc2, 0)
    check_eq('nonzero.expect-rc-business', s2 and s2['businessPass'], True)
    check_eq('nonzero.expect-rc-still-7', s2 and s2['rc'], 7)


def case_startup_fail(work, ledger, log_dir):
    missing = os.path.join(work, 'no-such-exe-xyz.exe')
    rc, s, _out, err = run_record(['run', '--id', 'startup-fail', '--jsonl', ledger,
                                   '--log-dir', log_dir, '--timeout', '10', '--', missing])
    check_eq('startup.exit', rc, 3)
    check_eq('startup.recordOk', s and s['recordOk'], True)
    check_eq('startup.started', s and s['started'], False)
    line = find_line(ledger_lines(ledger)[0], 'startup-fail')
    check('startup.no-fake-pid', line is not None and line.get('pid') is None,
          'pid=%r' % (line or {}).get('pid'))
    check('startup.no-fake-rc', line is not None and line.get('rc') is None,
          'rc=%r' % (line or {}).get('rc'))
    check('startup.line-written', line is not None, 'err=%r' % err[:200])


def case_timeout_partial(work, ledger, log_dir, py):
    rc, s, _out, err = run_record(['run', '--id', 'timeout', '--jsonl', ledger,
                                   '--log-dir', log_dir, '--timeout', '0.6',
                                   '--', py, SENTINEL, 'slow-print', '3'])
    check_eq('timeout.exit', rc, 5)
    check_eq('timeout.timedOut', s and s['timedOut'], True)
    check_eq('timeout.forced-rc', s and s['rc'], 8)
    check_eq('timeout.rc-source', s and s['rcSource'], 'forced-on-timeout')
    check_eq('timeout.record-still-ok', s and s['recordOk'], True)
    line = find_line(ledger_lines(ledger)[0], 'timeout')
    if line:
        raw_path = line['raw']['stdout']['path']
        raw = read_bytes(raw_path)
        check('timeout.partial-output-kept', b'tick' in raw and len(raw) > 0,
              'bytes=%d' % len(raw))
        check('timeout.sha-matches-disk',
              line['raw']['stdout']['sha256'] == sha_bytes(raw)
              and line['raw']['stdout']['bytes'] == len(raw),
              'sha=%r bytes=%r len=%d' % (line['raw']['stdout']['sha256'][:12],
                                          line['raw']['stdout']['bytes'], len(raw)))
    # 超时那条记录：校验器必须判 INCOMPLETE，**不能**判通过
    rc_v, out_v, _e = run_verify([ledger])
    check('timeout.verifier-not-pass', rc_v != 0, 'verify rc=%d' % rc_v)

    # 明示"期望超时"也不能把超时洗成干净通过：收口维度仍然不成立 → 退出码 6
    rc2, s2, _o2, _e2 = run_record(['run', '--id', 'timeout-expect', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--timeout', '0.6',
                                    '--expect-rc', '8', '--expect-timeout',
                                    '--', py, SENTINEL, 'slow-print', '3'])
    check_eq('timeout.expect-exit', rc2, 6)
    check_eq('timeout.expect-business-pass', s2 and s2['businessPass'], True)
    check_eq('timeout.expect-infra-false', s2 and s2['infraOk'], False)


def case_detach(work, ledger, log_dir, py):
    """父退孙活：整树收口必须收掉孙，不能把"父退出"当整树完成。"""
    rc, s, _out, err = run_record(['run', '--id', 'detach', '--jsonl', ledger,
                                   '--log-dir', log_dir, '--timeout', '30',
                                   '--', py, SENTINEL, 'detach', '20'])
    check_eq('detach.exit', rc, 0)
    line = find_line(ledger_lines(ledger)[0], 'detach')
    if line:
        check_eq('detach.parent-rc', line['rc'], 0)
        check_eq('detach.reaped', line.get('reapedSurvivors'), True)
        check_eq('detach.active-after', line.get('activeAfter'), 0)
        check_eq('detach.infra-ok', line.get('infraOk'), True)
    else:
        check('detach.line-present', False, 'err=%r' % err[:200])


def case_no_overwrite(work, ledger, log_dir, py):
    # 1) 同 id 再来一次：新独占目录（-<n>），旧 result.json 字节不变
    rc1, s1, _o1, _e1 = run_record(['run', '--id', 'repeat', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--', py, SENTINEL, 'quick'])
    first = find_line(ledger_lines(ledger)[0], 'repeat')
    first_bytes = read_bytes(first['resultPath'])
    first_sha = sha_bytes(first_bytes)
    rc2, s2, _o2, _e2 = run_record(['run', '--id', 'repeat', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--', py, SENTINEL, 'nonzero', '3'])
    check_eq('repeat.first-exit', rc1, 0)
    check_eq('repeat.second-exit', rc2, 4)
    check('repeat.new-dir', s2 and s2['id'] == 'repeat-2' and s2['caseDir'] != s1['caseDir'],
          'ids=%r/%r' % (s1 and s1['id'], s2 and s2['id']))
    check_eq('repeat.old-bytes-intact', sha_bytes(read_bytes(first['resultPath'])), first_sha)
    check('repeat.old-line-intact', find_line(ledger_lines(ledger)[0], 'repeat') is not None, '')

    # 2) --report 路径已存在：启动前拒绝，不覆盖；且不多写台账行
    before, _bad = ledger_lines(ledger)
    occupied = os.path.join(work, 'occupied-report.json')
    with open(occupied, 'wb') as fh:
        fh.write(b'PRE-EXISTING')
    rc3, _s3, _o3, _e3 = run_record(['run', '--id', 'occupied', '--jsonl', ledger,
                                     '--log-dir', log_dir, '--report', occupied,
                                     '--', py, SENTINEL, 'quick'])
    check_eq('occupied.exit', rc3, 2)
    check_eq('occupied.bytes-intact', read_bytes(occupied), b'PRE-EXISTING')
    after, _bad2 = ledger_lines(ledger)
    check_eq('occupied.no-new-line', len(after), len(before))

    # 3) --jsonl 指向目录：拒绝
    rc4, _s4, _o4, _e4 = run_record(['run', '--id', 'dirjsonl', '--jsonl', work,
                                     '--log-dir', log_dir, '--', py, SENTINEL, 'quick'])
    check_eq('dirjsonl.exit', rc4, 2)

    # 4) --args-file 与命令行 argv 同时给 / 非数组：都是用法错误
    af = write_args_file(os.path.join(work, 'args-bad.json'), {'not': 'array'})
    rc5, _s5, _o5, _e5 = run_record(['run', '--id', 'af-bad', '--jsonl', ledger,
                                     '--log-dir', log_dir, '--args-file', af])
    check_eq('argsfile.bad-shape-exit', rc5, 2)
    af2 = write_args_file(os.path.join(work, 'args-ok.json'), [py, SENTINEL, 'quick'])
    rc6, _s6, _o6, _e6 = run_record(['run', '--id', 'af-both', '--jsonl', ledger,
                                     '--log-dir', log_dir, '--args-file', af2,
                                     '--', py, SENTINEL, 'quick'])
    check_eq('argsfile.both-exit', rc6, 2)


def case_lock(work, ledger, log_dir, py):
    lock_dir = os.path.join(work, 'locks')
    os.makedirs(lock_dir, exist_ok=True)

    # 1) acquire：同宿主取锁 → 跑 → finally 释放
    lock_path = os.path.join(lock_dir, 'heavy.lock')
    rc, s, _o, err = run_record(['run', '--id', 'lock-acquire', '--jsonl', ledger,
                                 '--log-dir', log_dir, '--lock-path', lock_path,
                                 '--lock-mode', 'acquire', '--lock-agent', 'workbuddy',
                                 '--lock-task', 'r8-selfcheck',
                                 '--', py, SENTINEL, 'quick'])
    check_eq('lock.acquire-exit', rc, 0)
    check_eq('lock.released', s and s['lockReleased'], True)
    check_eq('lock.file-gone', os.path.exists(lock_path), False)
    line = find_line(ledger_lines(ledger)[0], 'lock-acquire')
    check('lock.mode-recorded', line and line['lock']['mode'] == 'acquire'
          and line['lock']['acquired'] is True, 'lock=%r' % (line or {}).get('lock'))
    check('lock.token-not-leaked-to-child', line and 'token' not in (line['env'].get('allowlist') or {}),
          'env allowlist keys=%r' % sorted((line or {}).get('env', {}).get('allowlist', {})))

    # 2) 他人持锁：本次**不发起**任何子进程（退出码 8），且绝不删他人锁
    foreign = {'token': 'foreign-token-abc', 'agent': 'zcode', 'task': 'R8-A-other',
               'runId': 'foreign-1', 'hostPid': os.getpid(),
               'hostIdentity': {'pid': os.getpid(), 'createFiletime': 1,
                                'identityStrength': 'pid+createFiletime'},
               'acquiredAt': '2026-11-02T00:00:00+0000', 'acquiredAtEpoch': 0}
    foreign_path = os.path.join(lock_dir, 'foreign.lock')
    with open(foreign_path, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(foreign, fh, ensure_ascii=False, indent=2)
    before_bytes = read_bytes(foreign_path)
    before_lines, _b = ledger_lines(ledger)
    rc2, s2, _o2, _e2 = run_record(['run', '--id', 'lock-busy', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--lock-path', foreign_path,
                                    '--lock-mode', 'acquire', '--', py, SENTINEL, 'quick'])
    check_eq('lock.busy-exit', rc2, 8)
    check_eq('lock.busy-no-launch', s2 and s2.get('recorded'), False)
    check_eq('lock.foreign-bytes-intact', read_bytes(foreign_path), before_bytes)
    after_lines, _a = ledger_lines(ledger)
    check_eq('lock.busy-no-ledger-line', len(after_lines), len(before_lines))
    check('lock.busy-holder-reported', s2 and ((s2.get('lockHolder') or {}).get('agent')) == 'zcode',
          'holder=%r' % (s2 or {}).get('lockHolder'))

    # 3) 外层已持锁的明确模式：只核对，不碰锁文件
    rc3, s3, _o3, _e3 = run_record(['run', '--id', 'lock-external', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--lock-path', foreign_path,
                                    '--lock-mode', 'external', '--lock-token', foreign['token'],
                                    '--', py, SENTINEL, 'quick'])
    check_eq('lock.external-exit', rc3, 0)
    check_eq('lock.external-bytes-intact', read_bytes(foreign_path), before_bytes)
    line3 = find_line(ledger_lines(ledger)[0], 'lock-external')
    check('lock.external-confirmed', line3
          and line3['lock']['mode'] == 'external'
          and line3['lock'].get('externalConfirmed') is True
          and (line3['lock'].get('holder') or {}).get('agent') == 'zcode',
          'lock=%r' % (line3 or {}).get('lock'))
    check('lock.external-token-not-copied',
          line3 and 'foreign-token-abc' not in json.dumps(line3['lock'], ensure_ascii=False),
          '不得抄他人的 token')

    # 4) 声明外层持锁却没锁文件：环境不符合声明 → 用法错误，不暗中绕锁
    rc4, _s4, _o4, _e4 = run_record(['run', '--id', 'lock-external-missing', '--jsonl', ledger,
                                     '--log-dir', log_dir,
                                     '--lock-path', os.path.join(lock_dir, 'nope.lock'),
                                     '--lock-mode', 'external', '--', py, SENTINEL, 'quick'])
    check_eq('lock.external-missing-exit', rc4, 2)

    # 5) 给了 --lock-path 却不挑明模式：拒绝（不能默认暗中绕锁）
    rc5, _s5, _o5, _e5 = run_record(['run', '--id', 'lock-mode-missing', '--jsonl', ledger,
                                     '--log-dir', log_dir,
                                     '--lock-path', os.path.join(lock_dir, 'x.lock'),
                                     '--', py, SENTINEL, 'quick'])
    check_eq('lock.mode-missing-exit', rc5, 2)


TRICKY = ['', 'a b', '中文', 'q"q', 'C:\\tail\\']


def case_fidelity(work, ledger, log_dir, py):
    # 参数保真：直接 argv（不经 shell）
    rc, s, _o, err = run_record(['run', '--id', 'argv-direct', '--jsonl', ledger,
                                 '--log-dir', log_dir, '--', py, SENTINEL, 'echo-args']
                                + TRICKY)
    check_eq('argv.direct-exit', rc, 0)
    line = find_line(ledger_lines(ledger)[0], 'argv-direct')
    if line:
        got = json.loads(read_bytes(line['raw']['stdout']['path']).decode('utf-8').strip())
        check_eq('argv.direct-roundtrip', got, TRICKY)
        check_eq('argv.recorded-exact', line['argv'][3:], TRICKY)

    # 参数保真：--args-file（跨宿主推荐路径）
    af = write_args_file(os.path.join(work, 'args-tricky.json'),
                         [py, SENTINEL, 'echo-args'] + TRICKY)
    rc2, s2, _o2, _e2 = run_record(['run', '--id', 'argv-file', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--args-file', af])
    check_eq('argv.file-exit', rc2, 0)
    line2 = find_line(ledger_lines(ledger)[0], 'argv-file')
    if line2:
        got2 = json.loads(read_bytes(line2['raw']['stdout']['path']).decode('utf-8').strip())
        check_eq('argv.file-roundtrip', got2, TRICKY)

    # 环境保真：只在子进程局部；父环境不变
    over = {'GOXML_R8_EMPTY': '', 'GOXML_R8_SPACE': 'a b', 'GOXML_R8_CN': '中文',
            'GOXML_R8_QUOTE': 'q"q\\'}
    env_args = []
    for k in sorted(over):
        env_args += ['--env', '%s=%s' % (k, over[k])]
    parent_before = dict(os.environ)
    rc3, s3, _o3, _e3 = run_record(['run', '--id', 'env', '--jsonl', ledger,
                                    '--log-dir', log_dir, '--go-env',
                                    '--env-key', 'GOXML_R8_CN'] + env_args +
                                   ['--', py, SENTINEL, 'echo-env'] + sorted(over))
    check_eq('env.exit', rc3, 0)
    line3 = find_line(ledger_lines(ledger)[0], 'env')
    if line3:
        got3 = json.loads(read_bytes(line3['raw']['stdout']['path']).decode('utf-8').strip())
        check_eq('env.roundtrip', got3, over)
        check_eq('env.parent-unchanged', dict(os.environ), parent_before)
        check('env.not-leaked', 'GOXML_R8_CN' not in os.environ, '')
        allow = line3['env'].get('allowlist') or {}
        check('env.go-fixed-recorded', allow.get('CGO_ENABLED') == '0'
              and allow.get('GOMAXPROCS') == '2' and allow.get('GOPROXY') == 'off',
              'allow=%r' % sorted(allow))
        check('env.overrides-recorded',
              (line3['env'].get('overrides') or {}).get('GOXML_R8_CN') == '中文',
              'overrides=%r' % (line3['env'].get('overrides'),))


def case_ps_hosts(work, ledger, log_dir, py):
    """只测本机**已有**的宿主，不安装；PS 传参用 --args-file 保真。"""
    hosts = []
    for label, exe in (('ps51', 'powershell.exe'), ('ps7', 'pwsh.exe')):
        found = shutil.which(exe)
        if found:
            hosts.append((label, found))
    if not hosts:
        skip('ps.hosts', '本机没有 powershell.exe / pwsh.exe')
        return
    af = write_args_file(os.path.join(work, 'args-ps.json'),
                         [py, SENTINEL, 'echo-args'] + TRICKY)
    for label, exe in hosts:
        # 宿主外面再包一层：PS 只负责把 --args-file 传进来，argv 的真实内容由 JSON 文件决定
        script = ("& '%s' '%s' run --id ps-%s --jsonl '%s' --log-dir '%s' "
                  "--args-file '%s'" % (sys.executable, RECORD, label, ledger, log_dir, af))
        proc = subprocess.run([exe, '-NoProfile', '-Command', script], cwd=REPO,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
        out = proc.stdout.decode('utf-8', 'replace')
        summary = None
        for line in reversed([x for x in out.splitlines() if x.strip()]):
            try:
                summary = json.loads(line)
                break
            except ValueError:
                continue
        check('%s.exit' % label, proc.returncode == 0 and summary is not None
              and summary.get('exitCode') == 0,
              'rc=%d summary=%r stderr=%r' % (proc.returncode, summary,
                                              proc.stderr.decode('utf-8', 'replace')[:160]))
        line = find_line(ledger_lines(ledger)[0], 'ps-%s' % label)
        if line:
            got = json.loads(read_bytes(line['raw']['stdout']['path']).decode('utf-8').strip())
            check_eq('%s.argv-roundtrip' % label, got, TRICKY)


def _wait_for_planned(run_dir, timeout=20.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if os.path.isfile(os.path.join(run_dir, 'planned.json')):
            return True
        time.sleep(0.05)
    return False


def case_soft_interrupt(work, ledger, log_dir, py):
    """软中断：要么按有界收口完成并落盘，要么被判成未知——两种都**有界**且可定位。

    信号是否真的被 Python 层接住取决于阻塞点，所以这里不断言"一定走哪条路"，
    只断言"一定有结果、有界、不重复"。
    """
    if os.name != 'nt':
        skip('soft-interrupt', '非 Windows：CTRL_BREAK_EVENT 不适用')
        return
    import signal

    base = os.path.join(log_dir, 'intr')
    argv = [sys.executable, RECORD, 'run', '--id', 'intr', '--jsonl', ledger,
            '--log-dir', log_dir, '--timeout', '6', '--', py, SENTINEL, 'grand', '30']
    flags = getattr(subprocess, 'CREATE_NEW_PROCESS_GROUP', 0)
    proc = subprocess.Popen(argv, cwd=REPO, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            creationflags=flags)
    try:
        got_planned = _wait_for_planned(base)
        check('soft.planned-written', got_planned, 'waited for %s' % base)
        time.sleep(0.3)
        try:
            proc.send_signal(signal.CTRL_BREAK_EVENT)
            time.sleep(0.3)
            proc.send_signal(signal.CTRL_BREAK_EVENT)
        except (OSError, ValueError, AttributeError) as exc:
            check('soft.signal-sent', False, 'send_signal failed: %s' % exc)
        t0 = time.monotonic()
        try:
            _out, _err = proc.communicate(timeout=45)
        except subprocess.TimeoutExpired:
            proc.kill()
            try:
                proc.communicate(timeout=20)
            except subprocess.TimeoutExpired:
                pass
            check('soft.bounded-exit', False, '45s 内没退出')
        dt = time.monotonic() - t0
        check('soft.exited-bounded', proc.returncode is not None and dt < 45,
              'rc=%r elapsed=%.1fs' % (proc.returncode, dt))
    finally:
        if proc.poll() is None:
            proc.kill()
            try:
                proc.communicate(timeout=20)
            except subprocess.TimeoutExpired:
                pass

    lines, _bad = ledger_lines(ledger)
    line = find_line(lines, 'intr')
    if line is None:
        # 没写成行（硬终止路径）：用 recover 补上，且必须补成可定位的形态
        rc_r, _s_r, _o_r, _e_r = run_record(['recover', '--run-dir', base, '--jsonl', ledger])
        check('soft.recover-exit', rc_r in (0, 3, 4, 5, 6, 7), 'rc=%d' % rc_r)
        line = find_line(ledger_lines(ledger)[0], 'intr')
    check('soft.line-present', line is not None, '')
    if line:
        check('soft.phase-known', line['phase'] in ('completed', 'recovered', 'unknown'),
              'phase=%r' % line['phase'])
        if line.get('timedOut') is True:
            check_eq('soft.timeout-rc-source', line['rcSource'], 'forced-on-timeout')
        check_eq('soft.no-duplicate',
                 len([x for x in ledger_lines(ledger)[0] if x.get('id') == 'intr']), 1)


def case_hard_kill_recover(work, ledger, log_dir, py):
    """硬终止：**不能**承诺 finally 落盘；留下 prepared 可定位，recover 补成 unknown。"""
    base = os.path.join(log_dir, 'kill')
    argv = [sys.executable, RECORD, 'run', '--id', 'kill', '--jsonl', ledger,
            '--log-dir', log_dir, '--timeout', '60', '--', py, SENTINEL, 'grand', '30']
    # 不用管道：孙进程可能把管道句柄继承下去，kill 后 communicate 会一直等 EOF。
    proc = subprocess.Popen(argv, cwd=REPO, stdout=subprocess.DEVNULL,
                            stderr=subprocess.DEVNULL)
    got_planned = _wait_for_planned(base)
    check('kill.planned-written', got_planned, 'waited for %s' % base)
    proc.kill()  # 只杀**自己**起的这个子进程
    try:
        proc.wait(timeout=30)
    except subprocess.TimeoutExpired:
        proc.kill()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            pass
        check('kill.bounded-exit', False, 'kill 后 30s 内没退出')

    before, _b = ledger_lines(ledger)
    check_eq('kill.no-line-yet', find_line(before, 'kill'), None)
    check('kill.planned-legible', os.path.isfile(os.path.join(base, 'planned.json')), '')
    planned = json.loads(read_bytes(os.path.join(base, 'planned.json')).decode('utf-8'))
    check_eq('kill.planned-phase', planned.get('phase'), 'prepared')
    check('kill.planned-has-argv', planned.get('argv')[-1] in ('30', 'grand'),
          'argv=%r' % (planned.get('argv'),))

    rc_r, s_r, _o_r, err_r = run_record(['recover', '--run-dir', base, '--jsonl', ledger])
    check_eq('kill.recover-exit', rc_r, 3)  # 结果未知 → 退出码 3
    after, bad = ledger_lines(ledger)
    check_eq('kill.no-bad-lines', bad, [])
    line = find_line(after, 'kill')
    check('kill.line-present', line is not None, 'err=%r' % err_r[:200])
    if line:
        check_eq('kill.phase-unknown', line['phase'], 'unknown')
        check_eq('kill.no-fake-started', line['started'], None)
        check_eq('kill.no-fake-rc', line['rc'], None)
        check_eq('kill.no-fake-time', line.get('elapsedSec'), None)
        check_eq('kill.record-ok-false', line['recordOk'], False)
        check_eq('kill.launch-outcome', line.get('launchOutcome'), 'unknown')
    check_eq('kill.one-line-only', len([x for x in after if x.get('id') == 'kill']), 1)

    # unknown 形态：校验器必须判 INCOMPLETE，**不能**判通过
    single = os.path.join(work, 'kill-only.jsonl')
    with open(single, 'wb') as fh:
        fh.write(json.dumps(line, ensure_ascii=False, sort_keys=True,
                            separators=(',', ':')).encode('utf-8') + b'\n')
    rc_v, out_v, _e_v = run_verify([single])
    check_eq('kill.verifier-incomplete', rc_v, 3)
    check('kill.verifier-text', 'verdict=INCOMPLETE' in out_v, 'out=%r' % out_v[:160])

    # 重复 recover：不再写第二行
    rc_r2, s_r2, _o2, _e2 = run_record(['recover', '--run-dir', base, '--jsonl', ledger])
    check_eq('kill.recover-idempotent-exit', rc_r2, 0)
    check_eq('kill.recover-idempotent-skipped', s_r2 and s_r2.get('skipped'), True)
    check_eq('kill.still-one-line',
             len([x for x in ledger_lines(ledger)[0] if x.get('id') == 'kill']), 1)


def case_repo_narrow_check(work, ledger, log_dir, py):
    """自用闭环：用本入口记录一条**本仓**只读窄检查，再由校验器验证。"""
    lock_target = os.path.join(work, 'locks', 'status-probe.lock')
    rc, s, _o, err = run_record(['run', '--id', 'repo-lock-status', '--jsonl', ledger,
                                 '--log-dir', log_dir, '--', py,
                                 os.path.join(HERE, 'verify-examples-lock.py'),
                                 'status', '--lock-path', lock_target])
    check_eq('repo.exit', rc, 0)
    line = find_line(ledger_lines(ledger)[0], 'repo-lock-status')
    if line:
        raw = read_bytes(line['raw']['stdout']['path'])
        parsed = json.loads(raw.decode('utf-8').strip())
        check_eq('repo.stdout-exists-false', parsed.get('exists'), False)
        check_eq('repo.sha-matches', line['raw']['stdout']['sha256'], sha_bytes(raw))
    single = os.path.join(work, 'repo-only.jsonl')
    # 把这一行单独拷出来做"只含健康记录"的校验，必须 PASS
    with open(single, 'wb') as fh:
        fh.write(json.dumps(line, ensure_ascii=False, sort_keys=True,
                            separators=(',', ':')).encode('utf-8') + b'\n')
    rc_v, out_v, err_v = run_verify([single])
    check_eq('repo.verifier-pass', rc_v, 0)
    check('repo.verifier-text', 'verdict=PASS' in out_v, 'out=%r err=%r' % (out_v[:160], err_v[:160]))


def case_final_ledger(work, ledger, log_dir):
    """收尾：整份台账逐行 parse，FAIL 必须为 0（可以有 INCOMPLETE）。"""
    lines, bad = ledger_lines(ledger)
    check_eq('final.no-bad-lines', bad, [])
    rc, out, err = run_verify([ledger], ['--json', os.path.join(work, 'final-verify.json')])
    check('final.verdict-not-fail', rc in (0, 3), 'rc=%d out=%r err=%r' % (rc, out[:200], err[:200]))
    check('final.no-duplicate-id', 'duplicate-id' not in out, 'out=%r' % out[:400])
    check('final.has-lines', len(lines) >= 10, 'lines=%d' % len(lines))
    # 台账字节：CRLF 必须为 0（O_BINARY 写入），且每行以 LF 结尾
    raw = read_bytes(ledger)
    check_eq('final.no-crlf', raw.count(b'\r'), 0)
    check('final.ends-with-lf', raw.endswith(b'\n'), 'last=%r' % raw[-3:])
    # 每一行的 raw SHA 都由校验器按磁盘重算过了；这里再独立抽验一条
    checked = 0
    for obj in lines:
        for stream in ('stdout', 'stderr'):
            spec = (obj.get('raw') or {}).get(stream)
            if not spec or not os.path.isfile(spec['path']):
                continue
            disk = read_bytes(spec['path'])
            if spec['sha256'] != sha_bytes(disk) or spec['bytes'] != len(disk):
                check('final.raw-mismatch:%s' % obj['id'], False,
                      '%s %r vs disk %d' % (stream, spec['sha256'][:12], len(disk)))
            checked += 1
    check('final.raw-rehashed', checked > 0, 'rehashed=%d' % checked)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True)
    args = ap.parse_args()
    work = os.path.abspath(args.work)
    if os.path.isdir(work):
        shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work, exist_ok=True)
    log_dir = os.path.join(work, 'logs')
    ledger = os.path.join(work, 'commands.jsonl')
    py = sys.executable

    # 见证进程：不在被测 job 里，任何收口都不该影响它；收口放在**外层 finally**。
    witness = subprocess.Popen([py, SENTINEL, 'grand', '180'])
    try:
        case_success(work, ledger, log_dir, py)
        case_nonzero(work, ledger, log_dir, py)
        case_startup_fail(work, ledger, log_dir)
        case_timeout_partial(work, ledger, log_dir, py)
        case_detach(work, ledger, log_dir, py)
        case_no_overwrite(work, ledger, log_dir, py)
        case_lock(work, ledger, log_dir, py)
        case_fidelity(work, ledger, log_dir, py)
        case_ps_hosts(work, ledger, log_dir, py)
        case_soft_interrupt(work, ledger, log_dir, py)
        case_hard_kill_recover(work, ledger, log_dir, py)
        case_repo_narrow_check(work, ledger, log_dir, py)
        check('witness.alive-during', witness.poll() is None, 'poll=%r' % witness.poll())
        case_final_ledger(work, ledger, log_dir)
    except BaseException as exc:
        check('harness.completed', False, 'exception=%s: %s' % (type(exc).__name__, exc))
    finally:
        cleanup_err = None
        try:
            if witness.poll() is None:
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
        check('witness.killed', witness.poll() is not None,
              'poll=%r cleanupError=%r' % (witness.poll(), cleanup_err))
        if cleanup_err:
            check('witness.cleanup-bounded', False, 'error=%s' % cleanup_err)

    failed = [r for r in ROWS if r['ok'] is False]
    skipped = [r for r in ROWS if r['ok'] is None]
    print(json.dumps({'status': 'PASS' if not failed else 'FAIL', 'checks': len(ROWS),
                      'failed': len(failed), 'skipped': len(skipped),
                      'ledger': ledger}, ensure_ascii=False))
    for f in failed:
        print('  FAILED: %s (%s)' % (f['check'], f['observed']))
    return 0 if not failed else 1


if __name__ == '__main__':
    sys.exit(main())

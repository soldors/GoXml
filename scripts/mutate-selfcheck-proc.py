# -*- coding: utf-8 -*-
r"""R6-C 自检的**红判据**：证明每条断言真的会翻红，不是恒真装饰。

做法：把 `verify_examples_proc.py` / `verify_examples_proctree.py` /
`selfcheck-verify-examples-proc.py` / `testdata/verify-proc-sentinel.py`
复制到 `<work>/<mutation>/scripts/`，**只在副本上**做一处文本替换（注入一种退化实现），
再跑副本的自检，检查它是否在**预期的断言**上失败。

不修改仓库里的源文件；运行前后对源文件做 SHA-256 比对，证明"源文件字节未变"。

每个变异都对应任务书里一条明令禁止的退化：
  M1 no-reap          父退出就算完，不查 ActiveProcesses       → 超时树收口失败
  M2 active-blind     ActiveProcesses 恒读 0，看不见孙进程      → 父退孙活检测失败
  M3 rc-before-kill   终止前读退出码（本轮真踩到的 bug）        → 强制退出码来源错
  M4 parent-only-kill 只 TerminateProcess(父)，不 TerminateJob  → 整树收口失败
  M5 display-hash    把解码后的展示文本写回 raw 再算 SHA        → raw 不再权威
  M6 reuse-dir        case 目录复用（覆盖历史字节）             → 历史证据被覆盖
  M7 ok-always-true    ok 判据硬编码为真（清理失败仍报成功）    → 故障态被当成成功
  M8 assign-silent    归属失败不置 failClosed                   → 静默退化不 fail-closed

用法：
  python mutate-selfcheck-proc.py --work <目录> --out <report.json> [--only M3,M5] [--keep]
"""
import argparse
import hashlib
import io
import json
import os
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
FILES = ['verify_examples_proc.py', 'verify_examples_proctree.py',
         'selfcheck-verify-examples-proc.py',
         os.path.join('testdata', 'verify-proc-sentinel.py')]

# (mut_id, 说明, 目标文件, 原文, 替换, 预期失败的断言名集合)
MUTATIONS = [
    ('M1', 'no-reap: 父退出即认为结束，不查 ActiveProcesses，不收口孙进程',
     'verify_examples_proctree.py',
     "        need_reap = result['timedOut'] or active is None or active > 0",
     "        need_reap = result['timedOut']",
     {'detach.reapedSurvivors', 'detach.activeAfter0', 'detach.ok'}),

    ('M2', 'active-blind: ActiveProcesses 恒读 0，看不见孙进程',
     'verify_examples_proctree.py',
     "    if not _k32.QueryInformationJobObject(job, JOB_OBJECT_BASIC_ACCOUNTING_INFORMATION,",
     "    if True:\n        return 0\n    if not _k32.QueryInformationJobObject(job, JOB_OBJECT_BASIC_ACCOUNTING_INFORMATION,",
     {'detach.reapedSurvivors'}),

    ('M3', 'rc-before-kill: 终止前读退出码，只能读到 STILL_ACTIVE',
     'verify_examples_proctree.py',
     "        elif result['timedOut'] and terminated:",
     "        elif result['timedOut'] and not terminated:",
     {'timeout.forcedRc'}),

    ('M4', 'parent-only-kill: 只杀父 PID，不用 job 终止整树',
     'verify_examples_proctree.py',
     "            elif not _k32.TerminateJobObject(job, 8):",
     "            elif not _k32.TerminateProcess(pi.hProcess, 8):",
     {'timeout.activeAfter0', 'timeout.record-trustworthy'}),

    ('M5', 'display-hash: 把解码展示文本写回 raw，decode 变成权威',
     'verify_examples_proc.py',
     "        result['displayTail'] = decode_for_display(merged)",
     "        result['displayTail'] = decode_for_display(merged)\n"
     "        with open(out_path, 'wb') as _fw:\n"
     "            _fw.write(result['displayTail']['tail'].encode('utf-8', 'replace'))",
     {'binary.sha-matches-bytes', 'binary.display-not-authoritative'}),

    ('M6', 'reuse-dir: case 目录复用，覆盖历史字节',
     'verify_examples_proc.py',
     "        path = os.path.join(base, name)\n        try:\n            os.makedirs(path, exist_ok=False)\n            return path, name\n        except FileExistsError:\n            continue",
     "        path = os.path.join(base, case_id)\n        os.makedirs(path, exist_ok=True)\n        return path, case_id",
     {'repeat.newDir', 'repeat.oldBytesIntact'}),

    ('M7', 'ok-always-true: compute_ok 硬编码为真（"清理失败仍报成功"的极端形态）',
     'verify_examples_proc.py',
     "    reasons = []\n",
     "    reasons = []\n    return True, []\n",
     {'cleanup.notOk', 'startup.failClosed', 'assign.failClosed'}),

    ('M8', 'assign-silent: 归属失败不置 failClosed，静默退化',
     'verify_examples_proctree.py',
     "            result['failClosed'] = True\n            # 绝不 resume",
     "            # 绝不 resume",
     {'assign.failClosed'}),

    # M9 专测"见证进程收口在外层 finally"：在检查中途抛异常，见证必须仍被有界收口。
    ('M9', 'witness-cleanup-on-exception: 检查中途抛异常，见证仍须有界收口',
     'selfcheck-verify-examples-proc.py',
     "        r = run('binary', [py, SENTINEL, 'binary'], timeout=20)",
     "        raise RuntimeError('injected-mid-check')",
     {'harness.completed'}),
]

# 需要额外核对的变异（断言集合之外的事实）
EXTRA = {'M9': 'witness-bounded-exception'}


def sha(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for c in iter(lambda: fh.read(1 << 20), b''):
            h.update(c)
    return h.hexdigest()


def prepare(root):
    """把工具复制到 <root>/scripts/（含 testdata），返回 scripts 目录。"""
    dst = os.path.join(root, 'scripts')
    os.makedirs(os.path.join(dst, 'testdata'), exist_ok=True)
    for rel in FILES:
        s = os.path.join(HERE, rel)
        d = os.path.join(dst, rel)
        os.makedirs(os.path.dirname(d), exist_ok=True)
        shutil.copyfile(s, d)
    return dst


def run_one(mut, work, stamp):
    mid, desc, target, old, new, expect = mut
    # 每次用新目录，**不删**任何既有证据（删除大批文件会被安全护栏拦下，也不该覆盖历史）。
    root = os.path.join(work, '%s-%s' % (mid, stamp))
    scripts = prepare(root)
    tpath = os.path.join(scripts, target)
    src = io.open(tpath, encoding='utf-8').read()
    hits = src.count(old)
    if hits != 1:
        return {'mutation': mid, 'ok': False, 'error': '锚点命中 %d 次（要求恰好 1 次）' % hits}
    io.open(tpath, 'w', encoding='utf-8', newline='\n').write(src.replace(old, new, 1))

    out_json = os.path.join(root, 'selfcheck.json')
    case_work = os.path.join(root, 'cases')
    argv = [sys.executable, os.path.join(scripts, 'selfcheck-verify-examples-proc.py'),
            '--work', case_work, '--out', out_json]
    t0 = time.monotonic()
    proc = subprocess.run(argv, cwd=scripts, stdin=subprocess.DEVNULL,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=900)
    dt = round(time.monotonic() - t0, 2)
    raw_out = os.path.join(root, 'selfcheck.out')
    raw_err = os.path.join(root, 'selfcheck.err')
    with open(raw_out, 'wb') as fh:
        fh.write(proc.stdout)
    with open(raw_err, 'wb') as fh:
        fh.write(proc.stderr)

    failed, status, checks = set(), None, 0
    if os.path.isfile(out_json):
        rep = json.load(io.open(out_json, encoding='utf-8'))
        status = rep.get('status')
        checks = rep.get('checkCount')
        failed = {r['check'] for r in rep.get('checks', []) if r.get('ok') is False}
    missing = sorted(expect - failed)
    row = {
        'mutation': mid, 'desc': desc, 'target': target,
        'expectedRed': sorted(expect), 'actualRed': sorted(failed),
        'missingRed': missing, 'selfcheckStatus': status, 'checks': checks,
        'selfcheckRc': proc.returncode, 'elapsedSec': dt,
        'stdoutPath': raw_out, 'stderrPath': raw_err, 'resultJson': out_json,
        'stdoutBytes': len(proc.stdout), 'stderrBytes': len(proc.stderr),
        'targetSha': sha(tpath),
        'ok': bool(missing == [] and proc.returncode != 0),
    }

    # M9 额外事实：异常下见证仍被收口（exitCode 非 None）+ 全程有界
    if EXTRA.get(mid) == 'witness-bounded-exception':
        rep = {}
        if os.path.isfile(out_json):
            rep = json.load(io.open(out_json, encoding='utf-8'))
        wc = rep.get('witnessCleanup') or {}
        row['witnessCleanup'] = wc
        row['exceptionRecorded'] = rep.get('exception')
        witness_ok = (wc.get('exitCode') is not None) and bool(rep.get('exception')) and dt < 60
        row['witnessBoundedOnException'] = witness_ok
        row['ok'] = bool(row['ok'] and witness_ok)
    return row


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--only', default='')
    args = ap.parse_args()

    work = os.path.abspath(args.work)
    os.makedirs(work, exist_ok=True)
    only = [s.strip() for s in args.only.split(',') if s.strip()]
    muts = [m for m in MUTATIONS if not only or m[0] in only]

    before = {rel: sha(os.path.join(HERE, rel)) for rel in FILES}
    rows = []
    stamp = time.strftime('%H%M%S')
    for m in muts:
        row = run_one(m, work, stamp)
        rows.append(row)
        print('%-4s %s  expectedRed=%s actualRed=%s rc=%s %ss'
              % (row['mutation'], 'PASS' if row.get('ok') else 'FAIL',
                 row.get('expectedRed'), row.get('actualRed'),
                 row.get('selfcheckRc'), row.get('elapsedSec')))
    after = {rel: sha(os.path.join(HERE, rel)) for rel in FILES}

    report = {
        'generatedUtc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        'python': sys.executable, 'work': work,
        'sourceShaBefore': before, 'sourceShaAfter': after,
        'sourceUnchanged': before == after,
        'mutationCount': len(rows),
        'passed': sum(1 for r in rows if r.get('ok')),
        'failed': [r['mutation'] for r in rows if not r.get('ok')],
        'mutations': rows,
    }
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or '.', exist_ok=True)
    with open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2)
    print(json.dumps({'mutations': report['mutationCount'], 'passed': report['passed'],
                      'failed': report['failed'],
                      'sourceUnchanged': report['sourceUnchanged']}, ensure_ascii=False))
    return 0 if report['passed'] == report['mutationCount'] and report['sourceUnchanged'] else 1


if __name__ == '__main__':
    sys.exit(main())

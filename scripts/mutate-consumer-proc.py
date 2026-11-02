# -*- coding: utf-8 -*-
r"""消费点测试的**红判据**：逐条削弱新判据，确认 mock 测试恰好在对应断言翻红。

只改 `<work>/<mutation>/scripts/` 里的副本，不动仓库源文件；前后核源文件 SHA-256。

  C1 ok-drop-closeErrors    删掉 closeErrors 判据     → ok.poison.close-errors-only 红
  C2 ok-active-lax          activeAfter 放宽到 in (0,None) → ok.poison.active-none 红
  C3 ok-drop-contained      删掉 contained 判据       → ok.poison.contained-false 红
  C4 ok-drop-restricted     删掉 restrictedSafe 判据  → ok.poison.restricted-unsafe 红
  C5 ok-stillrunning-loose  stillRunning 用真值判断   → ok.poison.still-running-none 红
  C6 auto-launches-when-unsupported  auto 在非 Windows 走 none → 拒绝类断言红
  C7 timeout-zero-allowed   timeout=0 当合法         → fakek32.timeout-zero-refused 红

用法：python mutate-consumer-proc.py --work <目录> --out <report.json> [--only C2,C6]
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
         'verify_examples_selcase.py',
         'test-verify-examples-proc-consumer.py',
         os.path.join('testdata', 'verify-proc-sentinel.py')]

MUTATIONS = [
    ('C1', 'ok-drop-closeErrors: 删掉 closeErrors 判据', 'verify_examples_proc.py',
     "    if result.get('closeErrors') != []:\n        reasons.append('close-errors')\n",
     "",
     {'ok.poison.close-errors-only'}),

    ('C2', 'ok-active-lax: 活动数放宽到 in (0, None)', 'verify_examples_proc.py',
     "    if not (type(active) is int and active == 0):",
     "    if active not in (0, None):",
     {'ok.poison.active-none'}),

    ('C3', 'ok-drop-contained: 删掉 contained 判据', 'verify_examples_proc.py',
     "    if result.get('contained') is not True:\n        reasons.append('not-contained')\n",
     "",
     {'ok.poison.contained-false'}),

    ('C4', 'ok-drop-restricted: 删掉 restrictedSafe 判据', 'verify_examples_proc.py',
     "    if result.get('restrictedSafe') is False:\n        reasons.append('not-restricted-safe')\n",
     "",
     {'ok.poison.restricted-unsafe'}),

    ('C5', 'ok-stillrunning-loose: stillRunning 用真值判断', 'verify_examples_proc.py',
     "    if result.get('stillRunning') is not False:",
     "    if result.get('stillRunning'):",
     {'ok.poison.still-running-none'}),

    ('C6', 'auto-silent-on-nonwindows: 非 Windows 的 auto 静默走 none',
     'verify_examples_proc.py',
     "    if os.name != 'nt':\n        return CONTAINMENT_REFUSED, 'unsupported-platform: process-tree containment is Windows-only (os.name=%r)' % os.name\n",
     "    if os.name != 'nt':\n        return CONTAINMENT_NONE, 'silently-degraded'\n",
     {'refuse.nonwindows-auto-mode'}),

    ('C6b', 'tree-unavailable-degrades: Job 能力不可用时静默走 none（本机可复现）',
     'verify_examples_proc.py',
     "    if mod is None:\n        return CONTAINMENT_REFUSED, 'tree-unavailable: %s' % reason\n",
     "    if mod is None:\n        return CONTAINMENT_NONE, 'silently-degraded: %s' % reason\n",
     {'refuse.auto-popen-zero'}),

    ('C7', 'timeout-zero-allowed: timeout<=0 被当成合法值', 'verify_examples_proctree.py',
     "    if val <= 0:\n        return False, None, 'timeout must be > 0 seconds (got %r); infinite wait is not allowed' % (timeout,)\n",
     "    if val <= 0:\n        return True, 1, None\n",
     {'fakek32.timeout-zero-refused', 'fakek32.timeout-negative-refused'}),

    ('C8', 'gate-ignores-infra: case_passed 忽略 infraOk（上层门失效）', 'verify_examples_selcase.py',
     "    return bool(row.get('infraOk') is True and business is True)",
     "    return bool(business is True)",
     {'gate.infra-fail-business-true', 'gate.infra-missing'}),
]


def sha(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for c in iter(lambda: fh.read(1 << 20), b''):
            h.update(c)
    return h.hexdigest()


def prepare(root):
    dst = os.path.join(root, 'scripts')
    os.makedirs(os.path.join(dst, 'testdata'), exist_ok=True)
    for rel in FILES:
        shutil.copyfile(os.path.join(HERE, rel), os.path.join(dst, rel))
    return dst


def run_one(mut, work, stamp):
    mid, desc, target, old, new, expect = mut
    root = os.path.join(work, '%s-%s' % (mid, stamp))  # 每次新目录，不删旧证据
    scripts = prepare(root)
    tpath = os.path.join(scripts, target)
    src = io.open(tpath, encoding='utf-8').read()
    hits = src.count(old)
    if hits != 1:
        return {'mutation': mid, 'ok': False, 'error': 'anchor hits=%d' % hits}
    io.open(tpath, 'w', encoding='utf-8', newline='\n').write(src.replace(old, new, 1))

    out_json = os.path.join(root, 'consumer.json')
    argv = [sys.executable, os.path.join(scripts, 'test-verify-examples-proc-consumer.py'),
            '--work', os.path.join(root, 'w'), '--out', out_json]
    t0 = time.monotonic()
    cp = subprocess.run(argv, cwd=scripts, stdin=subprocess.DEVNULL,
                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=900)
    dt = round(time.monotonic() - t0, 2)
    out_path = os.path.join(root, 'consumer.out')
    err_path = os.path.join(root, 'consumer.err')
    open(out_path, 'wb').write(cp.stdout)
    open(err_path, 'wb').write(cp.stderr)

    failed, status = set(), None
    if os.path.isfile(out_json):
        rep = json.load(io.open(out_json, encoding='utf-8'))
        status = rep.get('status')
        failed = {r['check'] for r in rep.get('checks', []) if r.get('ok') is False}
    missing = sorted(expect - failed)
    return {'mutation': mid, 'desc': desc, 'expectedRed': sorted(expect),
            'actualRed': sorted(failed), 'missingRed': missing,
            'consumerStatus': status, 'consumerRc': cp.returncode, 'elapsedSec': dt,
            'stdoutPath': out_path, 'stderrPath': err_path, 'resultJson': out_json,
            'ok': bool(missing == [] and cp.returncode != 0)}


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
    stamp = time.strftime('%H%M%S')

    before = {rel: sha(os.path.join(HERE, rel)) for rel in FILES}
    rows = []
    for m in muts:
        row = run_one(m, work, stamp)
        rows.append(row)
        print('%-3s %s expectedRed=%s actualRed=%s rc=%s %ss'
              % (row['mutation'], 'PASS' if row.get('ok') else 'FAIL',
                 row.get('expectedRed'), row.get('actualRed'),
                 row.get('consumerRc'), row.get('elapsedSec')))
    after = {rel: sha(os.path.join(HERE, rel)) for rel in FILES}

    report = {'generatedUtc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
              'python': sys.executable, 'work': work,
              'sourceShaBefore': before, 'sourceShaAfter': after,
              'sourceUnchanged': before == after,
              'mutationCount': len(rows),
              'passed': sum(1 for r in rows if r.get('ok')),
              'failed': [r['mutation'] for r in rows if not r.get('ok')],
              'mutations': rows}
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or '.', exist_ok=True)
    with io.open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2)
    print(json.dumps({'mutations': report['mutationCount'], 'passed': report['passed'],
                      'failed': report['failed'], 'sourceUnchanged': report['sourceUnchanged']},
                     ensure_ascii=False))
    return 0 if report['passed'] == report['mutationCount'] and report['sourceUnchanged'] else 1


if __name__ == '__main__':
    sys.exit(main())

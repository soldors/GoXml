#!/usr/bin/env python
"""Independent regressions for R8 recorder review defects; Python sentinels only."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--out', default=tempfile.gettempdir())
    args = ap.parse_args()
    root = Path(args.out) / ('recorder-review-' + str(time.time_ns()))
    root.mkdir(parents=True)
    checks, calls = [], []

    def check(name, value):
        checks.append({'name': name, 'pass': bool(value)})

    def call(extra):
        argv = [sys.executable, str(HERE / 'record-command.py')] + extra
        start = time.time()
        p = subprocess.run(argv, capture_output=True,
                           env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'), timeout=20)
        n = len(calls) + 1
        (root / ('%02d.stdout.raw' % n)).write_bytes(p.stdout)
        (root / ('%02d.stderr.raw' % n)).write_bytes(p.stderr)
        calls.append({'argv': argv, 'rc': p.returncode, 'startedEpoch': start,
                      'endedEpoch': time.time(), 'stdoutSHA': hashlib.sha256(p.stdout).hexdigest(),
                      'stderrSHA': hashlib.sha256(p.stderr).hexdigest()})
        return p

    def run(name, extra=()):
        return call(['run', '--id', name, '--jsonl', str(root / 'ledger.jsonl'),
                     '--log-dir', str(root / 'runs')] + list(extra) +
                    ['--', sys.executable, '-c', 'print("ORIGINAL")'])

    spec = importlib.util.spec_from_file_location('review_recorder', HERE / 'record-command.py')
    rec = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(rec)
    lockmod = rec.load_lock_helper()
    lockpath = root / 'own.lock'
    existing = root / 'existing.json'
    existing.write_bytes(b'USER')
    owned = ['--lock-path', str(lockpath), '--lock-mode', 'acquire']
    for name, extra in [('report', ['--report', str(existing)]),
                        ('cwd', ['--cwd', str(root / 'missing')]),
                        ('timeout', ['--timeout', '0']),
                        ('env', ['--env', 'BAD'])]:
        p = run('invalid-' + name, owned + extra)
        check(name + '-usage-failure', p.returncode == 2)
        check(name + '-no-lock-leak', not lockpath.exists())
        if lockpath.exists():
            # Only this regression's acquired token, never a shared lock.
            holder = json.loads(lockpath.read_text())
            cleanup = lockmod.Lock(str(lockpath))
            cleanup.acquired, cleanup.token = True, holder['token']
            cleanup.release()
    check('existing-report-unchanged', existing.read_bytes() == b'USER')

    # Inject a late append IO failure after a real child completes: finally still releases.
    a = rec.build_parser().parse_args(['run', '--id', 'append-error', '--jsonl',
        str(root / 'late.jsonl'), '--log-dir', str(root / 'late'), *owned, '--',
        sys.executable, '-c', 'print("late")'])
    handlers = {s: signal.getsignal(s) for s in (signal.SIGINT, signal.SIGTERM, signal.SIGBREAK)}
    caught = False
    try:
        with patch.object(rec, 'append_jsonl', side_effect=OSError('review injected append failure')):
            rec.cmd_run(a)
    except OSError:
        caught = True
    finally:
        for s, handler in handlers.items():
            signal.signal(s, handler)
    check('late-io-error-observed', caught)
    check('late-io-finally-released', not lockpath.exists())
    check('late-io-real-child-finished', json.loads((root / 'late/append-error/c1/result.json').read_text())['rc'] == 0)

    good = run('good')
    check('healthy-run', good.returncode == 0)
    case = root / 'runs/good/c1'
    resultpath = case / 'result.json'
    historical = resultpath.read_bytes()
    raw = case / 'stdout.raw'
    original = raw.read_bytes()
    raw.write_bytes(b'TAMPERED\n')
    p = call(['recover', '--run-dir', str(case.parent), '--jsonl', str(root / 'tampered.jsonl')])
    row = json.loads((root / 'tampered.jsonl').read_text())
    check('tampered-recovery-rejected', p.returncode == 7 and row['recordOk'] is False)
    check('recovery-keeps-historical-hash', row['raw']['stdout']['sha256'] == json.loads(historical)['stdout']['sha256'])
    check('recovery-does-not-rewrite-result', resultpath.read_bytes() == historical)
    raw.write_bytes(original)
    p = call(['recover', '--run-dir', str(case.parent), '--jsonl', str(root / 'healthy.jsonl')])
    check('healthy-recovery', p.returncode == 0)

    for name, mutate in [('prepared', lambda obj: obj.update(phase='prepared')),
                         ('wrong-case', lambda obj: obj.update(caseId='other')),
                         ('wrong-argv', lambda obj: obj.update(argv=['fake']))]:
        obj = json.loads(historical)
        mutate(obj)
        resultpath.write_text(json.dumps(obj), encoding='utf-8')
        p = call(['recover', '--run-dir', str(case.parent), '--jsonl', str(root / (name + '.jsonl'))])
        row = json.loads((root / (name + '.jsonl')).read_text())
        check(name + '-unknown', p.returncode == 3 and row['phase'] == 'unknown' and row['rc'] is None)
    resultpath.write_bytes(historical)

    absent = root / 'must-not-append.jsonl'
    p = call(['recover', '--run-dir', str(case.parent), '--jsonl', str(absent), '--report', str(existing)])
    check('recover-existing-report-no-write', p.returncode == 2 and not absent.exists())
    p = run('report-ledger-alias', ['--report', str(root / 'ledger.jsonl')])
    check('report-ledger-alias-refused', p.returncode == 2)
    for name in ('../outside', '..', 'C:\\outside', '/outside'):
        p = run(name)
        check('unsafe-id-' + name, p.returncode == 2)
    check('unsafe-id-no-escape', not (root / 'outside').exists())

    outer = lockmod.Lock(str(root / 'outer.lock'))
    outer.acquire('review', 'outer', 'outer-proof')
    before = Path(outer.path).read_bytes()
    try:
        ext = ['--lock-path', outer.path, '--lock-mode', 'external']
        p = run('external-no-proof', ext)
        check('external-no-proof-refused', p.returncode == 2)
        p = run('external-wrong-proof', ext + ['--lock-token', 'wrong'])
        check('external-wrong-proof-refused', p.returncode == 2)
        p = run('external-valid-proof', ext + ['--lock-token', outer.token])
        check('external-valid-proof-runs', p.returncode == 0)
        check('external-owner-bytes-intact', Path(outer.path).read_bytes() == before)
    finally:
        check('outer-owner-released', outer.release())

    summary = {'checks': checks, 'passed': sum(c['pass'] for c in checks),
               'failed': sum(not c['pass'] for c in checks), 'calls': calls, 'root': str(root)}
    (root / 'summary.json').write_text(json.dumps(summary, indent=2), encoding='utf-8')
    print(json.dumps(summary, ensure_ascii=True))
    return 1 if summary['failed'] else 0


if __name__ == '__main__':
    raise SystemExit(main())

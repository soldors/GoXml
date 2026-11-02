# -*- coding: utf-8 -*-
r"""R8-C 证据工具的**红判据**：证明每条断言真的会翻红，不是恒真装饰。

做法沿用 R6 的 `mutate-selfcheck-proc.py`：把相关脚本复制到 `<work>/<mutation>/scripts/`，
**只在副本上**做一处文本替换（注入一种任务书明令禁止的退化），再跑副本里的测试，
检查它是否在**预期断言**上失败。仓库源文件不动：运行前后对源文件做 SHA-256 比对。

每个变异对应一条禁止的退化：
  M1 ok-always-pass        schema 判定恒返回 pass                 → 毒化全部不红
  M2 bool-is-int           去掉 bool 不是 int 的守卫               → 类型毒化不红
  M3 raw-trust-self        不按磁盘重算 raw 的 bytes/SHA           → 篡改证据不红
  M4 jsonl-truncate        台账改成截断写（覆盖历史）              → 覆盖历史不红
  M5 unknown-claims-false  unknown 形态把 started 写成 False       → 未知被当成"没跑过"
  M6 lock-bypass           external 模式不核对锁文件是否存在       → 暗中绕锁不红
  M7 forced-rc-observed    不校验"强制退出码必须来自超时"          → 冒充自然 rc 不红
  M8 duplicate-allowed     不查重复 id                             → 重复记录不红

用法：
  python mutate-evidence-tools.py --work <目录> --out <report.json> [--only M1,M3]
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
FILES = [
    'evidence_schema.py', 'record-command.py', 'verify-evidence.py',
    'test-verify-evidence.py', 'test-record-command.py',
    'verify_examples_proc.py', 'verify_examples_proctree.py',
    'verify-examples-lock.py',
    os.path.join('testdata', 'verify-proc-sentinel.py'),
]

FAST = 'test-verify-evidence.py'
SLOW = 'test-record-command.py'

# (id, 说明, 目标文件, 原文, 替换, 跑哪个测试, 必须翻红的断言集合)
MUTATIONS = [
    ('M1', 'ok-always-pass: schema 判定恒返回 pass',
     'evidence_schema.py',
     "def _status_of(issues):\n    if any(i['severity'] == SEVERITY_FAIL for i in issues):",
     "def _status_of(issues):\n    return STATUS_PASS\n    if any(i['severity'] == SEVERITY_FAIL for i in issues):",
     FAST,
     {'poison.missing-field.status', 'poison.rc-string.status', 'poison.time-order.status',
      'poison.record-not-ok.status', 'legacy.unidentifiable-fails'}),

    ('M2', 'bool-is-int: 去掉 bool 不是 int 的守卫（True 会被当成 0/整数）',
     'evidence_schema.py',
     "    if type(value) is bool and bool not in types:\n        return False\n",
     "",
     FAST,
     {'poison.active-after-bool.code', 'poison.active-after-bool.status'}),

    ('M3', 'raw-trust-self: 不按磁盘重算 raw 的 bytes/SHA，只信台账自述',
     'evidence_schema.py',
     "def validate_full(obj, check_disk=True):",
     "def validate_full(obj, check_disk=True):\n    if True:\n"
     "        return validate_record(obj)\n",
     FAST,
     {'poison.raw-sha-mismatch', 'poison.raw-bytes-mismatch', 'poison.raw-file-missing',
      'poison.tampered-content', 'poison.missing-result-file'}),

    ('M7', 'forced-rc-observed: 不校验"强制退出码必须来自超时"',
     'evidence_schema.py',
     "    if src == RC_SOURCE_FORCED_TIMEOUT and timed_out is not True:",
     "    if False:",
     FAST,
     {'poison.forced-rc-without-timeout.code', 'poison.forced-rc-without-timeout.status'}),

    ('M8', 'duplicate-allowed: 不查重复 id',
     'verify-evidence.py',
     "        if len(where) > 1:",
     "        if False:",
     FAST,
     {'dup.exit', 'dup.code'}),

    ('M4', 'jsonl-truncate: 台账改成截断写（覆盖历史记录）',
     'record-command.py',
     "    flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND | getattr(os, 'O_BINARY', 0)",
     "    flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC | getattr(os, 'O_BINARY', 0)",
     SLOW,
     {'repeat.old-line-intact', 'final.has-lines'}),

    ('M5', 'unknown-claims-false: unknown 形态把 started 写成 False（未知被当成事实）',
     'record-command.py',
     "        rec['started'] = None\n        rec['rc'] = None",
     "        rec['started'] = False\n        rec['rc'] = None",
     SLOW,
     {'kill.no-fake-started'}),

    ('M6', 'lock-bypass: external 模式不核对锁文件是否存在（暗中绕锁）',
     'record-command.py',
     "    if mode == 'external':\n",
     "    if mode == 'external':\n        return None, info, None\n",
     SLOW,
     {'lock.external-missing-exit'}),
]


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def copy_tree(dst):
    scripts = os.path.join(dst, 'scripts')
    os.makedirs(os.path.join(scripts, 'testdata'), exist_ok=True)
    for name in FILES:
        src = os.path.join(HERE, name)
        target = os.path.join(scripts, name)
        os.makedirs(os.path.dirname(target), exist_ok=True)
        shutil.copyfile(src, target)
    return scripts


def apply_mutation(scripts, mut):
    _mid, _desc, target, anchor, repl, _test, _expect = mut
    path = os.path.join(scripts, target)
    with open(path, 'r', encoding='utf-8') as fh:
        text = fh.read()
    if anchor not in text:
        return False, 'anchor not found in %s' % target
    with open(path, 'w', encoding='utf-8', newline='\n') as fh:
        fh.write(text.replace(anchor, repl, 1))
    return True, None


def run_test(scripts, test_file, work_dir, timeout=900):
    argv = [sys.executable, os.path.join(scripts, test_file), '--work', work_dir]
    proc = subprocess.run(argv, cwd=scripts, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          timeout=timeout)
    out = proc.stdout.decode('utf-8', 'replace')
    failed = set()
    for line in out.splitlines():
        if line.startswith('FAIL '):
            failed.add(line.split()[1])
    return proc.returncode, failed, out, proc.stderr.decode('utf-8', 'replace')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--only', default=None, help='只跑指定的变异，逗号分隔，如 M1,M3')
    args = ap.parse_args()

    work = os.path.abspath(args.work)
    os.makedirs(work, exist_ok=True)

    before = {name: sha256_file(os.path.join(HERE, name)) for name in FILES}
    wanted = set((args.only or '').split(',')) if args.only else None

    rows = []
    for mut in MUTATIONS:
        mid, desc, _target, _anchor, _repl, test_file, expect = mut
        if wanted is not None and mid not in wanted:
            continue
        root = os.path.join(work, mid)
        if os.path.isdir(root):
            shutil.rmtree(root, ignore_errors=True)
        scripts = copy_tree(root)
        ok, err = apply_mutation(scripts, mut)
        if not ok:
            rows.append({'id': mid, 'desc': desc, 'ok': False, 'error': err,
                         'failed': [], 'expected': sorted(expect)})
            print('ANCHOR-MISS %-4s %s' % (mid, err))
            continue
        rc, failed, out, err_out = run_test(scripts, test_file, os.path.join(root, 'work'))
        hit = expect & failed
        good = bool(hit) and rc != 0
        rows.append({'id': mid, 'desc': desc, 'ok': good, 'exitCode': rc,
                     'failed': sorted(failed), 'expected': sorted(expect),
                     'hit': sorted(hit), 'missing': sorted(expect - failed),
                     'tail': out[-400:], 'stderr': err_out[-400:]})
        print('%-4s %-58s rc=%d hit=%d/%d %s' % (mid, desc[:58], rc, len(hit), len(expect),
                                                 'OK' if good else 'NOT-RED'))

    after = {name: sha256_file(os.path.join(HERE, name)) for name in FILES}
    unchanged = (before == after)
    bad = [r for r in rows if not r.get('ok')]
    report = {'status': 'PASS' if (not bad and unchanged) else 'FAIL',
              'sourcesUnchanged': unchanged,
              'mutations': rows, 'count': len(rows), 'notRed': len(bad)}
    os.makedirs(os.path.dirname(os.path.abspath(args.out)) or '.', exist_ok=True)
    with open(args.out, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2, sort_keys=True)
    print(json.dumps({'status': report['status'], 'count': len(rows), 'notRed': len(bad),
                      'sourcesUnchanged': unchanged}, ensure_ascii=False))
    if not unchanged:
        print('  ! 源文件字节在运行后发生了变化')
    return 0 if (not bad and unchanged) else 1


if __name__ == '__main__':
    sys.exit(main())

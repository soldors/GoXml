# -*- coding: utf-8 -*-
r"""verify-examples.ps1 的 **forms 示例注册**专用选择测试。

只测"示例选择"这一段（`-Example` 的白名单、all 列表、源材料校验与既有用例规则），
不重写锁助手、也不重写通用启动机制：本脚本通过 -SkipLock 由**调用方**持公共锁，
脚本内部的锁/marker/退出码逻辑沿用既有实现。

判据：
  F1 -Example forms         → rc 0，只出现 forms 的子目录（starter/tasks 未构建）
  F2 -Example all           → rc 0，starter/tasks/forms/workbench 四个子目录都在
                              （workbench 于 R5-C 接入 $known；此判据随注册点同步更新）
  F3 -Example bogus         → rc 2，提示"未知示例"且列出 forms
  F4 -Example forms -SourceDir <缺 vm.go> → rc 7（源材料校验）
  F5 -Example all -SourceDir x            → rc 2（既有规则未被破坏）

用法：
  python test-verify-examples-forms.py --repo <repo> --out out/forms-selector.json \
      --lock-path D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock \
      --python-exe C:/.../python.exe
"""
import argparse
import datetime
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time


def load_lock_helper(repo):
    path = os.path.join(repo, 'scripts', 'verify-examples-lock.py')
    spec = importlib.util.spec_from_file_location('verify_examples_lock', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod, path


def decode_console(raw):
    """PS 5.1 重定向出来的控制台是 GBK，不是 UTF-8（踩坑-PORTABLE-WORKBUDDY-01）。"""
    for enc in ('utf-8', 'mbcs', 'latin-1'):
        try:
            return raw.decode(enc), enc
        except UnicodeDecodeError:
            continue
    return raw.decode('utf-8', 'replace'), 'replace'


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--repo', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--lock-path', required=True)
    ap.add_argument('--python-exe', default=sys.executable)
    ap.add_argument('--go-exe', default='C:\\Program Files\\Go\\bin\\go.exe')
    ap.add_argument('--agent', default='workbuddy-r4c')
    ap.add_argument('--task', default='R4-C forms selector')
    args = ap.parse_args()

    repo = os.path.abspath(args.repo)
    script = os.path.join(repo, 'scripts', 'verify-examples.ps1')
    ps = shutil.which('powershell.exe')
    if not ps:
        print('找不到 powershell.exe')
        return 2
    if not os.path.isfile(script):
        print('脚本缺失: %s' % script)
        return 2

    lockmod, locksrc = load_lock_helper(repo)
    run_id = 'forms-selector-' + time.strftime('%Y%m%d-%H%M%S') + '-p' + str(os.getpid())
    report = {'runId': run_id, 'repo': repo, 'script': script, 'host': ps,
              'lockImpl': locksrc, 'started': utc(), 'criteria': []}

    lock = lockmod.Lock(args.lock_path)
    try:
        holder = lock.acquire(args.agent, args.task, run_id)
        report['lock'] = {'acquire': 'ok', 'token': holder.get('token'),
                          'identityStrength': (holder.get('hostIdentity') or {}).get('identityStrength')}
    except Exception as exc:
        report['lock'] = {'acquire': 'busy', 'error': '%s: %s' % (type(exc).__name__, exc)}
        print(json.dumps(report, ensure_ascii=False))
        return 70

    ok = False
    try:
        def run_case(cid, expect_rc, extra):
            # 输出根必须**不存在**：脚本对已有目录（含空目录）一律 rc 5。
            base = tempfile.mkdtemp(prefix='goxml-forms-sel-' + cid + '-')
            out_root = os.path.join(base, cid)
            argv = [ps, '-NoProfile', '-File', script,
                    '-RepoRoot', repo, '-OutputRoot', out_root, '-SkipLock',
                    '-PythonExe', args.python_exe, '-GoExe', args.go_exe] + extra
            t0 = time.time()
            try:
                p = subprocess.run(argv, cwd=repo, timeout=1800,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                rc, so, se = p.returncode, p.stdout, p.stderr
            except subprocess.TimeoutExpired as exc:
                rc, so, se = 8, (exc.stdout or b''), (exc.stderr or b'')
            elapsed = round(time.time() - t0, 2)
            text, enc = decode_console((so or b'') + (se or b''))
            kids = sorted(os.listdir(out_root)) if os.path.isdir(out_root) else []
            # 示例子目录：排除脚本自己的 bin/ 与根层的 marker/日志/入口产物。
            exdirs = sorted(d for d in kids
                            if os.path.isdir(os.path.join(out_root, d)) and d != 'bin')
            # 产物名不按目录名推导：starter 的页面是 home.gxml（生成 home_gen.go），
            # 窗口入口则是根层的 <示例>-entry.exe。
            entry = {}
            for name in exdirs:
                pth = os.path.join(out_root, name)
                gens = [f for f in os.listdir(pth) if f.endswith('_gen.go')] if os.path.isdir(pth) else []
                gen = os.path.join(pth, gens[0]) if gens else ''
                exe = os.path.join(out_root, name + '-entry.exe')
                entry[name] = {
                    'genFiles': gens,
                    'genBytes': os.path.getsize(gen) if gen and os.path.isfile(gen) else 0,
                    'entryExists': os.path.isfile(exe),
                    'entryBytes': os.path.getsize(exe) if os.path.isfile(exe) else 0,
                }
            row = {'id': cid, 'argv': argv, 'rc': rc, 'expectRc': expect_rc,
                   'elapsedSec': elapsed, 'stdoutEncoding': enc,
                   'outputRootEntries': kids, 'exampleDirs': exdirs, 'perExample': entry,
                   'outTail': text[-1200:]}
            report['criteria'].append(row)
            try:
                shutil.rmtree(base, ignore_errors=True)
            except OSError:
                pass
            return row

        ok = True

        # F1 单个示例：只构建 forms（starter/tasks 不出现）
        r = run_case('F1', 0, ['-Example', 'forms'])
        r['pass'] = (r['rc'] == 0 and r['exampleDirs'] == ['forms']
                     and r['perExample'].get('forms', {}).get('genBytes', 0) > 0
                     and r['perExample'].get('forms', {}).get('entryExists', False)
                     and r['perExample'].get('forms', {}).get('entryBytes', 0) > 0)
        ok = ok and r['pass']

        # F2 all：三个都在，且各自都有生成物与入口产物
        r = run_case('F2', 0, ['-Example', 'all'])
        r['pass'] = (r['rc'] == 0 and r['exampleDirs'] == ['forms', 'starter', 'tasks', 'workbench']
                     and all(r['perExample'].get(n, {}).get('genBytes', 0) > 0
                             and r['perExample'].get(n, {}).get('entryBytes', 0) > 0
                             for n in ('forms', 'starter', 'tasks', 'workbench')))
        ok = ok and r['pass']

        # F3 未知示例：rc 2 且列出 forms
        r = run_case('F3', 2, ['-Example', 'bogus'])
        r['pass'] = (r['rc'] == 2 and ('未知示例' in r['outTail']) and ('forms' in r['outTail']))
        ok = ok and r['pass']

        # F4 源材料缺失：rc 7
        bad = tempfile.mkdtemp(prefix='goxml-forms-src-')
        open(os.path.join(bad, 'forms.gxml'), 'w', encoding='utf-8').write(
            '<Goxml Version="1" ViewModel="ViewModel"><Page Route="/x"><Column><Text Value="a"/></Column></Page></Goxml>\n')
        r = run_case('F4', 7, ['-Example', 'forms', '-SourceDir', bad])
        r['pass'] = (r['rc'] == 7)
        ok = ok and r['pass']
        shutil.rmtree(bad, ignore_errors=True)

        # F5 既有规则：all + SourceDir → rc 2
        good = tempfile.mkdtemp(prefix='goxml-forms-src2-')
        for n in ('vm.go', 'main.go.txt'):
            shutil.copy2(os.path.join(repo, 'examples', 'gxml', 'testdata', 'forms', n),
                         os.path.join(good, n))
        shutil.copy2(os.path.join(repo, 'examples', 'gxml', 'testdata', 'forms', 'forms.gxml'),
                     os.path.join(good, 'forms.gxml'))
        r = run_case('F5', 2, ['-Example', 'all', '-SourceDir', good])
        r['pass'] = (r['rc'] == 2)
        ok = ok and r['pass']
        shutil.rmtree(good, ignore_errors=True)

        report['status'] = 'PASS' if ok else 'FAIL'
        report['ended'] = utc()

    except Exception as exc:
        report['status'] = 'FAIL'
        report['error'] = '%s: %s' % (type(exc).__name__, exc)
        ok = False
    finally:
        try:
            released = lock.release()
            report['lock']['release'] = 'ok' if released else ('refused: %s' % lock.release_error)
        except Exception as exc:
            report['lock']['release'] = 'failed: %s' % exc
        try:
            report['lock']['afterRelease'] = 'absent' if lockmod.Lock(args.lock_path).read() is None else 'still-present'
        except Exception as exc:
            report['lock']['afterRelease'] = 'read-failed:%s' % exc

    if report['lock'].get('release') != 'ok' or report['lock'].get('afterRelease') != 'absent':
        ok = False
        report['status'] = 'FAIL'
    report['ended'] = utc()
    out_path = os.path.abspath(args.out)
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, 'w', encoding='utf-8', newline='\n') as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print(json.dumps({c['id']: ('PASS' if c['pass'] else 'FAIL') + ' rc=%s' % c['rc']
                      for c in report['criteria']}, ensure_ascii=False))
    print(json.dumps({'status': report['status'], 'lock': report['lock']}, ensure_ascii=False))
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())

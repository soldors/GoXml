# -*- coding: utf-8 -*-
r"""verify-examples.ps1 的 **preferences 示例注册**专用选择测试。

只测"示例选择"这一段（`-Example` 的白名单、all 列表、源材料校验与既有用例规则），
不重写锁助手、也不重写通用启动机制：本脚本通过 -SkipLock 由**调用方**持公共锁，
脚本内部的锁/marker/退出码逻辑沿用既有实现（scripts/verify_examples_selcase.py）。

判据（与 forms 的 F1–F5 / workbench 的 W1–W5 同构，预期集合**独立**书写，
不是从被测结果反推）：
  P1 -Example preferences    → rc 0，只出现 preferences 的子目录（starter/tasks/forms/workbench 未构建）
  P2 -Example all            → rc 0，五个子目录都在，且各有生成物与入口产物
                               （preferences 于 R7-C 接入 $known，all 从 4 个变 5 个是本任务预期）
  P3 -Example bogus          → rc 2，提示"未知示例"且列出 preferences
  P4 -Example preferences -SourceDir <缺 vm.go> → rc 7（源材料校验）
  P5 -Example all -SourceDir x                  → rc 2（既有规则未被破坏）

业务判据先各自算出来，再与基础设施门（infraOk）合成一次；基础设施不可信时
case 判红，不用"跑通了"冒充"证据可信"。

用法：
  python test-verify-examples-preferences.py --repo <repo> --out out/preferences-selector.json \
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


def load_selcase(repo):
    """从**被测树**加载单 case 执行器：测试的就是这棵树里的实现。"""
    path = os.path.join(repo, 'scripts', 'verify_examples_selcase.py')
    spec = importlib.util.spec_from_file_location('verify_examples_selcase', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--repo', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--lock-path', required=True)
    ap.add_argument('--python-exe', default=sys.executable)
    ap.add_argument('--go-exe', default='C:\\Program Files\\Go\\bin\\go.exe')
    ap.add_argument('--agent', default='workbuddy-r7c')
    ap.add_argument('--task', default='R7-C preferences selector')
    # raw 落到独立日志目录；默认放在 --out 同级的 raw/<runId>，绝不覆盖历史。
    ap.add_argument('--log-dir', default='')
    ap.add_argument('--containment', default='win-job', choices=['win-job', 'auto', 'none'])
    ap.add_argument('--keep-output', action='store_true', help='保留本次构建输出根（默认只留日志）')
    ap.add_argument('--case-timeout', type=float, default=1800.0,
                    help='单个 case 的有界超时（秒）；<=0 会在启动前被拒绝')
    # 仅用于验证"上层是否真的消费故障"；正常门禁保持 none。
    ap.add_argument('--inject-fault', default='none',
                    choices=['none', 'assign-fail', 'terminate-fail'])
    ap.add_argument('--ps-exe', default='',
                    help='PowerShell 宿主；默认 5.1（系统 powershell），可传 7.x 的 pwsh 验证')
    args = ap.parse_args()

    if os.path.lexists(os.path.abspath(args.out)):
        print('报告路径已存在，拒绝覆盖: %s' % os.path.abspath(args.out))
        return 2

    repo = os.path.abspath(args.repo)
    script = os.path.join(repo, 'scripts', 'verify-examples.ps1')
    ps = args.ps_exe or shutil.which('powershell.exe')
    if ps and not (os.path.isfile(ps) or shutil.which(ps)):
        print('指定的 PowerShell 宿主不存在: %s' % ps)
        return 2
    if not ps:
        print('找不到 powershell.exe')
        return 2
    if not os.path.isfile(script):
        print('脚本缺失: %s' % script)
        return 2

    lockmod, locksrc = load_lock_helper(repo)
    selcase = load_selcase(repo)
    run_id = 'preferences-selector-' + time.strftime('%Y%m%d-%H%M%S') + '-p' + str(os.getpid())
    out_abs = os.path.abspath(args.out)
    log_dir = (os.path.abspath(args.log_dir) if args.log_dir
               else os.path.join(os.path.dirname(out_abs), 'raw', run_id))
    report = {'runId': run_id, 'repo': repo, 'script': script, 'host': ps,
              'lockImpl': locksrc, 'selcaseImpl': os.path.join(repo, 'scripts',
                                                              'verify_examples_selcase.py'),
              'started': utc(), 'criteria': []}

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
        runner = selcase.Runner(repo=repo, ps=ps, script=script, log_dir=log_dir,
                                python_exe=args.python_exe, go_exe=args.go_exe,
                                containment=args.containment, keep_output=args.keep_output,
                                case_timeout=args.case_timeout,
                                fault=(None if args.inject_fault == 'none'
                                       else {args.inject_fault.replace('-', '_'): True}))
        report['logDir'] = log_dir
        report['containment'] = args.containment
        report['host'] = ps
        report['caseTimeoutSec'] = args.case_timeout
        report['injectedFault'] = args.inject_fault
        report['containmentAvailable'] = runner.proc.tree_available()

        def run_case(cid, expect_rc, extra):
            row = runner.run_case(cid, extra)
            row['expectRc'] = expect_rc
            report['criteria'].append(row)
            return row

        ok = True

        # P1 单个示例：只构建 preferences（starter/tasks/forms/workbench 不出现）
        r = run_case('P1', 0, ['-Example', 'preferences'])
        r['pass'] = (r['rc'] == 0 and r['exampleDirs'] == ['preferences']
                     and r['perExample'].get('preferences', {}).get('genBytes', 0) > 0
                     and r['perExample'].get('preferences', {}).get('entryExists', False)
                     and r['perExample'].get('preferences', {}).get('entryBytes', 0) > 0)
        ok = ok and r['pass']

        # P2 all：五个都在，且各自都有生成物与入口产物
        r = run_case('P2', 0, ['-Example', 'all'])
        r['pass'] = (r['rc'] == 0
                     and r['exampleDirs'] == ['forms', 'preferences', 'starter', 'tasks', 'workbench']
                     and all(r['perExample'].get(n, {}).get('genBytes', 0) > 0
                             and r['perExample'].get(n, {}).get('entryBytes', 0) > 0
                             for n in ('forms', 'preferences', 'starter', 'tasks', 'workbench')))
        ok = ok and r['pass']

        # P3 未知示例：rc 2 且列出 preferences
        r = run_case('P3', 2, ['-Example', 'bogus'])
        r['pass'] = (r['rc'] == 2
                     and ('未知示例' in r['outTail']) and ('preferences' in r['outTail']))
        ok = ok and r['pass']

        # P4 源材料缺失：rc 7
        bad = tempfile.mkdtemp(prefix='goxml-preferences-src-')
        open(os.path.join(bad, 'preferences.gxml'), 'w', encoding='utf-8').write(
            '<Goxml Version="1" ViewModel="ViewModel"><Page Route="/x"><Column><Text Value="a"/></Column></Page></Goxml>\n')
        r = run_case('P4', 7, ['-Example', 'preferences', '-SourceDir', bad])
        r['pass'] = (r['rc'] == 7)
        ok = ok and r['pass']
        shutil.rmtree(bad, ignore_errors=True)

        # P5 既有规则：all + SourceDir → rc 2
        good = tempfile.mkdtemp(prefix='goxml-preferences-src2-')
        for n in ('vm.go', 'main.go.txt', 'preferences.gxml'):
            shutil.copy2(os.path.join(repo, 'examples', 'gxml', 'testdata', 'preferences', n),
                         os.path.join(good, n))
        r = run_case('P5', 2, ['-Example', 'all', '-SourceDir', good])
        r['pass'] = (r['rc'] == 2)
        ok = ok and r['pass']
        shutil.rmtree(good, ignore_errors=True)

        # 基础设施门**只在这里**施加一次：业务判据先算出来，再与 infraOk 合成。
        report['infraFailures'] = [c['id'] for c in report['criteria'] if not c['infraOk']]
        for c in report['criteria']:
            c['businessPass'] = c['pass']
            c['pass'] = selcase.case_passed(c, c['pass'])
            if not c['pass']:
                ok = False
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
    out_path = out_abs
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    with open(out_path, 'x', encoding='utf-8', newline='\n') as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print(json.dumps({c['id']: ('PASS' if c['pass'] else 'FAIL') + ' rc=%s' % c['rc']
                      for c in report['criteria']}, ensure_ascii=False))
    print(json.dumps({'status': report['status'], 'lock': report['lock']}, ensure_ascii=False))
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())

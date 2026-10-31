# -*- coding: utf-8 -*-
r"""verify-examples.ps1 的 Windows PowerShell 5.1 / PowerShell 7 双宿主验收。

真实调用 scripts/verify-examples.ps1，不模拟其内部函数。每个宿主分别验证：
  * 成功：空参数、含空格/Unicode/单引号/尾反斜杠的输出路径、真实生成与入口构建；
    测试 Go 包装进程回读 argv/cwd/环境，确认 ProcessStartInfo 注入和参数一致；
  * 失败：非法绑定必须保留工具退出码 3 与 GXML-BIND-001，不能被日志/清理覆盖。

本脚本只写 --output-root / --raw-dir / --report / --commands 指定的位置。
Go 重负载前取得共享 heavy.lock，finally 实测释放自己的 token。
不安装宿主或依赖、不修改全局 TMP/TEMP/HOME、不执行窗口。

示例：
  python scripts/test-verify-examples-hosts.py \
    --goxml-exe "D:\path with space\goxml.exe" \
    --output-root "D:\delivery\work" --raw-dir "D:\delivery\raw" \
    --report "D:\delivery\out\host-matrix.json" \
    --commands "D:\delivery\commands.jsonl"
"""
import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path

GO = r'C:\Program Files\Go\bin\go.exe'
# 夜间公共锁：本测试跑的是真实 Go 构建，按纪律**显式**指定，不用仓库内默认值。
LOCK = r'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock'


def load_lock_helper(repo_root):
    """按路径加载**仓库自带**的锁助手（不再依赖任何仓库外目录）。"""
    path = Path(repo_root) / 'scripts' / 'verify-examples-lock.py'
    spec = importlib.util.spec_from_file_location('verify_examples_lock', str(path))
    module = importlib.util.module_from_spec(spec)
    sys.modules['verify_examples_lock'] = module
    spec.loader.exec_module(module)
    return module, path


def now():
    return datetime.now().astimezone().isoformat(timespec='milliseconds')


def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def write_json(path, obj):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_bytes((json.dumps(obj, ensure_ascii=False, indent=2) + '\n').encode('utf-8'))


def read_utf8_log(path):
    data = Path(path).read_bytes()
    return {
        'bytes': len(data),
        'sha256': hashlib.sha256(data).hexdigest(),
        'hasUtf8Bom': data.startswith(b'\xef\xbb\xbf'),
        'text': data.decode('utf-8'),
    }


class Runner:
    def __init__(self, raw_dir, run_id):
        self.raw_dir = Path(raw_dir)
        self.raw_dir.mkdir(parents=True, exist_ok=True)
        self.run_id = run_id
        self.commands = []
        self.counter = 0

    def run(self, argv, cwd, label, env=None):
        # raw 文件名带 runId：不同轮次的取证互不覆盖（首轮失败的证据也要保留）。
        self.counter += 1
        stem = '%s-%02d-%s' % (self.run_id, self.counter, label)
        stdout_path = self.raw_dir / (stem + '.stdout.log')
        stderr_path = self.raw_dir / (stem + '.stderr.log')
        started = now()
        t0 = time.monotonic()
        with open(stdout_path, 'wb') as out, open(stderr_path, 'wb') as err:
            proc = subprocess.run(argv, cwd=str(cwd), env=env, stdout=out, stderr=err,
                                  shell=False)
        row = {
            'runId': self.run_id,
            'label': label,
            'argv': [str(x) for x in argv],
            'cwd': str(cwd),
            'startedAt': started,
            'endedAt': now(),
            'elapsedSeconds': round(time.monotonic() - t0, 3),
            'rc': proc.returncode,
            'stdout': str(stdout_path),
            'stderr': str(stderr_path),
        }
        self.commands.append(row)
        return row


def host_version(runner, name, path):
    code = (
        "$clr=$PSVersionTable.CLRVersion; "
        "if($null -eq $clr){$clrText=''}else{$clrText=$clr.ToString()}; "
        "$osText=''; if($PSVersionTable.OS){$osText=$PSVersionTable.OS}; "
        "[ordered]@{PSVersion=$PSVersionTable.PSVersion.ToString();"
        "PSEdition=$PSVersionTable.PSEdition;CLRVersion=$clrText;OS=$osText;"
        "Framework=[System.Runtime.InteropServices.RuntimeInformation]::FrameworkDescription;"
        "Path=$env:PATH;Tmp=$env:TMP;Temp=$env:TEMP;Home=$env:HOME}"
        " | ConvertTo-Json -Compress"
    )
    row = runner.run([path, '-NoLogo', '-NoProfile', '-NonInteractive', '-Command', code],
                     cwd=Path.home(), label='version-' + name)
    result = {'name': name, 'path': path, 'rc': row['rc']}
    if row['rc'] == 0:
        try:
            result.update(json.loads(Path(row['stdout']).read_text(encoding='utf-8').strip()))
        except Exception as exc:
            result['parseError'] = str(exc)
    return result


def assert_probe(report, probe_log, expected_argv, expected_cwd, host_env, failures, prefix):
    """断言工具派生的子进程环境与**宿主自身**环境一致（而非 Python 父环境）。

    背景（2026-10-31 实测）：pwsh 经 WindowsApps 应用执行别名启动时，会在**宿主启动
    阶段**把别名目录前置到 PATH（delta=86 字符、后缀与父环境完全一致；只跑
    `-Command '$env:PATH'` 不加载任何仓库脚本即可复现）。那是宿主行为，
    不是 verify-examples.ps1 的污染。因此正确的判据是「子 == 宿主」，
    同时把宿主相对父环境的 PATH 差异作为证据记录在 report 里。
    """
    if not Path(probe_log).is_file():
        failures.append(prefix + ': 子进程探针日志不存在')
        return
    lines = [x for x in Path(probe_log).read_text(encoding='utf-8').splitlines() if x.strip()]
    if len(lines) != 1:
        failures.append(prefix + ': 探针记录数应恰为 1，实际 %d' % len(lines))
        return
    rec = json.loads(lines[0])
    report['childArgv'] = rec['argv']
    report['childCwd'] = rec['cwd']
    report['childEnv'] = rec['env']
    if rec['argv'] != expected_argv:
        failures.append(prefix + ': 子进程 argv 不一致')
    if os.path.normcase(os.path.normpath(rec['cwd'])) != os.path.normcase(os.path.normpath(expected_cwd)):
        failures.append(prefix + ': 子进程 cwd 不一致')
    expected_env = {
        'CGO_ENABLED': '0', 'GOMAXPROCS': '2', 'GOPROXY': 'off', 'GOSUMDB': 'off',
        'GOTOOLCHAIN': 'local', 'GOFLAGS': '-mod=readonly',
    }
    for key, want in expected_env.items():
        if rec['env'].get(key) != want:
            failures.append('%s: 子环境 %s=%r，期望 %r' % (prefix, key, rec['env'].get(key), want))
    env_key_map = {'TMP': 'Tmp', 'TEMP': 'Temp', 'HOME': 'Home', 'PATH': 'Path'}
    for key, prop in env_key_map.items():
        if rec['env'].get(key, '') != host_env.get(prop, ''):
            failures.append(prefix + ': 子环境 ' + key + ' 与宿主环境不一致')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--repo-root', default=str(Path(__file__).resolve().parent.parent))
    parser.add_argument('--goxml-exe', required=True)
    parser.add_argument('--output-root', required=True)
    parser.add_argument('--raw-dir', required=True)
    parser.add_argument('--report', required=True)
    parser.add_argument('--commands', required=True)
    parser.add_argument('--lock-path', default=LOCK)
    parser.add_argument('--timeout-sec', type=int, default=240)
    args = parser.parse_args()

    repo = Path(args.repo_root).resolve()
    script = repo / 'scripts' / 'verify-examples.ps1'
    goxml = Path(args.goxml_exe).resolve()
    run_id = 'r3-hosts-' + datetime.now().strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8]
    runner = Runner(args.raw_dir, run_id)
    work = Path(args.output_root) / run_id
    work.mkdir(parents=True, exist_ok=False)
    failures = []
    report = {
        'kind': 'verify-examples-host-matrix',
        'runId': run_id,
        'startedAt': now(),
        'repoRoot': str(repo),
        'tool': str(script),
        'toolSha256': sha256(script),
        'toolHasUtf8Bom': script.read_bytes().startswith(b'\xef\xbb\xbf'),
        'reusedGoxmlExe': str(goxml),
        'reusedGoxmlSha256': sha256(goxml),
        'reusedCliBuildScope': '复用 R3 已冻结 CLI；本轮不重建 CLI，仅构建测试探针与 tasks 窗口入口',
        'hosts': [],
        'failures': failures,
    }
    if not report['toolHasUtf8Bom']:
        failures.append('verify-examples.ps1 缺少 UTF-8 BOM')

    host_paths = [('powershell5.1', shutil.which('powershell.exe')),
                  ('powershell7', shutil.which('pwsh.exe'))]
    versions = []
    for name, path in host_paths:
        if path:
            version = host_version(runner, name, path)
            version['present'] = True
            parent_path = os.environ.get('PATH', '')
            host_path = version.get('Path', '')
            version['startupPathEqualsParent'] = host_path == parent_path
            if host_path != parent_path and host_path.endswith(parent_path) and len(host_path) > len(parent_path):
                version['pathDeltaVsParent'] = host_path[:len(host_path) - len(parent_path)]
                version['pathDeltaKind'] = 'prefix-appended-by-host-startup'
            elif host_path != parent_path:
                version['pathDeltaKind'] = 'other'
            versions.append(version)
        else:
            versions.append({'name': name, 'present': False})
    report['hostVersions'] = versions

    lockmod, lockmod_path = load_lock_helper(repo)
    report['lockHelper'] = str(lockmod_path)
    lock = lockmod.Lock(args.lock_path)
    holder = None
    try:
        try:
            holder = lock.acquire(agent='workbuddy', task='r3-verify-examples-hosts', run_id=run_id)
        except lockmod.LockHeldError as exc:
            report['lockBusy'] = {'holder': exc.holder, 'staleCandidate': exc.stale_candidate}
            report['endedAt'] = now()
            write_json(args.report, report)
            print(json.dumps(report['lockBusy'], ensure_ascii=False), file=sys.stderr)
            return 77
        report['lockToken'] = holder['token']

        probe = work / 'bin' / 'process probe.exe'
        probe.parent.mkdir(parents=True, exist_ok=True)
        go_env = dict(os.environ)
        go_env.update({'CGO_ENABLED': '0', 'GOMAXPROCS': '2', 'GOPROXY': 'off',
                       'GOSUMDB': 'off', 'GOTOOLCHAIN': 'local', 'GOFLAGS': '-mod=readonly'})
        build = runner.run([GO, 'build', '-mod=readonly', '-o', str(probe), '.'],
                           cwd=repo / 'scripts' / 'testdata' / 'verify-process-probe',
                           label='build-process-probe', env=go_env)
        report['probeBuildRc'] = build['rc']
        if build['rc'] != 0:
            failures.append('测试探针构建失败')

        for name, host in host_paths:
            failures_before_host = len(failures)
            entry = {'host': name, 'present': bool(host)}
            report['hosts'].append(entry)
            if not host:
                entry['status'] = 'unverified-host-not-installed'
                continue
            host_work = work / name
            success_out = str(host_work / "成功 空格 中文'引号") + '\\'
            failure_out = str(host_work / "失败 空格 中文'引号") + '\\'
            source_dir = host_work / "源 空间 中文'引号" / 'tasks'
            source_dir.mkdir(parents=True, exist_ok=True)
            original_source = repo / 'examples' / 'gxml' / 'testdata' / 'tasks'
            for file_name in ('vm.go', 'tasks.gxml', 'main.go.txt'):
                shutil.copyfile(original_source / file_name, source_dir / file_name)
            gxml = source_dir / 'tasks.gxml'
            broken = gxml.read_bytes().replace(b'{Summary}', b'{Missing}')
            if broken == gxml.read_bytes():
                failures.append(name + ': 无法制备非法绑定用例')
            gxml.write_bytes(broken)
            source_arg = str(source_dir) + '\\'

            probe_log = runner.raw_dir / (run_id + '-' + name + '-success-probe.jsonl')
            child_env = dict(os.environ)
            child_env['GOXML_VERIFY_ARGV_LOG'] = str(probe_log)
            child_env['GOXML_VERIFY_REAL_GO'] = GO
            before_env = {k: os.environ.get(k, '') for k in ('TMP', 'TEMP', 'HOME', 'PATH')}
            host_env = next(v for v in versions if v['name'] == name)
            success_argv = [
                host, '-NoLogo', '-NoProfile', '-NonInteractive', '-File', str(script),
                '-Example', 'tasks', '-RepoRoot', '', '-OutputRoot', success_out,
                '-GoxmlExe', str(goxml), '-GoExe', str(probe), '-Mode', 'isolated',
                '-SourceDir', '', '-SkipLock', '-TimeoutSec', str(args.timeout_sec),
            ]
            success = runner.run(success_argv, cwd=work, label=name + '-success', env=child_env)
            after_env = {k: os.environ.get(k, '') for k in ('TMP', 'TEMP', 'HOME', 'PATH')}
            trimmed = success_out.rstrip('\\')
            run_log_path = Path(trimmed) / 'verify-examples-run.log'
            generated = Path(trimmed) / 'tasks' / 'tasks_gen.go'
            entry_exe = Path(trimmed) / 'tasks-entry.exe'
            success_result = {
                'rc': success['rc'],
                'outputRootArgument': success_out,
                'emptyRepoRootArgument': True,
                'emptySourceDirArgument': True,
                'parentEnvUnchanged': before_env == after_env,
                'generatedExists': generated.is_file(),
                'generatedBytes': generated.stat().st_size if generated.is_file() else 0,
                'entryExists': entry_exe.is_file(),
                'entryBytes': entry_exe.stat().st_size if entry_exe.is_file() else 0,
                'runLogExists': run_log_path.is_file(),
            }
            entry['success'] = success_result
            if success['rc'] != 0:
                failures.append(name + ': 成功路径 rc=%d，期望 0' % success['rc'])
            if not generated.is_file() or generated.stat().st_size == 0:
                failures.append(name + ': 生成产物缺失或为空')
            if not entry_exe.is_file() or entry_exe.stat().st_size == 0:
                failures.append(name + ': 窗口入口未构建')
            if before_env != after_env:
                failures.append(name + ': 父进程环境被修改')
            if run_log_path.is_file():
                log = read_utf8_log(run_log_path)
                success_result['runLogUtf8NoBom'] = not log['hasUtf8Bom']
                success_result['runLogSha256'] = log['sha256']
                if log['hasUtf8Bom']:
                    failures.append(name + ': 运行日志不应带 BOM')
                for text in ('全部通过', '输出目录: ' + trimmed, '退出码 0'):
                    if text not in log['text']:
                        failures.append(name + ': 成功日志缺少 ' + text)
            else:
                failures.append(name + ': 成功运行日志缺失')
            expected_argv = [str(probe), 'build', '-o', str(entry_exe), './cmd/tasks']
            assert_probe(success_result, probe_log, expected_argv,
                         str(Path(trimmed) / 'tasks'), host_env, failures, name + ' 成功路径')

            failure_probe_log = runner.raw_dir / (run_id + '-' + name + '-failure-probe.jsonl')
            failure_env = dict(os.environ)
            failure_env['GOXML_VERIFY_ARGV_LOG'] = str(failure_probe_log)
            failure_env['GOXML_VERIFY_REAL_GO'] = GO
            failure_argv = [
                host, '-NoLogo', '-NoProfile', '-NonInteractive', '-File', str(script),
                '-Example', 'tasks', '-RepoRoot', str(repo), '-OutputRoot', failure_out,
                '-GoxmlExe', str(goxml), '-GoExe', str(probe), '-Mode', 'isolated',
                '-SourceDir', source_arg, '-SkipLock', '-TimeoutSec', str(args.timeout_sec),
            ]
            failure = runner.run(failure_argv, cwd=work, label=name + '-illegal-binding', env=failure_env)
            failure_trimmed = failure_out.rstrip('\\')
            failure_log_path = Path(failure_trimmed) / 'verify-examples-run.log'
            failure_result = {
                'rc': failure['rc'],
                'sourceDirArgument': source_arg,
                'outputRootArgument': failure_out,
                'expectedRc': 3,
                'runLogExists': failure_log_path.is_file(),
            }
            entry['illegalBinding'] = failure_result
            if failure['rc'] != 3:
                failures.append(name + ': 非法绑定 rc=%d，期望 3' % failure['rc'])
            stderr_text = Path(failure['stderr']).read_bytes().decode('utf-8', 'replace')
            stdout_text = Path(failure['stdout']).read_bytes().decode('utf-8', 'replace')
            failure_result['hasBind001'] = 'GXML-BIND-001' in (stderr_text + stdout_text)
            if not failure_result['hasBind001']:
                failures.append(name + ': 非法绑定输出缺少 GXML-BIND-001')
            if failure_log_path.is_file():
                log = read_utf8_log(failure_log_path)
                failure_result['runLogUtf8NoBom'] = not log['hasUtf8Bom']
                failure_result['runLogSha256'] = log['sha256']
                for text in ('失败[rc=3]', source_arg, '退出码 3'):
                    if text not in log['text']:
                        failures.append(name + ': 失败日志缺少 ' + text)
            else:
                failures.append(name + ': 失败运行日志缺失')
            entry['status'] = 'verified' if len(failures) == failures_before_host else 'failed'
    finally:
        if holder is not None:
            released = lock.release()
            report['lockReleased'] = released
            report['lockReleaseError'] = lock.release_error
            if not released:
                failures.append('自己的共享锁未实测释放')
        report['endedAt'] = now()
        report['status'] = 'PASS' if not failures else 'FAIL'
        write_json(args.report, report)
        Path(args.commands).parent.mkdir(parents=True, exist_ok=True)
        Path(args.commands).write_bytes(''.join(
            json.dumps(row, ensure_ascii=False) + '\n' for row in runner.commands).encode('utf-8'))

    print(json.dumps({'runId': run_id, 'status': report['status'], 'failures': failures,
                      'report': args.report}, ensure_ascii=False, indent=2))
    return 0 if not failures else 1


if __name__ == '__main__':
    raise SystemExit(main())

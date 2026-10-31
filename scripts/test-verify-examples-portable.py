# -*- coding: utf-8 -*-
r"""verify-examples 可迁移性的**针对性**验证（不跑旧整套 10 项，不为补证扩大范围）。

覆盖两块：

  切片 1（可迁移）
    L1 锁助手默认锁路径落在**仓库内**（隔离布局里跑，不碰历史目录）
    L2 自己的 token 可取可放（acquire -> status -> release）
    L3 错误 token 拒绝释放（rc 78 + 锁文件字节不变）
    L4 子进程 import 路径不出现历史目录（PYTHONVERBOSE 取证 + 静态无 D:\\Codes 字面量）
    L5 隔离布局缺锁助手 -> rc 2 且信息明确（路径含空格/中文）
    L6 缺 Python / Python 不可用 -> rc 2 且信息明确（不弹商店、不绕锁）
    L10 隔离 repo 布局里真实成功一次（一个示例 + 一个宿主 + 复用已冻结 CLI）

  切片 2（runId / marker 所有权）
    L7 已有目录带**他人 marker** -> rc 5 且目录零新增、原 marker 字节不变
    L8 已有目录无 marker -> rc 5 且目录零新增
    L9 同一 PowerShell 宿主连续两次调用：runId 与 lockToken 各自唯一（PID 相同，证明不能只用 PID）

重负载规则：L10 是真实 Go 构建，必须显式 -LockPath 沿用夜间公共锁；测试自身不取公共锁，
由脚本按 -LockPath 取放。锁忙时不判失败也不判通过，记为 lock-busy 并在末尾重试一次。

只写 --output-root / --raw-dir / --report / --commands 指定的位置；不安装、不运行窗口、
不改系统环境变量、不删除任何他人的锁。
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import uuid
from datetime import datetime
from pathlib import Path

GO = r'C:\Program Files\Go\bin\go.exe'
SHARED_LOCK = r'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock'
MARKER = '.verify-examples-marker.json'
HISTORICAL_MARKERS = ('Goxml-agent-rounds', 'Goxml-night-reports', 'wcollector')


def now():
    return datetime.now().astimezone().isoformat(timespec='milliseconds')


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def write_json(path, obj):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_bytes((json.dumps(obj, ensure_ascii=False, indent=2) + '\n').encode('utf-8'))


class Runner(object):
    def __init__(self, raw_dir, run_id):
        self.raw_dir = Path(raw_dir)
        self.raw_dir.mkdir(parents=True, exist_ok=True)
        self.run_id = run_id
        self.commands = []
        self.counter = 0

    def run(self, argv, cwd, label, env=None):
        self.counter += 1
        stem = '%s-%02d-%s' % (self.run_id, self.counter, label)
        out_path = self.raw_dir / (stem + '.stdout.log')
        err_path = self.raw_dir / (stem + '.stderr.log')
        started = now()
        with open(out_path, 'wb') as fo, open(err_path, 'wb') as fe:
            proc = subprocess.run(argv, cwd=str(cwd), env=env, stdout=fo, stderr=fe,
                                  shell=False)
        row = {'runId': self.run_id, 'label': label, 'argv': [str(x) for x in argv],
               'cwd': str(cwd), 'startedAt': started, 'endedAt': now(),
               'rc': proc.returncode, 'stdout': str(out_path), 'stderr': str(err_path)}
        self.commands.append(row)
        out_text, out_enc = decode_console(Path(out_path).read_bytes())
        err_text, err_enc = decode_console(Path(err_path).read_bytes())
        row['stdoutText'] = out_text
        row['stderrText'] = err_text
        row['stdoutEncoding'] = out_enc
        row['stderrEncoding'] = err_enc
        return row


def decode_console(raw):
    """解码子进程输出。

    实测（2026-11-01）：Windows PowerShell 5.1 的 Write-Host 输出被重定向成字节流时，
    用的是**控制台代码页**（简中环境为 GBK/cp936），不是 UTF-8——直接按 UTF-8 解码会
    UnicodeDecodeError，按 replace 解码会把中文全换成替换符，于是"信息包含某中文片段"
    这类判据必然假红。这里逐级回退，并把实际用到的编码记进证据。
    """
    candidates = ['utf-8']
    if os.name == 'nt':
        candidates.append('mbcs')
    else:
        import locale
        candidates.append(locale.getpreferredencoding(False))
    candidates.append('latin-1')
    for enc in candidates:
        try:
            return raw.decode(enc), enc
        except (UnicodeDecodeError, LookupError):
            continue
    return raw.decode('utf-8', 'replace'), 'utf-8/replace'


def helper_json(row):
    text = row['stdoutText'].strip().splitlines()
    if not text:
        return None
    try:
        return json.loads(text[-1])
    except ValueError:
        return None


def snapshot(root):
    """目录快照：相对路径 -> (字节数, sha256)。用于"零新增/未改动"判据。"""
    root = Path(root)
    items = {}
    if root.is_dir():
        for p in sorted(root.rglob('*')):
            rel = str(p.relative_to(root))
            items[rel] = (p.stat().st_size if p.is_file() else -1,
                          sha256_file(p) if p.is_file() else '')
    return items


def copy_repo_layout(repo, dest, ignore_dirs=('.git', 'tmp')):
    """复制一份**隔离的 repo 布局**（不含 .git / tmp，纯工作树副本）。"""
    dest = Path(dest)
    if dest.exists():
        shutil.rmtree(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)

    def _ignore(path, names):
        return [n for n in names if n in ignore_dirs or n.endswith('.exe')]

    shutil.copytree(str(repo), str(dest), ignore=_ignore)
    return dest


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--repo-root', default=str(Path(__file__).resolve().parent.parent))
    ap.add_argument('--goxml-exe', required=True, help='已冻结的 goxml.exe（复用，不再构建 CLI）')
    ap.add_argument('--output-root', required=True)
    ap.add_argument('--raw-dir', required=True)
    ap.add_argument('--report', required=True)
    ap.add_argument('--commands', required=True)
    ap.add_argument('--shared-lock', default=SHARED_LOCK)
    ap.add_argument('--python-exe', default='python')
    ap.add_argument('--host-exe', default='powershell.exe')
    ap.add_argument('--timeout-sec', type=int, default=600)
    args = ap.parse_args()

    repo = Path(args.repo_root).resolve()
    goxml = Path(args.goxml_exe).resolve()
    run_id = 'portable-' + datetime.now().strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8]
    runner = Runner(args.raw_dir, run_id)
    work = Path(args.output_root) / run_id
    work.mkdir(parents=True, exist_ok=False)

    report = {'kind': 'verify-examples-portable', 'runId': run_id, 'startedAt': now(),
              'repoRoot': str(repo), 'goxmlExe': str(goxml),
              'goxmlSha256': sha256_file(goxml), 'hostExe': args.host_exe,
              'pythonExe': args.python_exe, 'sharedLock': args.shared_lock,
              'cases': {}, 'failures': []}
    failures = report['failures']
    ps = str(repo / 'scripts' / 'verify-examples.ps1')
    helper = str(repo / 'scripts' / 'verify-examples-lock.py')

    def case(name):
        box = {'failuresBefore': len(failures)}
        report['cases'][name] = box
        return box

    def close(box, name):
        if len(failures) == box['failuresBefore']:
            box['status'] = box.get('status') or 'verified'
        else:
            box['status'] = 'failed'
        return box

    def fail(name, msg):
        failures.append('%s: %s' % (name, msg))

    # ---------------------------------------------------------------- L1/L2/L3/L4
    # 隔离布局刻意放在**系统临时目录**下（与 D:\Codes\... 无关）：
    # 路径里带空格与中文，且不含任何历史/交付目录前缀，这样"默认锁路径落在仓库内"
    # 与"不引用历史目录"两条判据才是有判别力的。
    layout = copy_repo_layout(
        repo, Path(tempfile.mkdtemp(prefix='goxml 隔离 布局 ' + run_id + '-')) / 'Goxml')
    report['isolatedLayout'] = str(layout)
    layout_helper = str(layout / 'scripts' / 'verify-examples-lock.py')
    layout_lock = str(layout / 'tmp' / 'verify-examples' / 'heavy.lock')

    c1 = case('L1-default-lock-path-is-repo-local')
    row = runner.run([args.python_exe, layout_helper, 'status'], cwd=str(layout),
                     label='L1-helper-status-default')
    c1['rc'] = row['rc']
    c1['stdout'] = row['stdoutText'].strip()
    js = helper_json(row)
    c1['json'] = js
    if js is None or js.get('path') != layout_lock:
        fail('L1', '默认锁路径应为布局内 %s，实际 %r' % (layout_lock, js and js.get('path')))
    elif any(m in str(js.get('path', '')) for m in HISTORICAL_MARKERS):
        fail('L1', '默认锁路径落在历史目录: %s' % js.get('path'))
    close(c1, 'L1')

    c2 = case('L2-own-token-acquire-release')
    row = runner.run([args.python_exe, layout_helper, 'acquire', '--run-id', run_id + '-L2'],
                     cwd=str(layout), label='L2-acquire')
    acq = helper_json(row)
    c2['acquireRc'] = row['rc']
    c2['token'] = acq and acq.get('token')
    c2['lockPath'] = acq and acq.get('path')
    if row['rc'] != 0 or not acq or not acq.get('token'):
        fail('L2', '取锁失败 rc=%s' % row['rc'])
    else:
        lock_file = Path(acq['path'])
        if not lock_file.is_file():
            fail('L2', '锁文件不存在: %s' % lock_file)
        else:
            holder = json.loads(lock_file.read_text(encoding='utf-8'))
            c2['holder'] = holder
            if holder.get('runId') != run_id + '-L2':
                fail('L2', '锁里 runId 不是本次传入值: %r' % holder.get('runId'))
            if holder.get('hostIdentity', {}).get('identityStrength') != 'pid+createFiletime':
                fail('L2', '身份强度不是 pid+createFiletime: %r' % holder.get('hostIdentity'))
            rel = runner.run([args.python_exe, layout_helper, 'status',
                              '--lock-path', acq['path']], cwd=str(layout), label='L2-status')
            c2['statusExists'] = (helper_json(rel) or {}).get('exists')
            rrel = runner.run([args.python_exe, layout_helper, 'release', acq['token'],
                               '--lock-path', acq['path']], cwd=str(layout), label='L2-release')
            rj = helper_json(rrel) or {}
            c2['releaseRc'] = rrel['rc']
            c2['released'] = rj.get('released')
            if rrel['rc'] != 0 or not rj.get('released') or lock_file.exists():
                fail('L2', '自己的锁未能释放（rc=%s，文件仍存在=%s）' % (rrel['rc'], lock_file.exists()))
    close(c2, 'L2')

    c3 = case('L3-wrong-token-refuses')
    row = runner.run([args.python_exe, layout_helper, 'acquire', '--run-id', run_id + '-L3'],
                     cwd=str(layout), label='L3-acquire')
    acq = helper_json(row) or {}
    if not acq.get('token'):
        fail('L3', '前置取锁失败')
    else:
        lock_file = Path(acq['path'])
        before = sha256_file(lock_file)
        wrong = runner.run([args.python_exe, layout_helper, 'release', 'deadbeef' * 4,
                            '--lock-path', acq['path']], cwd=str(layout), label='L3-release-wrong')
        wj = helper_json(wrong) or {}
        c3['rc'] = wrong['rc']
        c3['released'] = wj.get('released')
        c3['error'] = wj.get('error')
        after = sha256_file(lock_file)
        c3['lockBytesUnchanged'] = before == after
        if wrong['rc'] != 78:
            fail('L3', '错误 token 应返回 78，实际 %s' % wrong['rc'])
        if wj.get('released'):
            fail('L3', '错误 token 竟然释放了锁')
        if before != after:
            fail('L3', '错误 token 释放后锁文件字节变了')
        if not lock_file.exists():
            fail('L3', '错误 token 释放后锁文件消失')
        # 清场：用正确 token 释放
        runner.run([args.python_exe, layout_helper, 'release', acq['token'],
                    '--lock-path', acq['path']], cwd=str(layout), label='L3-cleanup')
        c3['cleanupFileGone'] = not lock_file.exists()
        if lock_file.exists():
            fail('L3', '正确 token 也未能释放（清场失败，可能残留测试锁）')
    close(c3, 'L3')

    c4 = case('L4-no-import-of-historical-dirs')
    env = dict(os.environ)
    env['PYTHONVERBOSE'] = '1'
    row = runner.run([args.python_exe, layout_helper, 'status'], cwd=str(layout),
                     label='L4-verbose-status', env=env)
    js = helper_json(row)
    c4['stdoutJson'] = js
    c4['stderrBytes'] = len(row['stderrText'])
    hits = [m for m in HISTORICAL_MARKERS + ('proclock',) if m in row['stderrText']]
    c4['historicalHitsInImportTrace'] = hits
    c4['lockImpl'] = js and js.get('lockImpl')
    if js is None:
        fail('L4', 'PYTHONVERBOSE 下助手未输出 JSON')
    elif hits:
        fail('L4', '子进程 import 轨迹出现历史路径: %s' % hits)
    elif not row['stderrText'].strip():
        fail('L4', 'PYTHONVERBOSE 未产生 import 轨迹（无法证明"没导入"）')
    elif os.path.normcase(str(js.get('lockImpl', ''))) != os.path.normcase(layout_helper):
        fail('L4', '执行的不是布局内的助手: %s' % js.get('lockImpl'))
    close(c4, 'L4')
    # 静态检查只针对**随仓交付的两个工具文件**：它们不得出现任何历史目录字面量。
    # 测试驱动（hosts/portable）会显式引用夜间公共锁路径，那是"显式配置"而非隐式依赖，
    # 因此对驱动不做这条检查，但要把这个范围写清楚，避免看起来像放水。
    static_hits = {}
    for name in ('verify-examples.ps1', 'verify-examples-lock.py'):
        p = repo / 'scripts' / name
        if p.is_file():
            text = p.read_bytes().decode('utf-8', 'replace')
            found = [m for m in ('D:\\Codes\\', 'D:/Codes/', 'Goxml-agent-rounds',
                                 'Goxml-night-reports') if m in text]
            if found:
                static_hits[name] = found
    c4['staticHits'] = static_hits
    c4['staticCheckedFiles'] = ['verify-examples.ps1', 'verify-examples-lock.py']
    c4['staticSkippedFiles'] = ['test-verify-examples-hosts.py',
                                'test-verify-examples-portable.py（驱动显式传 --shared-lock，'
                                '属显式配置，不是隐式依赖）']
    if static_hits:
        fail('L4', '脚本正文仍含历史目录字面量: %s' % static_hits)

    # ---------------------------------------------------------------- L5/L6
    broken = work / '缺 助手 布局 01' / 'Goxml'
    (broken / 'scripts').mkdir(parents=True, exist_ok=True)
    shutil.copyfile(repo / 'go.mod', broken / 'go.mod')
    if (repo / 'go.sum').is_file():
        shutil.copyfile(repo / 'go.sum', broken / 'go.sum')
    shutil.copyfile(ps, broken / 'scripts' / 'verify-examples.ps1')
    broken_ps = str(broken / 'scripts' / 'verify-examples.ps1')

    c5 = case('L5-missing-helper-rc2')
    row = runner.run([args.host_exe, '-NoLogo', '-NoProfile', '-NonInteractive', '-File',
                      broken_ps, '-Example', 'tasks', '-RepoRoot', str(broken)],
                     cwd=str(work), label='L5-missing-helper')
    c5['rc'] = row['rc']
    c5['tail'] = row['stdoutText'].strip().splitlines()[-3:]
    if row['rc'] != 2:
        fail('L5', '缺锁助手应返回 2，实际 %s' % row['rc'])
    elif '锁助手缺失' not in row['stdoutText']:
        fail('L5', 'rc=2 但信息里没有"锁助手缺失"')
    elif str(broken) not in row['stdoutText']:
        fail('L5', '信息里没有打印缺失助手的完整路径（含空格/中文的布局路径）')
    close(c5, 'L5')

    c6 = case('L6-missing-python-rc2')
    row = runner.run([args.host_exe, '-NoLogo', '-NoProfile', '-NonInteractive', '-File',
                      ps, '-Example', 'tasks', '-RepoRoot', str(repo),
                      '-PythonExe', 'no-such-python-xyz'], cwd=str(work),
                     label='L6-missing-python')
    c6['rc'] = row['rc']
    c6['tail'] = row['stdoutText'].strip().splitlines()[-3:]
    if row['rc'] != 2:
        fail('L6', 'Python 不可用应返回 2，实际 %s' % row['rc'])
    elif '找不到 Python' not in row['stdoutText']:
        fail('L6', 'rc=2 但信息里没有"找不到 Python"')
    close(c6, 'L6')

    # ---------------------------------------------------------------- L7/L8
    def make_user_dir(name, with_marker):
        d = work / name
        d.mkdir(parents=True, exist_ok=True)
        (d / '用户文件.txt').write_bytes(u'这是用户内容，不得被覆盖或删除\n'.encode('utf-8'))
        if with_marker:
            (d / MARKER).write_bytes(json.dumps({
                'tool': 'verify-examples.ps1', 'createdAt': '2026-10-31T00:00:00',
                'repoRoot': str(repo), 'mode': 'isolated', 'examples': ['tasks'],
                'pid': 1, 'runId': 'verify-examples-别人的运行',
                'markerToken': '别人的markertoken', 'lockToken': None},
                ensure_ascii=False, indent=2).encode('utf-8'))
        return d

    def existing_dir_case(name, with_marker, expect_fragment):
        d = make_user_dir(name, with_marker)
        before = snapshot(d)
        row = runner.run([args.host_exe, '-NoLogo', '-NoProfile', '-NonInteractive', '-File',
                          ps, '-Example', 'tasks', '-RepoRoot', str(repo),
                          '-OutputRoot', str(d), '-GoxmlExe', str(goxml), '-SkipLock'],
                         cwd=str(work), label=name)
        after = snapshot(d)
        box = case(name)
        box['rc'] = row['rc']
        box['tail'] = row['stdoutText'].strip().splitlines()[-3:]
        box['beforeCount'] = len(before)
        box['afterCount'] = len(after)
        box['added'] = sorted(set(after) - set(before))
        box['changed'] = sorted(k for k in set(after) & set(before) if after[k] != before[k])
        if row['rc'] != 5:
            fail(name, '已存在用户目录应返回 5，实际 %s' % row['rc'])
        if box['added']:
            fail(name, '目录出现新增文件（应为零新增）: %s' % box['added'])
        if box['changed']:
            fail(name, '目录内文件被改动: %s' % box['changed'])
        if expect_fragment and expect_fragment not in row['stdoutText']:
            fail(name, '信息里缺少 "%s"' % expect_fragment)
        close(box, name)
        return box

    existing_dir_case('L7-foreign-marker-zero-write', True, '拒绝接管')
    existing_dir_case('L8-user-dir-without-marker-zero-write', False, '拒绝接管')

    # ---------------------------------------------------------------- L9
    c9 = case('L9-runid-unique-in-same-host')
    lock9 = str(work / 'L9 锁' / 'run.lock')
    outs = []
    for i in (1, 2):
        o = work / ('L9 输出 %d' % i)
        outs.append(o)
    # 两次调用放在**同一个** PowerShell 宿主里执行（PID 相同），
    # 失败点选在 marker 写入之后（源材料缺失 rc 7），避免为取证多跑两次完整构建。
    bad_src = work / 'L9 坏源材料'
    bad_src.mkdir(parents=True, exist_ok=True)
    cmd = ('; ').join(
        ["& '%s' -Example tasks -RepoRoot '%s' -OutputRoot '%s' -SourceDir '%s' "
         "-LockPath '%s' -GoxmlExe '%s'" % (ps, repo, outs[i], bad_src, lock9, goxml)
         for i in (0, 1)])
    row = runner.run([args.host_exe, '-NoLogo', '-NoProfile', '-NonInteractive',
                      '-Command', cmd], cwd=str(work), label='L9-two-calls-one-host')
    c9['hostRc'] = row['rc']
    markers = []
    for o in outs:
        m = o / MARKER
        if m.is_file():
            markers.append(json.loads(m.read_bytes().decode('utf-8')))
    c9['markerCount'] = len(markers)
    c9['markers'] = markers
    if len(markers) != 2:
        fail('L9', '两次调用应各写一份 marker，实际 %d 份' % len(markers))
    else:
        m1, m2 = markers
        c9['pid1'] = m1.get('pid')
        c9['pid2'] = m2.get('pid')
        c9['runId1'] = m1.get('runId')
        c9['runId2'] = m2.get('runId')
        c9['token1'] = m1.get('lockToken')
        c9['token2'] = m2.get('lockToken')
        if m1.get('pid') != m2.get('pid'):
            fail('L9', '两次调用不在同一宿主（pid %s != %s），用例没证明到点子上'
                 % (m1.get('pid'), m2.get('pid')))
        if m1.get('runId') == m2.get('runId'):
            fail('L9', '同一宿主两次调用 runId 相同: %s' % m1.get('runId'))
        if not m1.get('lockToken') or not m2.get('lockToken'):
            fail('L9', 'marker 里没有 lockToken，无法追溯各自取到的锁')
        elif m1.get('lockToken') == m2.get('lockToken'):
            fail('L9', '两次调用 lockToken 相同: %s' % m1.get('lockToken'))
    close(c9, 'L9')

    # ---------------------------------------------------------------- L10
    def success_attempt(attempt):
        out = work / ('L10 成功 输出 %d' % attempt)
        row = runner.run([args.host_exe, '-NoLogo', '-NoProfile', '-NonInteractive', '-File',
                          str(layout / 'scripts' / 'verify-examples.ps1'),
                          '-Example', 'tasks', '-RepoRoot', str(layout),
                          '-OutputRoot', str(out), '-LockPath', args.shared_lock,
                          '-GoxmlExe', str(goxml), '-TimeoutSec', str(args.timeout_sec)],
                         cwd=str(work), label='L10-success-%d' % attempt)
        box = {'rc': row['rc'], 'outputRoot': str(out),
               'tail': row['stdoutText'].strip().splitlines()[-4:]}
        marker_p = out / MARKER
        if marker_p.is_file():
            box['marker'] = json.loads(marker_p.read_bytes().decode('utf-8'))
        gen = out / 'tasks' / 'tasks_gen.go'
        entry = out / 'tasks-entry.exe'
        box['generatedBytes'] = gen.stat().st_size if gen.is_file() else 0
        box['entryBytes'] = entry.stat().st_size if entry.is_file() else 0
        box['logExists'] = (out / 'verify-examples-run.log').is_file()
        return box, row

    c10 = case('L10-success-in-isolated-layout')
    box, row = success_attempt(1)
    if box['rc'] == 6:
        c10['attempt1'] = box
        c10['status'] = 'lock-busy'
        c10['note'] = '公共锁被他人持有：本轮不等待、不删他人锁，记为未验证（非失败也非通过）'
        box2, row2 = success_attempt(2)
        c10['attempt2'] = box2
        if box2['rc'] != 6:
            box = box2
    c10.update({k: v for k, v in box.items() if k not in ('tail',)})
    c10['tail'] = box.get('tail')
    if box['rc'] == 6:
        c10['status'] = 'unverified-lock-busy'
    else:
        # 重试成功后要清掉首次的 lock-busy 标记，否则状态会停在中间态。
        if c10.get('status') == 'lock-busy':
            c10.pop('status', None)
            c10['retriedAfterLockBusy'] = True
        if box['rc'] != 0:
            fail('L10', '隔离布局内成功调用应返回 0，实际 %s' % box['rc'])
        else:
            if box['generatedBytes'] <= 0:
                fail('L10', '生成产物缺失或为空')
            if box['entryBytes'] <= 0:
                fail('L10', '窗口入口未构建')
            if not box.get('logExists'):
                fail('L10', '运行日志缺失')
            mk = box.get('marker') or {}
            if not mk.get('runId') or not mk.get('markerToken'):
                fail('L10', 'marker 缺少 runId/markerToken，所有权判据无从生效')
            if str(layout).lower() not in str(mk.get('repoRoot', '')).lower():
                fail('L10', 'marker 里的 repoRoot 不是隔离布局: %s' % mk.get('repoRoot'))
    close(c10, 'L10')
    c10['layout'] = str(layout)

    report['endedAt'] = now()
    report['status'] = 'PASS' if not failures else 'FAIL'
    write_json(args.report, report)
    Path(args.commands).parent.mkdir(parents=True, exist_ok=True)
    Path(args.commands).write_bytes(''.join(
        json.dumps({k: v for k, v in row.items() if k not in ('stdoutText', 'stderrText')},
                   ensure_ascii=False) + '\n' for row in runner.commands).encode('utf-8'))
    print(json.dumps({'runId': run_id, 'status': report['status'], 'failures': failures,
                      'report': args.report}, ensure_ascii=False, indent=2))
    return 0 if not failures else 1


if __name__ == '__main__':
    raise SystemExit(main())

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""R12-C：--check 失败路径的工程级 E2E（真实 CLI + 一键入口）。

R11 已验证一键工程的 --check **成功**路径；本脚本补**失败**路径，全部用真实命令取证：

用例 1 `cli-failure-e2e`（自有测试工程，允许反复写同一目录）：
  S1 两个 View 各自 `generate --check` 成功（先有成功产物）；
  S2 全命中再跑 --check：仍执行检查（stdout 出现「提交前检查通过」）；
  S3 引入「过 GXML 分析、目标包编译失败」的 Go 类型错误（doc.go 引用未定义符号）
     → miss + 检查失败（rc=2，stderr 含真实 cause），逐字节核对旧产物/Manifest/
     成功缓存全部保留、mtime 不变（未被重写）；
  S4 含命中候选的整目录批次 `generate view --check`：检查失败、全或无、旧字节保留；
  S5 无 --check 对照：生成照常成功（rc=0、已生成），失败暴露在**后续入口构建**；
  S6 修好 doc.go：再跑 --check 命中且通过，入口构建恢复通过；
  S7 只破坏目标包用户源码独有的依赖：全命中仍检查失败，旧产物与缓存逐字保留。

用例 2-4 `entry-*`（一键入口，独占输出目录，影子仓库模板内置同一类型错误）：
  E1 带 --check：失败阶段是 generate（提交前检查），build-entry/test **未启动**，
     产物/Manifest/缓存零落盘，报告 check={forwarded:true, passed:false}；
  E2 不带 --check 对照：generate 成功、失败阶段是 build-entry（后续入口构建失败）；
  E3 修好模板后新目录重跑 --check：全过、check.passed=true、离屏 11 用例全绿。

变异（`--make-mutated-repo`，可复现、可编译）：
  * `skip-check`：precommit 检查整体跳过 → 用例 1 的 S1/S3 判据翻红；
  * `ignore-build-error`：检查子进程失败被吞 → 同样翻红（不同层）。

边界：零原生窗口（入口只构建不运行）、不安装、不联网；重负载自持公共锁、
finally 按自有 token 释放；一键入口用例不嵌套取锁（入口自己拿锁）。
"""
import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import uuid
from datetime import datetime

GO = r'C:\Program Files\Go\bin\go.exe'
LOCK = r'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock'
PROJECT = 'crosspackage-preferences'
TOOL = 'verify-projects.py'
THIS = 'test-precommit-fail-e2e.py'

# 引入到目标包用户源里的类型错误：引用一个不存在的包级符号。
# 它能通过：发现（go list 不做类型检查）、包名解析、命名预检（纯语法）、
# GXML 分析（只加载 VM=model 包）——恰好在 precommit 检查处爆出来。
TYPE_ERROR_LINE = 'var _ = noSuchSymbolR12C\n'

MUTATION_ANCHORS = {
    # 变异 1：提交前检查整体跳过（返回 nil）。
    'skip-check': [
        ('\tif len(cands) == 0 {\n\t\treturn nil\n\t}',
         '\tif true {\n\t\treturn nil\n\t}'),
    ],
    # 变异 2：检查子进程失败被当成通过（吞掉 go build 的非零退出）。
    'ignore-build-error': [
        ('\tif err == nil {\n\t\treturn nil\n\t}',
         '\tif err == nil || true {\n\t\treturn nil\n\t}'),
    ],
}


def now():
    return datetime.now().astimezone().isoformat(timespec='milliseconds')


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def read_bytes(path):
    with open(path, 'rb') as fh:
        return fh.read()


def load_helper(repo, filename, module_name):
    path = os.path.join(repo, 'scripts', filename)
    if not os.path.isfile(path):
        raise SystemExit('仓库缺少助手脚本 %s' % path)
    spec = importlib.util.spec_from_file_location(module_name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def patch_file(path, old, new, encoding='utf-8'):
    """按锚点替换；锚点必须唯一命中，否则直接失败（防止变异悄悄落空）。"""
    text = read_bytes(path).decode(encoding)
    if text.count(old) != 1:
        raise AssertionError('锚点在 %s 命中 %d 次（应为 1）：%r' % (path, text.count(old), old))
    with open(path, 'wb') as fh:
        fh.write(text.replace(old, new).encode(encoding))


def build_shadow(src_repo, dest, mutate=None):
    """整份复制仓库（不含 .git）做影子仓库；mutate 只改 CLI 检查实现。"""
    shutil.copytree(src_repo, dest, ignore=shutil.ignore_patterns('.git'),
                    ignore_dangling_symlinks=True)
    if mutate:
        for old, new in MUTATION_ANCHORS[mutate]:
            patch_file(os.path.join(dest, 'internal', 'cli', 'precommit_check.go'), old, new)
    return dest


class Ctx(object):
    """用例上下文：独立目录 + run_capture 取证。"""

    def __init__(self, case, work_root, repo, procmod, tool, report):
        self.case = case
        self.repo = repo
        self.procmod = procmod
        self.tool = tool
        self.report = report
        self.dir = os.path.join(work_root, case)
        tool.ensure_no_reparse(self.dir)
        os.mkdir(self.dir)
        self.logs = os.path.join(self.dir, 'logs')
        self.report['cases'][case] = {'workDir': self.dir, 'steps': [], 'failures': []}

    def path(self, *parts):
        return os.path.join(self.dir, *parts)

    def fail(self, msg):
        self.report['cases'][self.case]['failures'].append(msg)

    def step(self, step_id, argv, cwd):
        """真实命令 + 完整取证（argv/cwd/rc/收口/raw 哈希全在 logs/<case_id>/）。"""
        res = self.procmod.run_capture([str(a) for a in argv], step_id, self.logs,
                                       cwd=cwd, timeout=900.0,
                                       env_overrides=self.procmod.GO_ENV_FIXED,
                                       containment='auto')
        out_raw = read_bytes(res['stdout']['path']) if res['stdout'].get('path') else b''
        err_raw = read_bytes(res['stderr']['path']) if res['stderr'].get('path') else b''
        if not res['ok'] or res['timedOut'] or res['rcSource'] != 'observed':
            self.fail('%s 子进程结果不可信：%r' % (step_id, res['okReasons']))
        entry = {
            'id': step_id, 'argv': res['argv'], 'cwd': res['cwd'], 'rc': res['rc'],
            'timedOut': res['timedOut'], 'infraOk': res['ok'],
            'infraReasons': res['okReasons'], 'caseDir': res['caseDir'],
            'stdout': {'bytes': res['stdout']['bytes'], 'sha256': res['stdout']['sha256']},
            'stderr': {'bytes': res['stderr']['bytes'], 'sha256': res['stderr']['sha256']},
            'markers': markers_of(out_raw, err_raw, self.tool),
        }
        self.report['cases'][self.case]['steps'].append(entry)
        return res, out_raw, err_raw

    def check(self, cond, msg):
        if not cond:
            self.fail(msg)
        return cond


def markers_of(out_raw, err_raw, tool):
    """从原始字节里提取本轮关心的 CLI 输出标记（判据用标记，不用日志全文）。"""
    out = out_raw.decode('utf-8', 'replace')
    err = err_raw.decode('utf-8', 'replace')
    check_passed, check_failed = tool.check_markers(out_raw, err_raw)
    return {
        'hit': '（缓存命中）' in out,
        'miss': '缓存未命中' in out,
        'generated': '已生成' in out,
        'checkPassed': check_passed,
        'checkFailed': check_failed,
        'typeError': 'undefined: noSuchSymbolR12C' in err,
    }


# ---------------------------------------------------------------- 快照与逐字核对


def snapshot(project, skip=('.verify-projects',)):
    """工程内全部文件：相对路径 → (字节 sha256, 大小, mtime_ns)。"""
    out = {}
    skip_names = {os.path.normcase(s) for s in skip}
    for dirpath, dirnames, filenames in os.walk(project):
        dirnames[:] = [d for d in sorted(dirnames) if os.path.normcase(d) not in skip_names]
        for name in sorted(filenames):
            full = os.path.join(dirpath, name)
            st = os.stat(full)
            rel = os.path.relpath(full, project).replace('\\', '/')
            out[rel] = {'sha256': sha256_file(full), 'bytes': st.st_size,
                        'mtimeNs': st.st_mtime_ns}
    return out


def diff_snapshot(a, b, only=None):
    """比较两份快照；only 限定关心的相对路径集合。返回差异描述列表。"""
    keys = set(a) & set(b) if only is None else set(only)
    problems = []
    for k in sorted(keys):
        if k not in a or k not in b:
            problems.append('缺少需比较的文件 %s' % k)
            continue
        if (a[k]['sha256'], a[k]['bytes']) != (b[k]['sha256'], b[k]['bytes']):
            problems.append('内容变了 %s（%s → %s）' % (k, a[k]['sha256'][:12], b[k]['sha256'][:12]))
        elif a[k]['mtimeNs'] != b[k]['mtimeNs']:
            problems.append('被重写但字节相同 %s（mtime 变化）' % k)
    for k in sorted(set(b) - set(a)):
        problems.append('新增 %s' % k)
    for k in sorted(set(a) - set(b)):
        problems.append('消失 %s' % k)
    return problems


# ---------------------------------------------------------------- 用例 1：自有工程

KEY_ARTIFACTS = ['view/edit/edit_gen.go', 'view/summary/summary_gen.go']
KEY_MANIFEST = '.goxml/generate-manifest.json'


def case_cli_failure_e2e(ctx):
    """S1→S6 的完整失败路径序列。工程由一键入口 --copy-only 创建（材料同源）。"""
    lockmod = load_helper(ctx.repo, 'verify-examples-lock.py', 'lock_%s' % uuid.uuid4().hex[:6])
    lock = lockmod.Lock(LOCK)
    try:
        lock.acquire('r12c-e2e', 'cli-failure-e2e 全序列', 'r12c-cli-%s' % uuid.uuid4().hex[:8])
    except lockmod.LockHeldError as exc:
        return ['SKIP: 公共锁被他人持有（%r），本轮不启动 Go'
                % ((exc.holder or {}).get('agent'),)]

    try:
        _cli_failure_e2e_body(ctx)
    finally:
        released = lock.release()
        ctx.report['lockReleased'] = released
        if not released:
            ctx.fail('公共锁释放失败：%s' % lock.release_error)
    return ctx.report['cases'][ctx.case]['failures']


def _cli_failure_e2e_body(ctx):
    repo = ctx.repo
    # 0) 真实构建 CLI（被测实现；变异仓库在这里被测）
    cli = ctx.path('goxml.exe')
    res, _, err = ctx.step('s0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], repo)
    if not ctx.check(res['ok'] and res['rc'] == 0, 'CLI 构建失败 rc=%s：%s'
                     % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
        return ctx.report['cases'][ctx.case]['failures']

    # 1) 自有工程：一键入口 --copy-only（材料同源；独占创建）
    proj = ctx.path('工程 失败路径')
    env = dict(os.environ, PYTHONIOENCODING='utf-8')
    copy_proc = subprocess.run(
        [sys.executable, os.path.join(repo, 'scripts', TOOL), PROJECT,
         '--out-dir', proj, '--repo', repo, '--copy-only'],
        cwd=ctx.dir, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    with open(ctx.path('copy-stdout.txt'), 'wb') as fh:
        fh.write(copy_proc.stdout)
    with open(ctx.path('copy-stderr.txt'), 'wb') as fh:
        fh.write(copy_proc.stderr)
    if not ctx.check(copy_proc.returncode == 0,
                     '一键入口 --copy-only 失败 rc=%d：%r'
                     % (copy_proc.returncode, copy_proc.stderr.decode('utf-8', 'replace')[-300:])):
        return ctx.report['cases'][ctx.case]['failures']

    # 这个包只被目标包的用户 Go 源码引用，不属于 VM 的依赖闭包。
    dep = ctx.path('工程 失败路径', 'checkdep', 'dep.go')
    os.makedirs(os.path.dirname(dep))
    dep_ok = b'package checkdep\nconst Ready = 1\n'
    with open(dep, 'wb') as fh:
        fh.write(dep_ok)
    with open(os.path.join(proj, 'view', 'edit', 'checkdep_testfixture.go'), 'wb') as fh:
        fh.write(b'package edit\nimport "example.com/crosspackage-preferences/checkdep"\n'
                 b'var _ = checkdep.Ready\n')

    # S1 两个 View 先成功生成（带 --check）
    for rel in ('view/edit/edit.gxml', 'view/summary/summary.gxml'):
        res, out, err = ctx.step('s1-gen-' + rel.replace('/', '-'), [cli, 'generate', rel, '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 0, 'S1 %s 应成功，rc=%s：%s' % (rel, res['rc'], err.decode('utf-8', 'replace')[-300:]))
        ctx.check(m['checkPassed'], 'S1 %s 应出现「提交前检查通过」（检查真的执行了）' % rel)
        ctx.check(m['generated'], 'S1 %s 应已生成' % rel)
    snap1 = snapshot(proj)
    ctx.check(all(k in snap1 for k in KEY_ARTIFACTS + [KEY_MANIFEST]),
              'S1 后应有产物与 Manifest：%r' % sorted(snap1))
    cache_files_1 = sorted(k for k in snap1 if k.startswith('.goxml/cache/'))
    ctx.check(len(cache_files_1) == 2, 'S1 后成功缓存应有 2 条记录，实得 %r' % cache_files_1)

    # S2 全命中再跑 --check：检查必须仍执行（判据：命中标记 + 检查通过标记）
    for rel in ('view/edit/edit.gxml',):
        res, out, err = ctx.step('s2-hit-check-' + rel.replace('/', '-'), [cli, 'generate', rel, '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(m['hit'], 'S2 应是缓存命中（实得 %r）' % m)
        ctx.check(res['rc'] == 0, 'S2 命中+检查应成功，rc=%s' % res['rc'])
        ctx.check(m['checkPassed'], 'S2 全命中也必须执行提交前检查（stdout 缺「提交前检查通过」）')
    problems = diff_snapshot(snap1, snapshot(proj), only=KEY_ARTIFACTS + [KEY_MANIFEST])
    ctx.check(not problems, 'S2 全命中不得重写产物/Manifest：%r' % problems)

    # S3 引入类型错误：doc.go 引用未定义符号（过分析、坏编译）
    doc = os.path.join(proj, 'view', 'edit', 'doc.go')
    doc_orig = read_bytes(doc)
    with open(doc, 'wb') as fh:
        fh.write(doc_orig + ('\n' + TYPE_ERROR_LINE).encode('utf-8'))
    res, out, err = ctx.step('s3-broken-check', [cli, 'generate', 'view/edit/edit.gxml', '--check'], proj)
    m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
    ctx.check(res['rc'] != 0, 'S3 带类型错误时 --check 必须失败，实得 rc=%s' % res['rc'])
    ctx.check(m['checkFailed'], 'S3 stderr 应含「提交前检查失败」：%r'
              % err.decode('utf-8', 'replace')[-300:])
    ctx.check(m['typeError'], 'S3 失败原因应是目标包类型错误（undefined: noSuchSymbolR12C）')
    ctx.check(m['miss'], 'S3 doc.go 变化使本输入 miss（package-sources-changed），实得 %r' % m)
    ctx.check(not m['generated'], 'S3 检查失败不得声称已生成')
    # 逐字核对：旧产物 / Manifest / 成功缓存全部保留（字节 + mtime 都不变）
    snap3 = snapshot(proj)
    problems = diff_snapshot(snap1, snap3, only=KEY_ARTIFACTS + [KEY_MANIFEST] + cache_files_1)
    ctx.check(not problems, 'S3 失败后旧产物/Manifest 必须逐字保留：%r' % problems)
    ctx.check(read_bytes(doc) == doc_orig + ('\n' + TYPE_ERROR_LINE).encode('utf-8'),
              'S3 只应改 doc.go（我们自己的注入）')
    cache_files_3 = sorted(k for k in snap3 if k.startswith('.goxml/cache/'))
    ctx.check(cache_files_3 == cache_files_1,
              'S3 失败不得写/删成功缓存：%r vs %r' % (cache_files_1, cache_files_3))

    # S4 含命中候选的整目录批次：edit miss + summary hit，检查失败、全或无
    res, out, err = ctx.step('s4-batch-check', [cli, 'generate', 'view', '--check'], proj)
    m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
    ctx.check(res['rc'] != 0, 'S4 批次检查必须失败，实得 rc=%s' % res['rc'])
    ctx.check(m['checkFailed'] and m['typeError'],
              'S4 批次失败原因应是同一类型错误：checkFailed=%s typeError=%s' % (m['checkFailed'], m['typeError']))
    ctx.check('跳过 view/summary/summary_gen.go（缓存命中）' in out.decode('utf-8', 'replace'),
              'S4 批次里 summary 应是命中候选（命中也要被检查）')
    snap4 = snapshot(proj)
    problems = diff_snapshot(snap1, snap4, only=KEY_ARTIFACTS + [KEY_MANIFEST] + cache_files_1)
    ctx.check(not problems, 'S4 批次全或无，旧产物/Manifest 逐字保留：%r' % problems)
    cache_files_4 = sorted(k for k in snap4 if k.startswith('.goxml/cache/'))
    ctx.check(cache_files_4 == cache_files_1, 'S4 不得改成功缓存：%r' % cache_files_4)

    # S5 无 --check 对照：生成成功，失败暴露在**后续入口构建**
    res, out, err = ctx.step('s5-broken-nocheck', [cli, 'generate', 'view/edit/edit.gxml'], proj)
    m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
    ctx.check(res['rc'] == 0, 'S5 无 --check 时生成必须照常成功，rc=%s' % res['rc'])
    ctx.check(m['generated'], 'S5 应已生成（无检查路径不受影响）')
    entry_exe = ctx.path('preferences-s5.exe')
    res, out, err = ctx.step('s5-entry-build', [GO, 'build', '-o', entry_exe, './cmd/preferences'], proj)
    m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
    ctx.check(res['rc'] != 0, 'S5 后续入口构建必须失败（类型错误暴露在这里）')
    ctx.check(m['typeError'], 'S5 入口构建失败原因应是 undefined: noSuchSymbolR12C')
    snap5 = snapshot(proj)
    ctx.check(all(k in snap5 for k in KEY_ARTIFACTS), 'S5 产物仍在（无检查路径照写）')
    cache_files_5 = sorted(k for k in snap5 if k.startswith('.goxml/cache/'))
    ctx.check(len(cache_files_5) > len(cache_files_1),
              'S5 无检查路径会为新键写成功缓存（对照 S3 的零写盘）：%d 条' % len(cache_files_5))

    # S6 修好 doc.go：重新生成并通过（命中 + 检查通过 + 入口构建恢复）
    with open(doc, 'wb') as fh:
        fh.write(doc_orig)
    res, out, err = ctx.step('s6-fixed-check', [cli, 'generate', 'view/edit/edit.gxml', '--check'], proj)
    m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
    ctx.check(res['rc'] == 0, 'S6 修复后 --check 应通过，rc=%s：%s'
              % (res['rc'], err.decode('utf-8', 'replace')[-200:]))
    ctx.check(m['checkPassed'], 'S6 修复后应出现「提交前检查通过」')
    ctx.check(m['hit'], 'S6 修复后回到原键应命中（实得 %r）' % m)
    entry_exe2 = ctx.path('preferences-s6.exe')
    res, out, err = ctx.step('s6-entry-build', [GO, 'build', '-o', entry_exe2, './cmd/preferences'], proj)
    ctx.check(res['rc'] == 0, 'S6 修复后入口构建应恢复通过，rc=%s' % res['rc'])

    # S7 只改目标包独有的依赖。生成缓存仍全命中，编译却应失败。
    with open(dep, 'wb') as fh:
        fh.write(dep_ok + TYPE_ERROR_LINE.encode('utf-8'))
    before = snapshot(proj)
    res, out, err = ctx.step('s7-all-hit-check-fails',
                             [cli, 'generate', 'view/edit/edit.gxml', '--check'], proj)
    m = markers_of(out, err, ctx.tool)
    ctx.check(res['rc'] == 2 and m['hit'] and not m['miss'] and not m['generated'],
              'S7 必须全命中且失败，rc=%s markers=%r' % (res['rc'], m))
    ctx.check(m['checkFailed'] and m['typeError'],
              'S7 应由原生检查报告目标包独有依赖的类型错误：%r' % m)
    problems = diff_snapshot(before, snapshot(proj))
    ctx.check(not problems, 'S7 检查失败不得改写工程（含产物和缓存）：%r' % problems)

    # 模板源码全程未被写
    return ctx.report['cases'][ctx.case]['failures']


# ---------------------------------------------------------------- 用例 2-4：一键入口


def run_entry(ctx, out_dir, repo, *extra, json_report=None):
    argv = [sys.executable, os.path.join(repo, 'scripts', TOOL), PROJECT,
            '--out-dir', out_dir, '--repo', repo,
            '--lock-path', LOCK]
    argv.extend(extra)
    if json_report:
        argv.extend(['--json', json_report])
    env = dict(os.environ, PYTHONIOENCODING='utf-8')
    proc = subprocess.run(argv, cwd=ctx.dir, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE)
    with open(ctx.path(os.path.basename(out_dir) + '-stdout.txt'), 'wb') as fh:
        fh.write(proc.stdout)
    with open(ctx.path(os.path.basename(out_dir) + '-stderr.txt'), 'wb') as fh:
        fh.write(proc.stderr)
    return {'rc': proc.returncode,
            'stdout': proc.stdout.decode('utf-8', 'replace'),
            'stderr': proc.stderr.decode('utf-8', 'replace'), 'argv': argv}


def load_report(path):
    with open(path, encoding='utf-8') as fh:
        return json.load(fh)


def case_entry_check_fails(ctx):
    """E1：一键入口 + --check：失败阶段=generate（提交前检查），后续阶段未启动。"""
    shadow = build_shadow(ctx.repo, ctx.path('shadow-broken'))
    tpl_doc = os.path.join(shadow, 'examples', 'gxml', 'testprojects', PROJECT,
                           'view', 'edit', 'doc.go.txt')
    with open(tpl_doc, 'ab') as fh:
        fh.write(('\n' + TYPE_ERROR_LINE).encode('utf-8'))
    out = ctx.path('E1 工程')
    rep = ctx.path('e1-report.json')
    res = run_entry(ctx, out, shadow, '--check', json_report=rep)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，一键入口未启动（rc=8）']
    data = load_report(rep)
    fails = []
    if res['rc'] != 4:
        fails.append('E1 应以业务失败退 4，实得 %d（stderr=%r）' % (res['rc'], res['stderr'][-300:]))
    names = [s['name'] for s in data['stages']]
    want = ['copy', 'build-cli', 'generate:view/edit/edit.gxml']
    if names != want:
        fails.append('E1 失败阶段应停在 generate:edit，实际阶段序列 %r（build-entry/test 不得启动）' % (names,))
    gen = [s for s in data['stages'] if s['name'].startswith('generate:')]
    for s in gen:
        if s.get('rc') == 0:
            fails.append('E1 generate 阶段退出码应为非 0：%r' % s.get('rc'))
        if s.get('checkReported') is not False:
            fails.append('E1 generate 阶段 checkReported 应为 false（观察到检查失败）：%r'
                         % s.get('checkReported'))
        if s.get('checkFailureObserved') is not True:
            fails.append('E1 generate 阶段 checkFailureObserved 应为 true（原生检查已执行并失败）：%r'
                         % s.get('checkFailureObserved'))
        raw = read_raw_of(s)
        if '提交前检查失败' not in raw['err']:
            fails.append('E1 generate 阶段 stderr 应含「提交前检查失败」：%r' % raw['err'][-200:])
        if 'undefined: noSuchSymbolR12C' not in raw['err']:
            fails.append('E1 失败原因应是目标包类型错误：%r' % raw['err'][-200:])
    chk = data.get('check') or {}
    if chk.get('forwarded') is not True or chk.get('passed') is not False:
        fails.append('E1 报告 check 应为 forwarded=true/passed=false（原生检查已执行并失败）：%r' % chk)
    if '已执行并失败' not in (chk.get('note') or ''):
        fails.append('E1 check.note 应写明「原生提交前检查已执行并失败」：%r' % chk.get('note'))
    v = data.get('verdict') or {}
    if v.get('code') != 4 or (v.get('detail') or {}).get('stage') != 'generate:view/edit/edit.gxml':
        fails.append('E1 verdict 应写明实际失败阶段 generate:edit：%r' % v)
    if not os.path.isfile(os.path.join(out, 'view', 'edit', 'edit_gen.go')):
        pass  # 正确：不落产物
    else:
        fails.append('E1 检查失败却落了 edit_gen.go')
    if os.path.isfile(os.path.join(out, 'view', 'summary', 'summary_gen.go')):
        fails.append('E1 第一条 generate 失败后不应继续 summary')
    if os.path.exists(os.path.join(out, '.goxml', 'generate-manifest.json')):
        fails.append('E1 检查失败不得落 Manifest')
    if os.path.exists(os.path.join(out, '.goxml', 'cache')) and \
            os.listdir(os.path.join(out, '.goxml', 'cache')):
        fails.append('E1 检查失败不得写成功缓存')
    lock = data.get('lock') or {}
    if lock.get('released') is not True:
        fails.append('E1 失败路径也必须按 token 释放锁：%r' % lock.get('released'))
    if not (lock.get('tokenFingerprint') or '').startswith('sha256:'):
        fails.append('E1 应有 token 指纹：%r' % lock.get('tokenFingerprint'))
    return fails


def read_raw_of(stage):
    out = stage.get('stdout') or {}
    err = stage.get('stderr') or {}
    out_t = read_bytes(out['path']).decode('utf-8', 'replace') if out.get('path') and os.path.isfile(out['path']) else ''
    err_t = read_bytes(err['path']).decode('utf-8', 'replace') if err.get('path') and os.path.isfile(err['path']) else ''
    return {'out': out_t, 'err': err_t}


def case_entry_no_check_build_fails(ctx):
    """E2 对照：同一坏模板、不带 --check：generate 成功，失败在 build-entry。"""
    shadow = build_shadow(ctx.repo, ctx.path('shadow-broken2'))
    tpl_doc = os.path.join(shadow, 'examples', 'gxml', 'testprojects', PROJECT,
                           'view', 'edit', 'doc.go.txt')
    with open(tpl_doc, 'ab') as fh:
        fh.write(('\n' + TYPE_ERROR_LINE).encode('utf-8'))
    out = ctx.path('E2 工程')
    rep = ctx.path('e2-report.json')
    res = run_entry(ctx, out, shadow, json_report=rep)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，一键入口未启动（rc=8）']
    data = load_report(rep)
    fails = []
    if res['rc'] != 4:
        fails.append('E2 应以业务失败退 4（build-entry），实得 %d' % res['rc'])
    names = [s['name'] for s in data['stages']]
    if 'build-entry' not in names or 'test' in names:
        fails.append('E2 阶段应含 build-entry 且不得有 test（构建失败即停）：%r' % (names,))
    for s in data['stages']:
        if s['name'].startswith('generate:') and s.get('rc') != 0:
            fails.append('E2 无 --check 时 generate 必须成功：%s rc=%r' % (s['name'], s.get('rc')))
    build = [s for s in data['stages'] if s['name'] == 'build-entry']
    if build:
        raw = read_raw_of(build[0])
        if 'undefined: noSuchSymbolR12C' not in raw['err']:
            fails.append('E2 build-entry 失败原因应是目标包类型错误：%r' % raw['err'][-200:])
    v = data.get('verdict') or {}
    if (v.get('detail') or {}).get('stage') != 'build-entry':
        fails.append('E2 verdict 应写明实际失败阶段 build-entry：%r' % v)
    chk = data.get('check') or {}
    if chk.get('forwarded') is not False:
        fails.append('E2 未要求 --check 时 check.forwarded 应为 false：%r' % chk)
    if not os.path.isfile(os.path.join(out, 'view', 'edit', 'edit_gen.go')):
        fails.append('E2 无检查路径应照常落产物')
    lock = data.get('lock') or {}
    if lock.get('released') is not True:
        fails.append('E2 失败路径也必须按 token 释放锁：%r' % lock.get('released'))
    return fails


def case_entry_fixed_passes(ctx):
    """E3：同一影子仓库修好模板（恢复字节）后，新目录重跑 --check 全过。"""
    shadow = build_shadow(ctx.repo, ctx.path('shadow-fixed'))
    out = ctx.path('E3 工程 修好')
    rep = ctx.path('e3-report.json')
    res = run_entry(ctx, out, shadow, '--check', json_report=rep)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，一键入口未启动（rc=8）']
    data = load_report(rep)
    fails = []
    if res['rc'] != 0:
        fails.append('E3 修好后应全过，实得 rc=%d（stderr=%r）' % (res['rc'], res['stderr'][-300:]))
        return fails
    names = [s['name'] for s in data['stages']]
    want = ['copy', 'build-cli', 'generate:view/edit/edit.gxml',
            'generate:view/summary/summary.gxml', 'build-entry', 'test']
    if names != want:
        fails.append('E3 阶段序列应完整：%r' % (names,))
    for s in data['stages']:
        if s['name'].startswith('generate:') and s.get('checkReported') is not True:
            fails.append('E3 generate 阶段 checkReported 应为 true：%s' % s['name'])
    chk = data.get('check') or {}
    if chk.get('forwarded') is not True or chk.get('passed') is not True:
        fails.append('E3 报告 check 应为 forwarded=true/passed=true（观察到检查成功标记）：%r' % chk)
    test = [s for s in data['stages'] if s['name'] == 'test']
    if test:
        markers = test[0].get('markers') or {}
        if markers.get('pass', 0) < 1 or markers.get('skip') or markers.get('fail'):
            fails.append('E3 离屏测试标记不符：%r' % (markers,))
    if 'VERIFY-PROJECTS-OK' not in res['stdout']:
        fails.append('E3 应打印成功完成标记')
    return fails


CASES = [
    ('cli-failure-e2e', case_cli_failure_e2e),
    ('entry-check-fails', case_entry_check_fails),
    ('entry-no-check-build-fails', case_entry_no_check_build_fails),
    ('entry-fixed-passes', case_entry_fixed_passes),
]


def main(argv):
    parser = argparse.ArgumentParser(prog=THIS, description=__doc__.split('\n')[0])
    parser.add_argument('--work-root', default=None, help='用例工作根目录（跑用例时必填）')
    parser.add_argument('--report', default=None, help='汇总报告 JSON 路径')
    parser.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    parser.add_argument('--case', action='append', default=None, help='只跑指定用例（可多次）')
    parser.add_argument('--make-mutated-repo', default=None, metavar='DEST',
                        help='在 DEST 生成变异后的影子仓库并退出（不跑用例）')
    parser.add_argument('--mutate', default=None, help='变异名：%s' % '|'.join(sorted(MUTATION_ANCHORS)))
    args = parser.parse_args(argv)

    repo = os.path.abspath(args.repo) if args.repo else os.path.dirname(
        os.path.dirname(os.path.abspath(__file__)))
    if not os.path.isfile(os.path.join(repo, 'scripts', TOOL)):
        sys.stderr.write('仓库缺少 %s：%s\n' % (TOOL, repo))
        return 2

    if args.make_mutated_repo:
        if not args.mutate or args.mutate not in MUTATION_ANCHORS:
            sys.stderr.write('--make-mutated-repo 需要 --mutate <%s>\n'
                             % '|'.join(sorted(MUTATION_ANCHORS)))
            return 2
        if os.path.exists(args.make_mutated_repo):
            sys.stderr.write('目标已存在（不覆盖历史证据）：%s\n' % args.make_mutated_repo)
            return 2
        build_shadow(repo, args.make_mutated_repo, args.mutate)
        print(args.make_mutated_repo)
        return 0
    if not args.work_root:
        sys.stderr.write('跑用例必须给 --work-root\n')
        return 2

    known = {name for name, _ in CASES}
    wanted = args.case or [name for name, _ in CASES]
    unknown = set(wanted) - known
    if unknown:
        parser.error('未知用例：%s' % ', '.join(sorted(unknown)))

    tool = load_helper(repo, TOOL, 'verify_projects')
    procmod = load_helper(repo, 'verify_examples_proc.py', 'proc_%s' % uuid.uuid4().hex[:6])
    try:
        tool.ensure_no_reparse(args.work_root)
        os.makedirs(args.work_root, exist_ok=True)
        if args.report:
            tool.ensure_no_reparse(args.report)
            if os.path.lexists(args.report):
                raise FileExistsError(args.report)
    except (OSError, tool.Refused) as exc:
        parser.error('报告或用例目录不可用：%s' % exc)

    report = {'schema': 'r12c-precommit-fail-e2e-1', 'repo': repo,
              'startedAt': now(), 'cases': {}, 'status': None}
    failures = 0
    skipped = 0
    for name, func in CASES:
        if name not in wanted:
            continue
        try:
            ctx = Ctx(name, args.work_root, repo, procmod, tool, report)
            problems = func(ctx)
        except Exception as exc:  # 用例自身炸了也记失败，不能静默通过
            report['cases'].setdefault(name, {'steps': []})
            problems = ['%s: %s' % (type(exc).__name__, exc)]
        real = [p for p in problems if not p.startswith('SKIP:')]
        skips = [p for p in problems if p.startswith('SKIP:')]
        entry = report['cases'][name]
        entry['failures'] = real
        if real:
            entry['status'] = 'failed'
            failures += 1
        elif skips:
            entry['status'] = 'skipped'
            entry['note'] = skips[0]
            skipped += 1
        else:
            entry['status'] = 'passed'
        print('%-28s %-8s %s' % (name, entry['status'],
                                 real[0] if real else entry.get('note', '')))

    report['endedAt'] = now()
    report['failures'] = failures
    report['skipped'] = skipped
    report['status'] = 'FAIL' if failures else ('INCOMPLETE' if skipped else 'PASS')
    if args.report:
        try:
            tool.write_json(args.report, report)
        except (OSError, tool.Refused) as exc:
            parser.error('无法写入汇总报告：%s' % exc)
    print(json.dumps({'status': report['status'], 'failed': failures, 'skipped': skipped,
                      'report': args.report}, ensure_ascii=False))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))

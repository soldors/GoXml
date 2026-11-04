#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""R13-C：crosspackage-preferences 的一键入口改成**一次目录 generate** 覆盖两页。

R12-C 已验证 --check 的失败路径，但一键入口仍是「每个 View 一条 generate」。本轮
把两页并成一条真实目录命令，并用真实 CLI / 真实 argv / 原始日志取证：

用例 1 `entry-batch-success`（一键入口，独占输出目录）：
  阶段序列里 `generate` **只有一个**，argv 是 `generate view --out-dir view --check`；
  stdout 出现两次「已生成」与 `提交前检查通过：2 个目标包`；产物/Manifest 落点与
  逐 View 模式一致；入口构建通过、离屏 11 用例全绿。

用例 2 `own-project-cycle`（自有实验工程，允许反复写同一目录）：
  B1 同批生成（两页都 miss、检查覆盖 2 个目标包）→ B2 下一次真命中（跳过×2）且
  `--check` 仍执行、零重写（字节 + mtime）→ B3 一个视图所在包编译失败：两页旧产物、
  Manifest、成功缓存全部保留 → B4 修好后恢复通过（命中 + 检查通过）。

用例 3 `per-view-half-batch`（同一份材料，故意拆回逐页调用）：
  第一页成功即落盘、第二页检查失败 → 工程进入「一页新、一页旧」的混合状态。
  这是「为什么必须一个批次」的对照证据，也是变异要复现的形态。

用例 4 `equivalence-per-view`：
  同一模板分别用 R12 的逐 View argv 与新批次 argv 各生成一次，比对两页产物字节、
  Manifest、成功缓存键集合与离屏 11 用例——批次不得改变任何实际产物。

变异（`--make-mutated-repo --mutate per-view-loop`，可复现）：
  把 `generate_batch(...)` 一处调用换回逐页循环 → 用例 1 的「只启动一次」判据翻红
  （阶段数 2、每个 `checkTargets` 只有 1、没有 `generate:view`）。

边界：零原生窗口（入口只构建不运行）、不安装、不联网；自有工程用例自持公共锁并在
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
THIS = 'test-crosspackage-batch-e2e.py'

# 与 R12-C 同源的注入方式：过发现/包名解析/命名预检/GXML 分析，恰在提交前检查爆出。
TYPE_ERROR_LINE = 'var _ = noSuchSymbolR13C\n'

EDIT_GEN = 'view/edit/edit_gen.go'
SUMMARY_GEN = 'view/summary/summary_gen.go'
MANIFEST = '.goxml/generate-manifest.json'
KEY_ARTIFACTS = [EDIT_GEN, SUMMARY_GEN]

# 变异：把「一次目录 generate」拆回「每页一条命令」（R12 / 旧脚本的形态）。
MUTATIONS = {
    'per-view-loop': [(
        "        generate_batch(report, procmod, cli_path, out_dir, log_dir, args)\n",
        "        for _rel in report['gxml']:\n"
        "            _dir = os.path.dirname(_rel) or '.'\n"
        "            _argv = [cli_path, 'generate', _rel, '--out-dir', _dir]\n"
        "            if args.check:\n"
        "                _argv.append('--check')\n"
        "            run_generate_stage(report, procmod, 'generate:' + _rel, _argv,\n"
        "                               out_dir, log_dir, args.timeout, args.check)\n",
    )],
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
        raise AssertionError('锚点在 %s 命中 %d 次（应为 1）：%r'
                             % (path, text.count(old), old))
    with open(path, 'wb') as fh:
        fh.write(text.replace(old, new).encode(encoding))


def build_shadow(src_repo, dest, mutate=None):
    """整份复制仓库（不含 .git）做影子仓库；mutate 只改一键脚本的生成调用。"""
    shutil.copytree(src_repo, dest, ignore=shutil.ignore_patterns('.git'),
                    ignore_dangling_symlinks=True)
    if mutate:
        for old, new in MUTATIONS[mutate]:
            patch_file(os.path.join(dest, 'scripts', TOOL), old, new)
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

    def check(self, cond, msg):
        if not cond:
            self.fail(msg)
        return cond

    def step(self, step_id, argv, cwd):
        """真实命令 + 完整取证（argv/cwd/rc/收口/raw 哈希全在 logs/<step_id>/）。"""
        res = self.procmod.run_capture([str(a) for a in argv], step_id, self.logs,
                                       cwd=cwd, timeout=1800.0,
                                       env_overrides=self.procmod.GO_ENV_FIXED,
                                       containment='auto')
        out_raw = read_bytes(res['stdout']['path']) if res['stdout'].get('path') else b''
        err_raw = read_bytes(res['stderr']['path']) if res['stderr'].get('path') else b''
        if not res['ok'] or res['timedOut'] or res['rcSource'] != 'observed':
            self.fail('%s 子进程结果不可信：%r' % (step_id, res['okReasons']))
        entry = {
            'id': step_id, 'argv': res['argv'], 'cwd': res['cwd'], 'rc': res['rc'],
            'timedOut': res['timedOut'], 'infraOk': res['ok'],
            'caseDir': res['caseDir'],
            'stdout': {'bytes': res['stdout']['bytes'], 'sha256': res['stdout']['sha256']},
            'stderr': {'bytes': res['stderr']['bytes'], 'sha256': res['stderr']['sha256']},
            'markers': markers_of(out_raw, err_raw, self.tool),
        }
        self.report['cases'][self.case]['steps'].append(entry)
        return res, out_raw, err_raw


def markers_of(out_raw, err_raw, tool):
    """从原始字节里提取本轮关心的 CLI 标记（判据用标记，不用日志全文）。"""
    out = out_raw.decode('utf-8', 'replace')
    err = err_raw.decode('utf-8', 'replace')
    check_passed, check_failed = tool.check_markers(out_raw, err_raw)
    return {
        'hit': '（缓存命中）' in out,
        'hitCount': out.count('（缓存命中）'),
        'miss': '缓存未命中' in out,
        'generated': '已生成' in out,
        'generatedCount': sum(1 for line in out.splitlines() if line.startswith('已生成 ')),
        'checkPassed': check_passed,
        'checkFailed': check_failed,
        'checkTargets': tool.check_targets(out_raw),
        'batchSummary': '共 2 个文件生成成功（按路径排序处理）' in out,
        'typeError': 'undefined: noSuchSymbolR13C' in err,
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


def cache_records(project):
    """成功缓存记录：keyHash → 去掉 createdAt 后的记录（时间戳不是语义）。"""
    out = {}
    cache_dir = os.path.join(project, '.goxml', 'cache')
    if not os.path.isdir(cache_dir):
        return out
    for name in sorted(os.listdir(cache_dir)):
        if not name.endswith('.json'):
            continue
        with open(os.path.join(cache_dir, name), encoding='utf-8') as fh:
            rec = json.load(fh)
        rec.pop('createdAt', None)
        out[name[:-len('.json')]] = rec
    return out


# ---------------------------------------------------------------- 结果断言


def batch_expectations(data, out_dir):
    """批次形态的判据集合（用例 1 要求全空；变异要求其中「只启动一次」翻红）。

    `已生成` / `提交前检查通过：N 个目标包` 是**生成子进程**的输出，入口脚本只把它们
    留在证据目录里（不转发到自己的 stdout）——所以判据读原始日志，不读入口摘要。
    """
    fails = []
    gen = [s for s in data['stages'] if s['name'].startswith('generate:')]
    if len(gen) != 1:
        fails.append('只启动一次 generate：阶段数应为 1，实得 %d %r'
                     % (len(gen), [s['name'] for s in gen]))
    # argv 判据按「整批只有一条目录命令」写：拆回逐页时会同时命中条数与形态两条。
    argvs = [[str(a) for a in (s.get('argv') or [])][1:] for s in gen]
    if argvs != [['generate', 'view', '--out-dir', 'view', '--check']]:
        fails.append('generate argv 应恰为 generate view --out-dir view --check，实得 %r'
                     % (argvs,))
    targets = [s.get('checkTargets') for s in gen]
    if targets != [2]:
        fails.append('本次检查应覆盖 2 个目标包，实得每阶段 checkTargets=%r' % (targets,))
    if any(s.get('checkReported') is not True for s in gen):
        fails.append('generate 阶段都应观察到「提交前检查通过」：%r'
                     % [s.get('checkReported') for s in gen])
    raws = []
    for s in gen:
        raw_path = (s.get('stdout') or {}).get('path')
        if raw_path and os.path.isfile(raw_path):
            raws.append(read_bytes(raw_path).decode('utf-8', 'replace'))
        else:
            fails.append('generate 阶段缺少原始 stdout 证据：%r' % (raw_path,))
    raw_text = ''.join(raws)
    lines = raw_text.splitlines()
    generated = [line for line in lines if line.startswith('已生成 ')]
    if generated != ['已生成 ' + EDIT_GEN, '已生成 ' + SUMMARY_GEN]:
        fails.append('generate 原始 stdout 应按路径顺序报两页已生成，实得 %r' % (generated,))
    if '提交前检查通过：2 个目标包' not in raw_text:
        fails.append('generate 原始 stdout 应有「提交前检查通过：2 个目标包」（一次检查覆盖两页）')
    for rel in KEY_ARTIFACTS + [MANIFEST]:
        if not os.path.isfile(os.path.join(out_dir, rel)):
            fails.append('缺少产物 %s' % rel)
    manifest = os.path.join(out_dir, MANIFEST)
    if os.path.isfile(manifest):
        with open(manifest, encoding='utf-8') as fh:
            entries = json.load(fh).get('entries') or []
        outputs = sorted(e.get('output') for e in entries)
        if outputs != sorted(KEY_ARTIFACTS):
            fails.append('Manifest 应登记两页产物，实得 %r' % (outputs,))
    chk = data.get('check') or {}
    if chk.get('forwarded') is not True or chk.get('passed') is not True:
        fails.append('报告 check 应为 forwarded=true/passed=true：%r' % (chk,))
    names = [s['name'] for s in data['stages']]
    if 'build-entry' not in names or 'test' not in names:
        fails.append('批次成功后应继续 build-entry 与 test：%r' % (names,))
    return fails


def offline_expectations(data, label):
    """离屏测试判据：11 个 CROSS-DONE 全在，且 PASS 集合一致。"""
    fails = []
    test = [s for s in data['stages'] if s['name'] == 'test']
    if not test:
        return ['%s 缺少 test 阶段' % label]
    markers = test[0].get('markers') or {}
    expected = 11
    if markers.get('pass') != expected or markers.get('skip') or markers.get('fail'):
        fails.append('%s 离屏测试标记不符：pass=%r skip=%r fail=%r'
                     % (label, markers.get('pass'), markers.get('skip'), markers.get('fail')))
    if len(set(markers.get('completed') or [])) != expected:
        fails.append('%s CROSS-DONE 去重后应 %d 个，实得 %d'
                     % (label, expected, len(set(markers.get('completed') or []))))
    if set(markers.get('completed') or []) != set(markers.get('passedNames') or []):
        fails.append('%s CROSS-DONE 与 PASS 集合不一致' % label)
    return fails


# ---------------------------------------------------------------- 入口调用


def run_entry(ctx, out_dir, repo, *extra, json_report=None):
    argv = [sys.executable, os.path.join(repo, 'scripts', TOOL), PROJECT,
            '--out-dir', out_dir, '--repo', repo, '--lock-path', LOCK]
    argv.extend(extra)
    if json_report:
        argv.extend(['--json', json_report])
    env = dict(os.environ, PYTHONIOENCODING='utf-8')
    proc = subprocess.run(argv, cwd=ctx.dir, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE)
    tag = os.path.basename(out_dir)
    with open(ctx.path(tag + '-stdout.txt'), 'wb') as fh:
        fh.write(proc.stdout)
    with open(ctx.path(tag + '-stderr.txt'), 'wb') as fh:
        fh.write(proc.stderr)
    return {'rc': proc.returncode, 'argv': argv,
            'stdout': proc.stdout.decode('utf-8', 'replace'),
            'stderr': proc.stderr.decode('utf-8', 'replace')}


def load_report(path):
    with open(path, encoding='utf-8') as fh:
        return json.load(fh)


def make_project(ctx, name, repo):
    """用一键入口 --copy-only 造实验工程（材料与模板同源，不走 Go、不取锁）。"""
    proj = ctx.path(name)
    env = dict(os.environ, PYTHONIOENCODING='utf-8')
    proc = subprocess.run(
        [sys.executable, os.path.join(repo, 'scripts', TOOL), PROJECT,
         '--out-dir', proj, '--repo', repo, '--copy-only'],
        cwd=ctx.dir, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    with open(ctx.path(name + '-copy-stdout.txt'), 'wb') as fh:
        fh.write(proc.stdout)
    with open(ctx.path(name + '-copy-stderr.txt'), 'wb') as fh:
        fh.write(proc.stderr)
    if proc.returncode != 0:
        raise AssertionError('--copy-only 失败 rc=%d：%r'
                             % (proc.returncode, proc.stderr.decode('utf-8', 'replace')[-300:]))
    return proj


def with_lock(ctx, task, body):
    """自有工程用例：自持公共锁，finally 按自有 token 释放（不嵌套外层锁）。"""
    lockmod = load_helper(ctx.repo, 'verify-examples-lock.py', 'lock_%s' % uuid.uuid4().hex[:6])
    lock = lockmod.Lock(LOCK)
    try:
        lock.acquire('r13c-e2e', task, 'r13c-%s' % uuid.uuid4().hex[:8])
    except lockmod.LockHeldError as exc:
        return ['SKIP: 公共锁被他人持有（%r），本轮不启动 Go'
                % ((exc.holder or {}).get('agent'),)]
    try:
        return body()
    finally:
        released = lock.release()
        ctx.report['cases'][ctx.case]['lockReleased'] = released
        if not released:
            ctx.fail('公共锁释放失败：%s' % lock.release_error)


# ---------------------------------------------------------------- 用例 1：一键入口批次


def case_entry_batch_success(ctx, repo=None):
    """一次目录 generate 覆盖两页：argv、检查覆盖面、产物、入口构建、离屏。"""
    repo = repo or ctx.repo
    out = ctx.path('批次工程')
    rep = ctx.path('entry-report.json')
    res = run_entry(ctx, out, repo, '--check', json_report=rep)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，一键入口未启动（rc=8）']
    fails = []
    if res['rc'] != 0:
        fails.append('批次入口应全过，实得 rc=%d（stderr=%r）' % (res['rc'], res['stderr'][-400:]))
        return fails
    data = load_report(rep)
    fails.extend(batch_expectations(data, out))
    fails.extend(offline_expectations(data, '批次工程'))
    if 'VERIFY-PROJECTS-OK' not in res['stdout']:
        fails.append('应打印成功完成标记')
    # 原始日志所有权：一次启动 = 一份 stdout/stderr；批次阶段只能有一个证据目录。
    gen = [s for s in data['stages'] if s['name'].startswith('generate:')]
    if len(gen) == 1:
        raw = (gen[0].get('stdout') or {}).get('path')
        if not raw or not os.path.isfile(raw) or os.path.getsize(raw) == 0:
            fails.append('generate 阶段应留下非空原始 stdout：%r' % (raw,))
        else:
            case_dir = gen[0].get('caseDir') or ''
            ctx.report['cases'][ctx.case]['generateCaseDir'] = case_dir
            parts = [p for p in case_dir.replace('\\', '/').split('/') if p]
            if gen[0]['name'].replace(':', '-') not in parts:
                fails.append('generate 的原始日志目录与阶段名不一致：%r vs %r'
                             % (case_dir, gen[0]['name']))
    lock = data.get('lock') or {}
    if lock.get('released') is not True:
        fails.append('批次成功也要按 token 释放锁：%r' % (lock.get('released'),))
    return fails


# ---------------------------------------------------------------- 用例 2：自有工程循环


def case_own_project_cycle(ctx):
    """B1 同批 → B2 真命中且检查仍执行 → B3 一包失败两页保旧 → B4 修好恢复。"""

    def body():
        cli = ctx.path('goxml.exe')
        res, _, err = ctx.step('b0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], ctx.repo)
        if not ctx.check(res['ok'] and res['rc'] == 0,
                         'CLI 构建失败 rc=%s：%s' % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
            return ctx.report['cases'][ctx.case]['failures']
        proj = make_project(ctx, '实验工程', ctx.repo)

        # B1 两页同批生成（带 --check）
        res, out, err = ctx.step('b1-batch-generate', [cli, 'generate', 'view', '--out-dir', 'view', '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 0, 'B1 批次生成应成功，rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        ctx.check(m['miss'], 'B1 首次应两页都未命中，实得 %r' % m)
        ctx.check(m['generatedCount'] == 2, 'B1 应报两页已生成，实得 %d' % m['generatedCount'])
        ctx.check(m['checkTargets'] == 2, 'B1 检查应覆盖 2 个目标包，实得 %r' % m['checkTargets'])
        snap1 = snapshot(proj)
        ctx.check(all(k in snap1 for k in KEY_ARTIFACTS + [MANIFEST]),
                  'B1 后应有两页产物与 Manifest：%r' % sorted(snap1))
        cache1 = sorted(k for k in snap1 if k.startswith('.goxml/cache/'))
        ctx.check(len(cache1) == 2, 'B1 后成功缓存应有 2 条，实得 %r' % cache1)

        # B2 下一次：真命中 + 检查仍执行 + 零重写
        res, out, err = ctx.step('b2-batch-hit', [cli, 'generate', 'view', '--out-dir', 'view', '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 0, 'B2 全命中应成功，rc=%s' % res['rc'])
        ctx.check(m['hitCount'] == 2, 'B2 两页都应命中，实得 %d 个命中标记' % m['hitCount'])
        ctx.check(not m['generated'], 'B2 全命中不应重写产物')
        ctx.check(m['checkTargets'] == 2, 'B2 全命中也必须执行检查且覆盖 2 个目标包，实得 %r'
                  % m['checkTargets'])
        problems = diff_snapshot(snap1, snapshot(proj))
        ctx.check(not problems, 'B2 全命中不得改动工程任何文件：%r' % problems)

        # B3 一个视图所在包编译失败：两页旧产物 / Manifest / 缓存全保留
        doc = os.path.join(proj, 'view', 'edit', 'doc.go')
        doc_orig = read_bytes(doc)
        with open(doc, 'wb') as fh:
            fh.write(doc_orig + TYPE_ERROR_LINE.encode('utf-8'))
        snap_before = snapshot(proj)
        res, out, err = ctx.step('b3-batch-check-fails',
                                 [cli, 'generate', 'view', '--out-dir', 'view', '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 2, 'B3 检查失败应以 2 退出（实得 %s）' % res['rc'])
        ctx.check(m['checkFailed'], 'B3 stderr 应含「提交前检查失败」：%r'
                  % err.decode('utf-8', 'replace')[-300:])
        ctx.check(m['typeError'], 'B3 失败原因应是目标包类型错误 undefined: noSuchSymbolR13C')
        ctx.check(not m['generated'], 'B3 检查失败不得声称已生成')
        problems = diff_snapshot(snap_before, snapshot(proj))
        # 唯一的允许差异是我们自己注入的 doc.go。
        problems = [p for p in problems if 'doc.go' not in p]
        ctx.check(not problems, 'B3 失败后两页旧产物/Manifest/缓存必须逐字保留：%r' % problems)
        snap3 = snapshot(proj)
        ctx.check([snap3[k] for k in KEY_ARTIFACTS] == [snap1[k] for k in KEY_ARTIFACTS],
                  'B3 两页产物字节与 mtime 都不应变')
        ctx.check(sorted(k for k in snap3 if k.startswith('.goxml/cache/')) == cache1,
                  'B3 不得新增/删除成功缓存')

        # B4 修好：命中 + 检查通过 + 入口构建恢复
        with open(doc, 'wb') as fh:
            fh.write(doc_orig)
        res, out, err = ctx.step('b4-batch-fixed',
                                 [cli, 'generate', 'view', '--out-dir', 'view', '--check'], proj)
        m = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 0, 'B4 修好后应恢复通过，rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-200:]))
        ctx.check(m['hitCount'] == 2, 'B4 修好后回到原键应两页命中，实得 %r' % m)
        ctx.check(m['checkTargets'] == 2, 'B4 修好后检查应再次覆盖 2 个目标包，实得 %r'
                  % m['checkTargets'])
        entry_exe = ctx.path('preferences.exe')
        res, _, err = ctx.step('b4-entry-build', [GO, 'build', '-o', entry_exe, './cmd/preferences'], proj)
        ctx.check(res['rc'] == 0, 'B4 入口构建应恢复通过，rc=%s' % res['rc'])
        return ctx.report['cases'][ctx.case]['failures']

    return with_lock(ctx, 'own-project-cycle', body)


# ---------------------------------------------------------------- 用例 3：拆回逐页的后果


def case_per_view_half_batch(ctx):
    """同一材料拆回逐页调用：第一页落盘、第二页失败 → 混合状态（对照证据）。"""

    def body():
        cli = ctx.path('goxml.exe')
        res, _, err = ctx.step('h0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], ctx.repo)
        if not ctx.check(res['ok'] and res['rc'] == 0,
                         'CLI 构建失败 rc=%s：%s'
                         % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
            return ctx.report['cases'][ctx.case]['failures']
        proj = make_project(ctx, '半批工程', ctx.repo)
        # 先建出稳定的成功基线（两页都有产物与缓存）
        res, _, err = ctx.step('h1-baseline', [cli, 'generate', 'view', '--out-dir', 'view'], proj)
        if not ctx.check(res['rc'] == 0, 'H1 基线生成失败 rc=%s：%s'
                         % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
            return ctx.report['cases'][ctx.case]['failures']
        snap_h1 = snapshot(proj)
        with open(os.path.join(proj, MANIFEST), encoding='utf-8') as fh:
            ent1 = {e.get('output'): e.get('inputHash')
                    for e in (json.load(fh).get('entries') or [])}

        # 让两页都变成 miss：改 edit 页的文本、在 summary 包里注入类型错误
        gxml = os.path.join(proj, 'view', 'edit', 'edit.gxml')
        gxml_orig = read_bytes(gxml)
        old_text = 'Value="偏好设置 · 编辑"'
        new_text = 'Value="偏好设置 · 编辑 R13C"'
        gxml_text = gxml_orig.decode('utf-8')
        if gxml_text.count(old_text) != 1:
            raise AssertionError('edit.gxml 里标题锚点命中 %d 次（应为 1）'
                                 % gxml_text.count(old_text))
        with open(gxml, 'wb') as fh:
            fh.write(gxml_text.replace(old_text, new_text).encode('utf-8'))
        sdoc = os.path.join(proj, 'view', 'summary', 'doc.go')
        sdoc_orig = read_bytes(sdoc)
        with open(sdoc, 'wb') as fh:
            fh.write(sdoc_orig + TYPE_ERROR_LINE.encode('utf-8'))

        # 逐页：第一页成功（落盘），第二页检查失败
        res, out, err = ctx.step('h2-perview-edit',
                                 [cli, 'generate', 'view/edit/edit.gxml', '--out-dir', 'view/edit', '--check'], proj)
        m1 = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 0, 'H2 第一页应成功（这正是问题所在），rc=%s' % res['rc'])
        ctx.check(m1['generated'], 'H2 第一页应已生成')
        res, out, err = ctx.step('h3-perview-summary',
                                 [cli, 'generate', 'view/summary/summary.gxml', '--out-dir', 'view/summary', '--check'], proj)
        m2 = ctx.report['cases'][ctx.case]['steps'][-1]['markers']
        ctx.check(res['rc'] == 2 and m2['checkFailed'],
                  'H3 第二页应检查失败（rc=%s）' % res['rc'])

        snap_h3 = snapshot(proj)
        edit_changed = snap_h3[EDIT_GEN]['sha256'] != snap_h1[EDIT_GEN]['sha256']
        summary_same = snap_h3[SUMMARY_GEN] == snap_h1[SUMMARY_GEN]
        ctx.check(edit_changed, 'H4 逐页调用应已把第一页新产物落盘（混合状态）')
        ctx.check(summary_same, 'H4 第二页失败应保持旧产物（混合状态的另一半）')
        with open(os.path.join(proj, MANIFEST), encoding='utf-8') as fh:
            entries = {e.get('output'): e for e in (json.load(fh).get('entries') or [])}
        ctx.check(entries.get(EDIT_GEN, {}).get('inputHash') != ent1.get(EDIT_GEN),
                  'H4 Manifest 第一页应是新登记（inputHash 变了）：%r' % (entries.get(EDIT_GEN),))
        ctx.check(entries.get(SUMMARY_GEN, {}).get('inputHash') == ent1.get(SUMMARY_GEN),
                  'H4 Manifest 第二页应保持旧登记：%r' % (entries.get(SUMMARY_GEN),))
        ctx.check(len(snap_h3) > len(snap_h1),
                  'H4 逐页调用会为新键新增缓存记录（混合状态的佐证）：%d → %d'
                  % (len(snap_h1), len(snap_h3)))
        ctx.check(True, '对照：同一输入在批次模式下（用例 2 的 B3）不会出现这种混合状态')
        # 恢复源，避免污染后续用例的模板指纹判断
        with open(gxml, 'wb') as fh:
            fh.write(gxml_orig)
        with open(sdoc, 'wb') as fh:
            fh.write(sdoc_orig)
        return ctx.report['cases'][ctx.case]['failures']

    return with_lock(ctx, 'per-view-half-batch', body)


# ---------------------------------------------------------------- 用例 4：与逐 View 等价


def case_equivalence_per_view(ctx):
    """同一模板：R12 逐 View argv 与 R13 批次 argv 的产物/缓存/离屏行为必须一致。"""

    def body():
        cli = ctx.path('goxml.exe')
        res, _, err = ctx.step('e0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], ctx.repo)
        if not ctx.check(res['ok'] and res['rc'] == 0, 'CLI 构建失败 rc=%s' % res['rc']):
            return ctx.report['cases'][ctx.case]['failures']
        per_view = make_project(ctx, '逐View工程', ctx.repo)
        batch = make_project(ctx, '批次工程', ctx.repo)

        # R12 形态：两页各一条命令（argv 与 R12-C 一键入口完全一致）
        for rel in ('view/edit/edit.gxml', 'view/summary/summary.gxml'):
            d = os.path.dirname(rel)
            res, _, err = ctx.step('e1-perview-' + rel.replace('/', '-'),
                                   [cli, 'generate', rel, '--out-dir', d, '--check'], per_view)
            ctx.check(res['rc'] == 0, 'E1 %s 逐 View 生成应成功 rc=%s：%s'
                      % (rel, res['rc'], err.decode('utf-8', 'replace')[-200:]))
        # R13 形态：一次目录 generate
        res, _, err = ctx.step('e2-batch', [cli, 'generate', 'view', '--out-dir', 'view', '--check'], batch)
        ctx.check(res['rc'] == 0, 'E2 批次生成应成功 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-200:]))

        for rel in KEY_ARTIFACTS + [MANIFEST]:
            a, b = sha256_file(os.path.join(per_view, rel)), sha256_file(os.path.join(batch, rel))
            ctx.check(a == b, 'E3 %s 应与逐 View 生成逐字相同（%s vs %s）' % (rel, a[:12], b[:12]))
            ctx.report['cases'][ctx.case].setdefault('equivalence', {})[rel] = {
                'perView': a, 'batch': b}
        rec_a, rec_b = cache_records(per_view), cache_records(batch)
        ctx.check(sorted(rec_a) == sorted(rec_b),
                  'E4 成功缓存键集合应一致：%r vs %r' % (sorted(rec_a), sorted(rec_b)))
        ctx.check(rec_a == rec_b, 'E4 缓存记录内容（去掉时间戳）应一致：%r' % (rec_a,))
        ctx.report['cases'][ctx.case]['cacheKeys'] = sorted(rec_a)

        # 离屏行为：两边各跑一遍模板自带测试（共享同一个 ViewModel）
        for label, proj in (('per-view', per_view), ('batch', batch)):
            res, out, err = ctx.step('e5-test-' + label,
                                     [GO, 'test', '-count=1', '-v', '-timeout', '20m',
                                      './view/...', '-run', '^TestCross'], proj)
            markers = ctx.tool.test_markers(out)
            ctx.report['cases'][ctx.case].setdefault('offscreen', {})[label] = {
                'rc': res['rc'], 'pass': markers['pass'], 'skip': markers['skip'],
                'fail': markers['fail'], 'completed': len(set(markers['completed']))}
            ctx.check(res['rc'] == 0, '%s 离屏测试应通过 rc=%s：%s'
                      % (label, res['rc'], err.decode('utf-8', 'replace')[-300:]))
            problem = ctx.tool.test_verdict(markers)
            ctx.check(problem is None, '%s 离屏判据不成立：%s' % (label, problem))
        return ctx.report['cases'][ctx.case]['failures']

    return with_lock(ctx, 'equivalence-per-view', body)


# ---------------------------------------------------------------- 变异：拆回逐页


def case_mutation_per_view_loop(ctx):
    """拆回逐页调用后，「只启动一次 generate」等批次判据必须翻红。"""
    shadow = build_shadow(ctx.repo, ctx.path('shadow-per-view-loop'), 'per-view-loop')
    out = ctx.path('变异工程')
    rep = ctx.path('mutation-report.json')
    res = run_entry(ctx, out, shadow, '--check', json_report=rep)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，一键入口未启动（rc=8）']
    fails = []
    if res['rc'] != 0:
        fails.append('变异只改生成调用形态，流程本身仍应成功（便于看判据翻红），实得 rc=%d：%r'
                     % (res['rc'], res['stderr'][-300:]))
        return fails
    data = load_report(rep)
    red = batch_expectations(data, out)
    ctx.report['cases'][ctx.case]['mutationRed'] = red
    if not red:
        fails.append('变异后批次判据竟然全绿——说明这些断言没有牙')
        return fails
    if not any('只启动一次' in item for item in red):
        fails.append('变异后应出现「只启动一次 generate」翻红，实得 %r' % (red,))
    names = [s['name'] for s in data['stages'] if s['name'].startswith('generate:')]
    if len(names) != 2:
        fails.append('变异后应是两条逐页命令，实得 %r' % (names,))
    ctx.check('generate:view' not in [s['name'] for s in data['stages']],
              '变异后不应再有 generate:view 批次阶段')
    return fails


CASES = [
    ('entry-batch-success', case_entry_batch_success),
    ('own-project-cycle', case_own_project_cycle),
    ('per-view-half-batch', case_per_view_half_batch),
    ('equivalence-per-view', case_equivalence_per_view),
    ('mutation-per-view-loop', case_mutation_per_view_loop),
]


def main(argv):
    parser = argparse.ArgumentParser(prog=THIS, description=__doc__.split('\n')[0])
    parser.add_argument('--work-root', default=None, help='用例工作根目录（跑用例时必填）')
    parser.add_argument('--report', default=None, help='汇总报告 JSON 路径')
    parser.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    parser.add_argument('--case', action='append', default=None, help='只跑指定用例（可多次）')
    parser.add_argument('--make-mutated-repo', default=None, metavar='DEST',
                        help='在 DEST 生成变异后的影子仓库并退出（不跑用例）')
    parser.add_argument('--mutate', default=None,
                        help='变异名：%s' % '|'.join(sorted(MUTATIONS)))
    args = parser.parse_args(argv)

    repo = os.path.abspath(args.repo) if args.repo else os.path.dirname(
        os.path.dirname(os.path.abspath(__file__)))
    if not os.path.isfile(os.path.join(repo, 'scripts', TOOL)):
        sys.stderr.write('仓库缺少 %s：%s\n' % (TOOL, repo))
        return 2

    if args.make_mutated_repo:
        if not args.mutate or args.mutate not in MUTATIONS:
            sys.stderr.write('--make-mutated-repo 需要 --mutate <%s>\n'
                             % '|'.join(sorted(MUTATIONS)))
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

    report = {'schema': 'r13c-crosspackage-batch-e2e-1', 'repo': repo,
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
            problems = ['用例异常：%s: %s' % (type(exc).__name__, exc)]
        entry = report['cases'].setdefault(name, {'failures': []})
        problems = list(dict.fromkeys(entry['failures'] + problems))
        entry['failures'] = problems
        if problems and all(str(p).startswith('SKIP') for p in problems):
            skipped += 1
            report['cases'][name]['status'] = 'SKIP'
        elif problems:
            failures += 1
            report['cases'][name]['status'] = 'FAIL'
        else:
            report['cases'][name]['status'] = 'PASS'
    report['endedAt'] = now()
    report['failures'] = failures
    report['skipped'] = skipped
    report['status'] = 'FAIL' if failures else ('INCOMPLETE' if skipped else 'PASS')
    text = json.dumps(report, ensure_ascii=False, indent=1, sort_keys=True)
    if args.report:
        try:
            tool.write_json(args.report, report)
        except (OSError, tool.Refused) as exc:
            parser.error('无法写入汇总报告：%s' % exc)
    print(text)
    print('CROSS-BATCH-E2E-%s cases=%d fail=%d skip=%d'
          % (report['status'], len(report['cases']), failures, skipped))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""routed-gallery 的**随仓**验收入口：一条命令跑完主验收 + 缓存/资产/变异矩阵（R22-C）。

`scripts/verify-projects.py` 证明的是「复制 → 一次目录生成 → 构建 → 离屏测试」能过。
本脚本补它之后那一段，回答四个更难的问题：

  1. 生成之后的**再生成**是不是真命中（字节、mtime、Manifest 三者都不动）；
  2. 换掉一个**合法**资产会不会只重验受影响的那一页；
  3. 资产损坏 / 删除时是否拒绝并**保留**旧产物；
  4. 两条退化变异是否**可编译**、且让对应断言真的翻红（不是编译失败冒充红）。

同一份判据还负责把「资产内容变化」与「构建上下文变化」分开：两者都报
`package-sources-changed`，但前者只让一页未命中、后者让两页同时未命中——
只有**未命中的范围**能区分它们。所以矩阵里既有资产换图的用例，也有一条
只改构建上下文（`GOFLAGS`）的对照。

用法（一条命令）::

    python scripts/verify-gallery-accept.py \
        --run-dir "D:/work/r22 图库 验收" \
        --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"

`--run-dir` 必须是**本轮独占创建**的新目录（已存在就拒绝，且零新增）。布局：

    <run-dir>/project/     verify-projects 产出的真实工程（含 .verify-projects 证据）
    <run-dir>/main.json    主验收报告（verify-projects 原样落盘的那一份）
    <run-dir>/raw/          矩阵每条命令的 stdout/stderr 原始字节 + result.json
    <run-dir>/work/         变异副本（M1 / M2）
    <run-dir>/report.json   本脚本的报告（判据、命令台账、快照、结论）

锁：**同一时刻只有一层持锁**，三段串行，没有嵌套——

  (a) 预检：取锁后立刻按自有 token 释放。只为在锁忙时**不创建任何目录**；
      它不承担排他，真正的排他由 (b)(c) 各自完成。
  (b) 主验收：锁由 `verify-projects.py` 子进程持有，本进程不持。
  (c) 矩阵：本进程自己取锁、自己按 token 释放。
每段的持锁者都在自己的 `finally` 里释放；报告只留 token 的 sha256 指纹。

为什么每条命令都必须带固定环境：**`GOFLAGS` 的字面值是缓存键的一项**。
用裸 shell 跑 generate 会得到一次 `package-sources-changed` 的假未命中——
那测的是环境差异，不是缓存。矩阵所有命令都走 `GO_ENV_FIXED`；
唯一的例外是 S7，它**故意**改这一项，用来证明「上下文变化」长什么样。

退出码：0 全部成立 · 1 有判据不成立 · 2 用法错误 · 3 拒绝（目录/报告已存在、模板缺失）
        · 8 锁忙（未启动任何 Go 进程）· 9 锁未按自有 token 释放
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
import os
import re
import shutil
import sys

SCHEMA = 'r22c-gallery-accept-1'
PROJECT = 'routed-gallery'

EXIT_OK = 0
EXIT_FAILED = 1
EXIT_USAGE = 2
EXIT_REFUSED = 3
EXIT_LOCK_BUSY = 8
EXIT_LOCK_NOT_RELEASED = 9

# 生成命令：**一次目录生成**覆盖两页（发现根=输出根=工程根下的 view）。
GEN_ARGS = ['generate', 'view', '--out-dir', 'view', '--check']

HOME_GEN = 'view/home/home_gen.go'
DETAIL_GEN = 'view/detail/detail_gen.go'
HOME_ASSET = 'view/home/assets/badge.png'
DETAIL_ASSET = 'view/detail/assets/cover.png'
MANIFEST = '.goxml/generate-manifest.json'

# 「失败保旧」的判据是**字节 + mtime 都没动**，两者缺一不可：
# 只看字节会把「重写了一遍同样内容」误判成没动（R21-C 的踩坑）。
PRESERVE = (HOME_GEN, DETAIL_GEN, MANIFEST)

# S7 只改构建上下文；这两份文件若被 `-mod` 默认值变化波及，快照能抓到。
CONTEXT_FILES = ('go.mod', 'go.sum')

HIT_ALL = '命中 2，生成 0'
MISS = '缓存未命中'
HIT_LINE = '（缓存命中）'
CHECK_OK_2 = '提交前检查通过：2 个目标包'
ASSET_DIAG = 'CODEGEN-ASSET-002'
BATCH_ABORT = '批次全部放弃'
CONTEXT_REASON = 'package-sources-changed'


class Refused(Exception):
    """带退出码的拒绝：调用方不再继续任何有副作用的动作。"""

    def __init__(self, code, message, **detail):
        Exception.__init__(self, message)
        self.code = code
        self.message = message
        self.detail = detail


# ---------------------------------------------------------------- 小工具


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds')


def sha256_bytes(data):
    return hashlib.sha256(data).hexdigest()


def read_bytes(path):
    with open(path, 'rb') as fh:
        return fh.read()


def load_helper(repo, filename, module_name):
    """按路径加载同目录助手（脚本名带连字符，不能直接 import）。"""
    path = os.path.join(repo, 'scripts', filename)
    spec = importlib.util.spec_from_file_location(module_name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def default_repo():
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def snapshot(root, rels):
    """字节 + sha256 + mtimeNs 三件套。mtime 单独记，是因为「重写同样字节」也该被抓到。"""
    out = {}
    for rel in rels:
        path = os.path.join(root, rel.replace('/', os.sep))
        if not os.path.isfile(path):
            out[rel] = None
            continue
        st = os.stat(path)
        data = read_bytes(path)
        out[rel] = {'bytes': len(data), 'sha256': sha256_bytes(data), 'mtimeNs': st.st_mtime_ns}
    return out


def changed(before, after):
    """返回前后不一致的键（不含顺序，只比内容）。"""
    keys = sorted(set(before) | set(after))
    return [k for k in keys if before.get(k) != after.get(k)]


# ---------------------------------------------------------------- 测试台账判据


RUN_RE = re.compile(r'^=== RUN\s+(Test\w+)$')
PASS_RE = re.compile(r'^--- PASS: (Test\w+)\b')
SKIP_RE = re.compile(r'^--- SKIP: (Test\w+)\b')
FAIL_RE = re.compile(r'^--- FAIL: (Test\w+)\b')
NOFILES_RE = re.compile(r'^\?\s+\S+\s+\[no test files\]$')


def audit_test_run(raw, spec):
    """独立重数 `go test -v` 的台账，不信任上层摘要。

    判据（缺一条都不成立）：
      * 零 SKIP（冻结行为禁用 `t.Skip` 兜底）、零 FAIL、零 `no test files`；
      * `=== RUN` 集合 == PASS 集合 == 完成标记集合 == 规格里的必要用例集合；
      * 数量也相等（`-count=1` 下同名重复出现本身就是异常）。

    返回 {'problems': [...], 以及各集合}，problems 为空才成立。
    """
    text = raw.decode('utf-8', 'replace')
    expected = list(spec['expectedTests'])
    marker = spec['completionMarker']
    done_re = re.compile(r': ' + re.escape(marker) + r' (Test\w+)$')

    run, passed, skipped, failed, done, nofiles = [], [], [], [], [], []
    for line in text.splitlines():
        m = RUN_RE.match(line)
        if m:
            run.append(m.group(1))
            continue
        m = PASS_RE.match(line)
        if m:
            passed.append(m.group(1))
            continue
        m = SKIP_RE.match(line)
        if m:
            skipped.append(m.group(1))
            continue
        m = FAIL_RE.match(line)
        if m:
            failed.append(m.group(1))
            continue
        if NOFILES_RE.match(line):
            nofiles.append(line.strip())
            continue
        m = done_re.search(line)
        if m:
            done.append(m.group(1))

    problems = []
    if skipped:
        problems.append('有 %d 个用例被 SKIP（冻结行为禁用 t.Skip 兜底）：%s'
                        % (len(skipped), ', '.join(sorted(skipped))))
    if failed:
        problems.append('有 %d 个用例 FAIL：%s' % (len(failed), ', '.join(sorted(failed))))
    if nofiles:
        problems.append('有包没有任何测试文件：%s' % '; '.join(nofiles))
    missing = [n for n in expected if n not in run]
    if missing:
        problems.append('缺少必要用例（没跑）：%s' % ', '.join(missing))
    extra = [n for n in run if n not in expected]
    if extra:
        problems.append('跑了规格外的用例：%s' % ', '.join(extra))
    if len(run) != len(set(run)):
        problems.append('有用例被重复执行（-count=1 下不应出现）')
    if set(passed) != set(expected):
        problems.append('PASS 集合与必要用例集合不一致：多 %s / 少 %s'
                        % (sorted(set(passed) - set(expected)) or '无',
                           sorted(set(expected) - set(passed)) or '无'))
    if len(passed) != len(expected) or len(done) != len(expected):
        problems.append('PASS/完成标记数量与必要用例数量不一致')
    if set(done) != set(expected):
        problems.append('%s 完成标记集合与必要用例集合不一致：多 %s / 少 %s'
                        % (marker, sorted(set(done) - set(expected)) or '无',
                           sorted(set(expected) - set(done)) or '无'))
    return {'expected': expected, 'run': run, 'passed': passed, 'skipped': skipped,
            'failed': failed, 'completed': done, 'noTestFiles': nofiles,
            'problems': problems}


# ---------------------------------------------------------------- 上下文


class Ctx(object):
    """一条命令 + 一条判据的收口点：命令一律经 `verify_examples_proc.run_capture`。"""

    def __init__(self, proj, cli, raw_dir, work_dir, go, timeout, spec, report, procmod):
        self.proj = proj
        self.cli = cli
        self.raw_dir = raw_dir
        self.work_dir = work_dir
        self.go = go
        self.timeout = timeout
        self.spec = spec
        self.report = report
        self.proc = procmod
        self.checks = report['checks']
        self.commands = report['commands']

    def run(self, case_id, argv, cwd, env_overrides=None, timeout=None):
        env = self.proc.GO_ENV_FIXED if env_overrides is None else env_overrides
        rec = self.proc.run_capture(
            argv, case_id, self.raw_dir, cwd=cwd,
            timeout=timeout or self.timeout, env_overrides=env, containment='auto')
        self.commands.append({
            'caseId': rec['caseId'],
            'argv': rec['argv'],
            'cwd': rec['cwd'],
            'rc': rec['rc'],
            'rcSource': rec['rcSource'],
            'timedOut': rec['timedOut'],
            'contained': rec['contained'],
            'ok': rec['ok'],
            'okReasons': rec['okReasons'],
            'elapsedSec': rec['elapsedSec'],
            'caseDir': rec['caseDir'],
            'stdout': rec['stdout'],
            'stderr': rec['stderr'],
        })
        # 基础设施不成立时后面的业务判据没有意义：先红这一条，再让业务判据各说各话。
        self.check('%s.infra' % case_id, '子进程真的启动、退出码可观测、进程树已收口',
                   bool(rec['ok']), 'okReasons=%s' % (rec['okReasons'],))
        return rec

    def check(self, check_id, name, ok, detail=''):
        self.checks.append({'id': check_id, 'name': name, 'ok': bool(ok), 'detail': detail})
        return bool(ok)

    def text(self, rec, stream='stdout'):
        info = rec.get(stream) or {}
        path = info.get('path')
        if not path or not os.path.isfile(path):
            return ''
        return read_bytes(path).decode('utf-8', 'replace')

    def gen_argv(self):
        return [self.cli] + GEN_ARGS


# ---------------------------------------------------------------- 主验收（委派）


def run_main_stage(repo, run_dir, project_dir, main_json, run_id, args):
    procmod = load_helper(repo, 'verify_examples_proc.py', 'r22c_verify_examples_proc')
    payload = [sys.executable, os.path.join(repo, 'scripts', 'verify-projects.py'), PROJECT,
               '--out-dir', project_dir,
               '--check',
               '--lock-path', os.path.abspath(args.lock_path),
               '--lock-agent', 'workbuddy',
               '--lock-task', 'gallery-accept-main',
               '--go', args.go,
               '--json', main_json,
               '--run-id', 'vp-' + run_id,
               '--quiet']
    rec = procmod.run_capture(payload, 'main-verify-projects', os.path.join(run_dir, 'raw'),
                              cwd=run_dir, timeout=args.main_timeout,
                              env_overrides=None, containment='auto')
    return rec


def audit_main(main, ctx, project_dir, spec):
    """核对主验收报告：只在**它自己的判据**成立时才把结果当证据用。"""
    checks = ctx.checks

    def add(check_id, name, ok, detail=''):
        checks.append({'id': check_id, 'name': name, 'ok': bool(ok), 'detail': detail})

    add('main.verdict', 'verify-projects 判定通过（verdict.ok / code==0）',
        main.get('verdict', {}).get('ok') is True and main.get('verdict', {}).get('code') == 0,
        'verdict=%s' % (main.get('verdict'),))
    add('main.project', '本轮跑的是 routed-gallery，输出目录是本轮独占创建的',
        main.get('project') == PROJECT and os.path.normcase(main.get('outDir') or '')
        == os.path.normcase(project_dir),
        'project=%s outDir=%s' % (main.get('project'), main.get('outDir')))
    add('main.template', '模板源码在整轮里字节未变（工具不写模板）',
        main.get('templateUntouched') is True, 'templateUntouched=%s'
        % (main.get('templateUntouched'),))

    stages = main.get('stages') or []
    gen = [s for s in stages if str(s.get('name', '')).startswith('generate:')]
    add('main.generate.single', '只有**一次**目录级 generate（不是逐页各跑一次）',
        len(gen) == 1, 'generate 阶段数=%d' % len(gen))
    if gen:
        argv = gen[0].get('argv') or []
        add('main.generate.argv', 'argv 是 `generate view --out-dir view --check`',
            argv[1:] == GEN_ARGS, 'argv[1:]=%s' % (argv[1:],))
        add('main.generate.cwd', 'generate 的工作目录是工程根（相对路径 view 才指对地方）',
            os.path.normcase(gen[0].get('cwd') or '') == os.path.normcase(project_dir),
            'cwd=%s' % (gen[0].get('cwd'),))
        add('main.generate.checkTargets', '提交前检查覆盖 2 个目标包（一批两页是真的）',
            gen[0].get('checkTargets') == 2, 'checkTargets=%s' % (gen[0].get('checkTargets'),))
        add('main.generate.rc', 'generate 退出码 0', gen[0].get('rc') == 0,
            'rc=%s' % (gen[0].get('rc'),))
    add('main.check.reported', '--check 的「提交前检查通过」标记被观察到（不只是参数转发）',
        (main.get('check') or {}).get('passed') is True, 'check=%s' % (main.get('check'),))

    entry_stage = [s for s in stages if s.get('name') == 'build-entry']
    add('main.entry.built', '窗口入口只构建（rc==0）',
        bool(entry_stage) and entry_stage[0].get('rc') == 0,
        'rc=%s' % (entry_stage[0].get('rc') if entry_stage else None,))
    entry = spec['entry']
    exe = os.path.basename(entry) + ('.exe' if os.name == 'nt' else '')
    executed = [s.get('name') for s in stages
                if s.get('argv') and os.path.basename(str(s['argv'][0])).lower() == exe.lower()]
    add('main.entry.never-run', '入口二进制从未作为 argv[0] 被执行（不会开原生窗口）',
        executed == [], '把它当命令跑过的阶段：%s' % (executed,))

    test_stage = [s for s in stages if s.get('name') == 'test']
    add('main.test.stage', '离屏测试阶段存在且 rc==0',
        bool(test_stage) and test_stage[0].get('rc') == 0,
        'rc=%s' % (test_stage[0].get('rc') if test_stage else None,))
    if test_stage:
        raw = b''
        info = test_stage[0].get('stdout') or {}
        if info.get('path') and os.path.isfile(info['path']):
            raw = read_bytes(info['path'])
        audit = audit_test_run(raw, spec)
        ctx.report['mainTest'] = {k: audit[k] for k in
                                  ('run', 'passed', 'skipped', 'failed', 'completed',
                                   'noTestFiles')}
        add('main.test.ledger', '独立重数台账：通过集 == %s 完成集 == 必要用例集，零 SKIP/FAIL/拒绝'
            % spec['completionMarker'], not audit['problems'],
            ' | '.join(audit['problems']) or '用例 %d 个，全部 PASS' % len(audit['passed']))
        add('main.test.count', '用例数 == 规格声明的 %d 个' % len(spec['expectedTests']),
            len(audit['passed']) == len(spec['expectedTests'])
            and len(audit['run']) == len(spec['expectedTests']),
            'run=%d pass=%d expected=%d' % (len(audit['run']), len(audit['passed']),
                                            len(spec['expectedTests'])))


# ---------------------------------------------------------------- 矩阵：缓存


def case_rerun_hit(ctx, case_id, label):
    proj = ctx.proj
    before = snapshot(proj, PRESERVE)
    rec = ctx.run(case_id, ctx.gen_argv(), proj)
    after = snapshot(proj, PRESERVE)
    text = ctx.text(rec)
    ctx.check(case_id + '.rc', '%s：rc==0' % label, rec['rc'] == 0, 'rc=%s' % (rec['rc'],))
    ctx.check(case_id + '.summary', '%s：输出「%s」' % (label, HIT_ALL), HIT_ALL in text,
              text.strip().replace('\n', ' / ')[:300])
    ctx.check(case_id + '.nomiss', '%s：没有任何「%s」行' % (label, MISS), MISS not in text, '')
    ctx.check(case_id + '.twohits', '%s：两页都报缓存命中' % label,
              text.count(HIT_LINE) == 2, '命中行数=%d' % text.count(HIT_LINE))
    ctx.check(case_id + '.frozen',
              '%s：字节、mtime、Manifest 三者全未动（真命中不是「重写同样字节」）' % label,
              before == after, '变了：%s' % (changed(before, after),))
    ctx.check(case_id + '.check', '%s：提交前检查仍覆盖 2 个目标包（命中不等于跳过检查）' % label,
              CHECK_OK_2 in text, '')
    ctx.report.setdefault('snapshots', {})[case_id] = {'before': before, 'after': after}
    return rec


def case_asset_replaced(ctx):
    """合法换图：只有首页重验；详情页连 mtime 都不能动。"""
    proj = ctx.proj
    home = os.path.join(proj, HOME_ASSET.replace('/', os.sep))
    detail = os.path.join(proj, DETAIL_ASSET.replace('/', os.sep))
    original = read_bytes(home)
    # 另一页的版本化 PNG 就是一张**合法的、不同的**图片：不引入生成器、不新增二进制。
    alternate = read_bytes(detail)
    ctx.check('s3.distinct', '两张模板资产字节不同（换图确实换了内容）', original != alternate,
              'home=%dB detail=%dB' % (len(original), len(alternate)))
    before = snapshot(proj, PRESERVE)
    with open(home, 'wb') as fh:
        fh.write(alternate)
    rec = ctx.run('s3-asset-replaced', ctx.gen_argv(), proj)
    after = snapshot(proj, PRESERVE)
    text = ctx.text(rec)
    ctx.check('s3.rc', '合法换图：rc==0', rec['rc'] == 0, 'rc=%s' % (rec['rc'],))
    ctx.check('s3.scope', '只有首页未命中，详情页命中（资产是按页进键的）',
              text.count(MISS) == 1 and ('%s %s' % (MISS, HOME_GEN)) in text
              and HIT_LINE in text,
              text.strip().replace('\n', ' / ')[:400])
    ctx.check('s3.home-bytes', '首页产物字节不变（生成源码只带路径，不带像素）',
              after[HOME_GEN]['sha256'] == before[HOME_GEN]['sha256'], '')
    ctx.check('s3.home-mtime', '被重验的首页产物 mtime 变了（确实重写了一次）',
              after[HOME_GEN]['mtimeNs'] != before[HOME_GEN]['mtimeNs'], '')
    ctx.check('s3.detail-untouched', '未被影响的详情页产物完全没动（字节与 mtime）',
              after[DETAIL_GEN] == before[DETAIL_GEN], '')
    ctx.report.setdefault('snapshots', {})['s3'] = {'before': before, 'after': after}
    return original


def case_asset_restored(ctx, original):
    proj = ctx.proj
    home = os.path.join(proj, HOME_ASSET.replace('/', os.sep))
    with open(home, 'wb') as fh:
        fh.write(original)
    before = snapshot(proj, PRESERVE)
    rec = ctx.run('s4-asset-restored', ctx.gen_argv(), proj)
    after = snapshot(proj, PRESERVE)
    text = ctx.text(rec)
    ctx.check('s4.key', '换回原字节后回到**原键**：%s（键跟内容走，不是跟最近一次走）' % HIT_ALL,
              rec['rc'] == 0 and HIT_ALL in text,
              text.strip().replace('\n', ' / ')[:300])
    ctx.check('s4.frozen', '恢复这一次没有重写任何文件（字节与 mtime 都不动）',
              before == after, '变了：%s' % (changed(before, after),))
    ctx.report.setdefault('snapshots', {})['s4'] = {'before': before, 'after': after}


def case_asset_corrupt(ctx):
    proj = ctx.proj
    home = os.path.join(proj, HOME_ASSET.replace('/', os.sep))
    before = snapshot(proj, PRESERVE)
    with open(home, 'wb') as fh:
        fh.write(b'not a png at all')
    rec = ctx.run('s5-asset-corrupt', ctx.gen_argv(), proj)
    after = snapshot(proj, PRESERVE)
    err = ctx.text(rec, 'stderr')
    ctx.check('s5.rc', '坏图：rc==2（拒绝，不是「当没看见继续生成」）', rec['rc'] == 2,
              'rc=%s' % (rec['rc'],))
    ctx.check('s5.diag', '坏图：诊断码 %s' % ASSET_DIAG, ASSET_DIAG in err,
              (err.strip().splitlines() or [''])[0][:200])
    ctx.check('s5.abort', '坏图：「%s，磁盘无任何变更」' % BATCH_ABORT, BATCH_ABORT in err, '')
    ctx.check('s5.preserve', '坏图：两页产物与 Manifest 的字节/mtime 全部保旧',
              before == after, '变了：%s' % (changed(before, after),))
    ctx.report.setdefault('snapshots', {})['s5'] = {'before': before, 'after': after}
    return before


def case_asset_deleted(ctx, before_corrupt, original):
    proj = ctx.proj
    home = os.path.join(proj, HOME_ASSET.replace('/', os.sep))
    os.remove(home)
    rec = ctx.run('s6-asset-deleted', ctx.gen_argv(), proj)
    after = snapshot(proj, PRESERVE)
    err = ctx.text(rec, 'stderr')
    ctx.check('s6.rc', '删图：rc==2（拒绝）', rec['rc'] == 2, 'rc=%s' % (rec['rc'],))
    ctx.check('s6.diag', '删图：诊断码 %s' % ASSET_DIAG, ASSET_DIAG in err,
              (err.strip().splitlines() or [''])[0][:200])
    ctx.check('s6.preserve', '删图：产物与 Manifest 仍保旧（失败不留半成品）',
              before_corrupt == after, '变了：%s' % (changed(before_corrupt, after),))
    ctx.report.setdefault('snapshots', {})['s6'] = {'before': before_corrupt, 'after': after}
    with open(home, 'wb') as fh:
        fh.write(original)
    rec = ctx.run('s6-restore', ctx.gen_argv(), proj)
    text = ctx.text(rec)
    ctx.check('s6.restore', '恢复资产后生成回到 rc==0（拒绝之后可重跑）', rec['rc'] == 0,
              'rc=%s' % (rec['rc'],))
    # 「保旧」不只是产物与 Manifest：**成功那几次的缓存记录也必须还在**。
    # 两次失败批次若顺手清了缓存，恢复后就会变成未命中（重写一遍），这条会红。
    ctx.check('s6.cache-survived', '两次失败批次没有删掉成功缓存：恢复后仍是 %s' % HIT_ALL,
              HIT_ALL in text, text.strip().replace('\n', ' / ')[:300])


def case_context_changed(ctx):
    """上下文变化 vs 资产变化：两者都报同一个原因词，但**范围**不同。

    这一条是**反例**：它故意破坏固定环境。它证明的是「看到
    `package-sources-changed` 不等于资产变了」——所以矩阵里其余每一步都必须
    回到固定环境跑，否则测出来的东西根本不指向资产。
    """
    proj = ctx.proj
    before_ctx = snapshot(proj, CONTEXT_FILES)
    before_files = snapshot(proj, PRESERVE)
    changed_env = dict(ctx.proc.GO_ENV_FIXED)
    changed_env['GOFLAGS'] = None  # 删掉 -mod=readonly：键里的字面值变了
    rec = ctx.run('s7-context-changed', ctx.gen_argv(), proj, env_overrides=changed_env)
    after_ctx = snapshot(proj, CONTEXT_FILES)
    after_files = snapshot(proj, PRESERVE)
    text = ctx.text(rec)
    ctx.check('s7.rc', '只改构建上下文：rc==0', rec['rc'] == 0, 'rc=%s' % (rec['rc'],))
    ctx.check('s7.scope', '只改构建上下文：**两页同时**未命中（资产变化只会让一页未命中）',
              text.count(MISS) == 2, text.strip().replace('\n', ' / ')[:400])
    ctx.check('s7.reason', '两页的原因都是 %s（与资产变化同一个词，只能靠范围区分）'
              % CONTEXT_REASON, text.count(CONTEXT_REASON) == 2,
              '出现 %d 次' % text.count(CONTEXT_REASON))
    ctx.check('s7.rewrote', '未命中就是真的重写了：两页产物与 Manifest 的 mtime 都变了',
              changed(before_files, after_files) == sorted(PRESERVE),
              '变了：%s' % (changed(before_files, after_files),))
    ctx.check('s7.buildfiles', '构建上下文变化没有改写 go.mod / go.sum',
              before_ctx == after_ctx, '变了：%s' % (changed(before_ctx, after_ctx),))
    ctx.report.setdefault('snapshots', {})['s7-context'] = {
        'before': before_files, 'after': after_files,
        'changed': changed(before_files, after_files)}

    # 「回到固定环境不再重写」的比较基线必须取在**这一条**之前：
    # 拿上下文实验之前的快照来比，比的是 S7 自己造成的重写，不是这一条的重写。
    mid = snapshot(proj, PRESERVE)
    rec = ctx.run('s7-context-restored', ctx.gen_argv(), proj)
    text = ctx.text(rec)
    after = snapshot(proj, PRESERVE)
    ctx.check('s7.back', '回到固定环境又是 %s（上下文实验没把工程带坏）' % HIT_ALL,
              rec['rc'] == 0 and HIT_ALL in text,
              text.strip().replace('\n', ' / ')[:300])
    ctx.check('s7.back-frozen', '回到固定环境后也没有重写任何文件', mid == after,
              '变了：%s' % (changed(mid, after),))
    ctx.report.setdefault('snapshots', {})['s7-restored'] = {'before': mid, 'after': after}


# ---------------------------------------------------------------- 矩阵：变异


def copy_project(src, dst):
    if os.path.lexists(dst):
        raise Refused(EXIT_REFUSED, '变异副本目录已存在：%s' % dst)
    # 不带 .verify-projects（20MB+ 的 CLI 与阶段日志）与 .goxml（缓存/Manifest）：
    # 变异只关心源码能不能编译、断言会不会红。
    shutil.copytree(src, dst, ignore=shutil.ignore_patterns('.verify-projects', '.goxml'))


def mutate_text(path, old, new):
    data = read_bytes(path)
    old_b = old.encode('utf-8')
    if data.count(old_b) != 1:
        raise Refused(EXIT_REFUSED, '变异锚点必须唯一：%r 在 %s 出现 %d 次'
                      % (old.strip()[:40], path, data.count(old_b)))
    with open(path, 'wb') as fh:
        fh.write(data.replace(old_b, new.encode('utf-8')))


MUTATIONS = (
    {
        'id': 'm1-no-destroy-wrap',
        'why': '拆掉 wire.Build 里的 app.WithOnDestroy(r.View(), r.Shutdown)：组合仍能编译、'
               '两页仍能渲染，但关窗时不再取消 Router。',
        'edits': [(
            '\tview, err := app.WithOnDestroy(r.View(), r.Shutdown)\n'
            '\tif err != nil {\n'
            '\t\treturn nil, err\n'
            '\t}\n',
            '\tview := app.View(r.View())\n',
        )],
        'test': 'TestRouteDestroyWiringPresent',
        # 源码接线的判据：包装存在、且回调指向 r.Shutdown。变异后第一条就红。
        'expectAny': ['生产组合未带关闭包装'],
    },
    {
        'id': 'm2-per-page-model',
        'why': '两个页面工厂各自 NewGallery()：能编译、两页都能渲染，'
               '但详情页改的计数回不到首页（点击也没落在共享模型上）。',
        'edits': [
            ('\t\treturn home(m)\n', '\t\treturn home(model.NewGallery())\n'),
            ('\t\treturn detail(m)\n', '\t\treturn detail(model.NewGallery())\n'),
        ],
        'test': 'TestRouteSharedModelSurvivesRoundTrip',
        'expectAny': ['页面持有的模型不是共享的那份', '未命中「打开详情」',
                      '回到首页应渲染共享后的计数', '实际 LastAction'],
    },
)


def case_mutations(ctx):
    for mut in MUTATIONS:
        tree = os.path.join(ctx.work_dir, mut['id'])
        copy_project(ctx.proj, tree)
        for old, new in mut['edits']:
            mutate_text(os.path.join(tree, 'wire', 'wire.go'), old, new)

        rec = ctx.run(mut['id'] + '-build', [ctx.go, 'build', './...'], tree)
        ctx.check(mut['id'] + '.build',
                  '%s：变异后可编译（go build ./... rc==0，红不能来自编译失败）' % mut['id'],
                  rec['rc'] == 0, ctx.text(rec, 'stderr').strip()[:400])

        rec = ctx.run(mut['id'] + '-test',
                      [ctx.go, 'test', '-count=1', '-v', '-timeout', '10m',
                       '-run', '^%s$' % mut['test'], './view/...'], tree)
        text = ctx.text(rec)
        fail_marker = '--- FAIL: %s' % mut['test']
        hit = next((s for s in mut['expectAny'] if s in text), '')
        fail_line = next((ln.strip() for ln in text.splitlines() if 'route_test.go:' in ln), '')
        ctx.check(mut['id'] + '.red', '%s：目标用例 FAIL（rc!=0）' % mut['id'],
                  rec['rc'] != 0 and fail_marker in text, 'rc=%s' % (rec['rc'],))
        ctx.check(mut['id'] + '.assertion',
                  '%s：失败来自本用例自己的断言（命中其一：%s）'
                  % (mut['id'], ' / '.join(mut['expectAny'])), bool(hit), fail_line[:300])
        ctx.report['mutations'].append({'id': mut['id'], 'why': mut['why'], 'tree': tree,
                                        'test': mut['test'], 'matchedAssertion': hit,
                                        'failLine': fail_line})

        rec = ctx.run(mut['id'] + '-control',
                      [ctx.go, 'test', '-count=1', '-v', '-run', '^%s$' % mut['test'],
                       './view/...'], ctx.proj)
        audit = audit_test_run(ctx.text(rec).encode('utf-8'), {
            'expectedTests': [mut['test']],
            'completionMarker': ctx.spec['completionMarker'],
        })
        ctx.check(mut['id'] + '.control',
                  '%s：未变异工程上同名用例实际通过且有完成标记' % mut['test'],
                  rec['rc'] == 0 and not audit['problems'],
                  ' | '.join(audit['problems']) or '目标用例 PASS，完成标记一致')


def case_destroy_event_coverage(ctx, repo):
    """关窗的第二层证据：受控 DestroyEvent 覆盖两个循环，与源码接线分开记。

    `m1` 证明的是 `wire.go` 里那条接线（编译期事实）；这一条证明的是
    `app` 的两个事件循环收到 `DestroyEvent` 后**真的调用**了已注册的回调
    （运行期事实）。两层都不是「读文档得出的结论」。
    """
    rec = ctx.run('destroy-event-two-loops',
                  [ctx.go, 'test', '-count=1', '-v', '-timeout', '5m',
                   '-run', '^TestDestroyEventShutsDownWrappedRouter$', './pkg/runtime/app'],
                  repo)
    text = ctx.text(rec)
    ctx.check('destroy.event.pass', 'pkg/runtime/app 的受控 DestroyEvent 用例通过',
              rec['rc'] == 0, ctx.text(rec, 'stderr').strip()[:400])
    loops = [name for name in ('app-loop', 'integration-loop')
             if ('--- PASS: TestDestroyEventShutsDownWrappedRouter/' + name) in text]
    ctx.check('destroy.event.loops', '两个事件循环各自被覆盖（app-loop / integration-loop）',
              len(loops) == 2, '命中子用例：%s' % (loops,))


# ---------------------------------------------------------------- 参数与主流程


def build_parser():
    p = argparse.ArgumentParser(
        prog='verify-gallery-accept.py',
        description=__doc__.split('\n')[0],
        formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('--run-dir', required=True,
                   help='本轮运行目录：必须不存在，由本轮独占创建（已存在则拒绝）')
    p.add_argument('--lock-path', required=True, help='公共锁路径；三段串行，每段自己取自己放')
    p.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    p.add_argument('--go', default=None, help='Go 工具链路径（默认取 verify-projects 的默认值）')
    p.add_argument('--json', default=None, help='报告副本路径（已存在则拒绝）')
    p.add_argument('--timeout', type=float, default=900.0, help='矩阵单条命令超时秒数')
    p.add_argument('--main-timeout', type=float, default=2400.0,
                   help='主验收（委派 verify-projects）超时秒数')
    p.add_argument('--quiet', action='store_true', help='只打印最终结论与标记')
    return p


def preflight(args, repo):
    if not os.path.isfile(os.path.join(repo, 'go.mod')):
        raise Refused(EXIT_USAGE, '不是 Go 模块仓库（缺 go.mod）：%s' % repo)
    run_dir = os.path.abspath(args.run_dir)
    if os.path.lexists(run_dir):
        raise Refused(EXIT_REFUSED, '运行目录已存在（不覆盖历史证据）：%s' % run_dir)
    if args.json and os.path.lexists(args.json):
        raise Refused(EXIT_USAGE, '报告路径已存在（不覆盖历史证据）：%s' % args.json)
    lock_path = os.path.abspath(args.lock_path)
    if not os.path.isdir(os.path.dirname(lock_path)):
        raise Refused(EXIT_USAGE, '公共锁目录不存在：%s' % os.path.dirname(lock_path))
    template = os.path.join(repo, 'examples', 'gxml', 'testprojects', PROJECT)
    for rel in ('wire/wire.go.txt', 'view/home/home.gxml', 'view/detail/detail.gxml',
                HOME_ASSET, DETAIL_ASSET, 'cmd/gallery/main.go.txt'):
        if not os.path.isfile(os.path.join(template, rel.replace('/', os.sep))):
            raise Refused(EXIT_REFUSED, '模板缺失：%s' % os.path.join(template, rel))
    return run_dir, lock_path


def probe_lock(lockmod, lock_path, run_id):
    """预检：能拿到就立刻按自有 token 放掉。锁忙时一个目录都不创建。"""
    lock = lockmod.Lock(lock_path)
    try:
        lock.acquire('workbuddy', 'gallery-accept-probe', run_id)
    except lockmod.LockHeldError as exc:
        holder = exc.holder or {}
        raise Refused(EXIT_LOCK_BUSY, '公共锁被他人持有，未创建任何目录、未启动任何 Go 进程：%s'
                      % lock_path,
                      holder={k: holder.get(k) for k in
                              ('agent', 'task', 'runId', 'hostPid', 'acquiredAt')})
    released = lock.release()
    if not released:
        raise Refused(EXIT_LOCK_NOT_RELEASED, '预检锁未按本轮 token 释放：%s'
                      % (lock.release_error or '未知原因'))
    return {'tokenFingerprint': 'sha256:' + sha256_bytes((lock.token or '').encode('utf-8')),
            'released': True}


def new_run_id():
    stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
    return 'ga-%s-%s' % (stamp, os.urandom(3).hex())


def main(argv):
    parser = build_parser()
    args = parser.parse_args(argv)
    repo = os.path.abspath(args.repo) if args.repo else default_repo()

    vp = load_helper(repo, 'verify-projects.py', 'r22c_verify_projects')
    spec = vp.PROJECTS[PROJECT]
    if args.go is None:
        args.go = vp.GO_DEFAULT
    procmod = load_helper(repo, 'verify_examples_proc.py', 'r22c_verify_examples_proc')

    started = datetime.datetime.now(datetime.timezone.utc)
    run_id = new_run_id()
    # owned_run_dir 只在**本轮独占创建成功**后置真：拒绝路径（锁忙 / 用法错误 /
    # 目录已存在）一字节都不许写——尤其不能往那个「已存在」的目录里塞报告。
    owned_run_dir = False
    report = {
        'schema': SCHEMA,
        'runId': run_id,
        'repo': repo,
        'project': PROJECT,
        'templateDir': os.path.join(repo, 'examples', 'gxml', 'testprojects', PROJECT),
        'runDir': os.path.abspath(args.run_dir),
        'startedUtc': utc(),
        'endedUtc': None,
        'elapsedSec': None,
        'env': dict(procmod.GO_ENV_FIXED),
        'envNote': 'GOFLAGS 等构建上下文是缓存键的一项：裸 shell（无 GOFLAGS）会得到 '
                   'package-sources-changed 的上下文未命中。矩阵除 S7 外全部在本环境变量下执行。',
        'expectedTests': spec['expectedTests'],
        'completionMarker': spec['completionMarker'],
        'lock': {'path': None, 'probe': None, 'main': None, 'matrix': None},
        'checks': [],
        'commands': [],
        'mutations': [],
        'snapshots': {},
        'verdict': {'ok': False, 'code': EXIT_FAILED, 'reasons': []},
    }
    reasons = report['verdict']['reasons']

    try:
        run_dir, lock_path = preflight(args, repo)
        lockmod = load_helper(repo, 'verify-examples-lock.py', 'r22c_verify_examples_lock')
        report['lock']['path'] = lock_path
        report['lock']['probe'] = probe_lock(lockmod, lock_path, run_id)

        # 运行目录在预检与取锁之后才创建：锁忙 / 用法错误都**零新增**。
        try:
            os.mkdir(run_dir)
        except FileExistsError:
            raise Refused(EXIT_REFUSED, '运行目录已存在（并发本轮）：%s' % run_dir)
        owned_run_dir = True
        project_dir = os.path.join(run_dir, 'project')
        raw_dir = os.path.join(run_dir, 'raw')
        work_dir = os.path.join(run_dir, 'work')
        main_json = os.path.join(run_dir, 'main.json')
        for path in (raw_dir, work_dir):
            os.mkdir(path)
        report['projectDir'] = project_dir
        report['rawDir'] = raw_dir

        procmod = load_helper(repo, 'verify_examples_proc.py', 'r22c_verify_examples_proc')
        ctx = Ctx(project_dir, os.path.join(project_dir, '.verify-projects',
                                            'goxml.exe' if os.name == 'nt' else 'goxml'),
                  raw_dir, work_dir, args.go, args.timeout, spec, report, procmod)

        # ---- (b) 主验收：锁由子进程持有
        rec = run_main_stage(repo, run_dir, project_dir, main_json, run_id, args)
        report['lock']['main'] = {'rc': rec['rc'], 'caseDir': rec['caseDir'],
                                  'ok': rec['ok'], 'stdout': rec['stdout'],
                                  'stderr': rec['stderr']}
        if rec.get('ok') is not True:
            raise Refused(EXIT_FAILED, '主验收进程退出或进程树收口未证实',
                          caseDir=rec['caseDir'], okReasons=rec.get('okReasons'))
        if rec['rc'] == EXIT_LOCK_BUSY:
            raise Refused(EXIT_LOCK_BUSY,
                          '预检通过但委派时锁被他人抢到：未启动任何 Go 进程',
                          caseDir=rec['caseDir'])
        if rec['rc'] != 0 or not os.path.isfile(main_json):
            raise Refused(EXIT_FAILED, '主验收失败（verify-projects rc=%s）' % (rec['rc'],),
                          caseDir=rec['caseDir'],
                          stderr=procmod.decode_for_display(
                              read_bytes(rec['stderr']['path'])) if rec['stderr'].get('path') else '')
        main_report = json.loads(read_bytes(main_json).decode('utf-8'))
        audit_main(main_report, ctx, project_dir, spec)
        if not all(c['ok'] for c in report['checks']):
            raise Refused(EXIT_FAILED, '主验收的判据不成立，矩阵不再继续（工程不可信）')

        # ---- (c) 矩阵：本进程自己取锁、自己按 token 释放
        lock = lockmod.Lock(lock_path)
        try:
            lock.acquire('workbuddy', 'gallery-accept-matrix', run_id)
        except lockmod.LockHeldError as exc:
            holder = exc.holder or {}
            raise Refused(EXIT_LOCK_BUSY, '矩阵阶段锁被他人持有，未启动矩阵里的任何 Go 进程：%s'
                          % lock_path,
                          holder={k: holder.get(k) for k in
                                  ('agent', 'task', 'runId', 'hostPid', 'acquiredAt')})
        released = None
        try:
            case_destroy_event_coverage(ctx, repo)
            case_rerun_hit(ctx, 's1-rerun-hit', '第二次生成')
            case_rerun_hit(ctx, 's2-hit-stable', '第三次生成')
            original = case_asset_replaced(ctx)
            case_asset_restored(ctx, original)
            before_corrupt = case_asset_corrupt(ctx)
            case_asset_deleted(ctx, before_corrupt, original)
            case_context_changed(ctx)
            case_mutations(ctx)
        finally:
            released = lock.release()
            report['lock']['matrix'] = {
                'tokenFingerprint': 'sha256:' + sha256_bytes((lock.token or '').encode('utf-8')),
                'released': bool(released),
                'releaseError': lock.release_error or None}
        if not released:
            raise Refused(EXIT_LOCK_NOT_RELEASED, '矩阵锁未按本轮 token 释放：%s'
                          % (lock.release_error or '未知原因'))
    except Refused as exc:
        report['verdict'].update({'ok': False, 'code': exc.code, 'reason': exc.message})
        if exc.detail:
            report['verdict']['detail'] = exc.detail
        code = exc.code
    except Exception as exc:  # 未预料的异常也要留证据，不能静默成功
        report['verdict'].update({'ok': False, 'code': EXIT_FAILED,
                                  'reason': '%s: %s' % (type(exc).__name__, exc)})
        code = EXIT_FAILED
    else:
        failed = [c for c in report['checks'] if not c['ok']]
        reasons.extend('%s: %s' % (c['id'], c['name']) for c in failed)
        report['verdict'].update({'ok': not failed,
                                  'code': EXIT_OK if not failed else EXIT_FAILED,
                                  'reason': None if not failed else '有 %d 条判据不成立' % len(failed)})
        code = report['verdict']['code']
    finally:
        report['endedUtc'] = utc()
        report['elapsedSec'] = round(
            (datetime.datetime.now(datetime.timezone.utc) - started).total_seconds(), 3)
        report['counts'] = {
            'checks': len(report['checks']),
            'passed': sum(1 for c in report['checks'] if c['ok']),
            'failed': sum(1 for c in report['checks'] if not c['ok']),
            'commands': len(report['commands']),
        }
        # 只有真的创建了运行目录才写报告：锁忙/用法错误时磁盘上不该有本轮的任何东西。
        if owned_run_dir and os.path.isdir(report['runDir']):
            payload = (json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True)
                       + '\n').encode('utf-8')
            if args.json:
                # A concurrent owner keeps its file; the requested copy must
                # succeed before this invocation may report success.
                try:
                    fd = os.open(args.json, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                                 | getattr(os, 'O_BINARY', 0), 0o600)
                    with os.fdopen(fd, 'wb') as fh:
                        fh.write(payload)
                except OSError as exc:
                    code = EXIT_REFUSED if isinstance(exc, FileExistsError) else EXIT_FAILED
                    report['verdict'].update({
                        'ok': False, 'code': code,
                        'reason': '报告副本写入失败，未覆盖已有文件：%s' % exc,
                    })
                    payload = (json.dumps(report, ensure_ascii=False, indent=2,
                                          sort_keys=True) + '\n').encode('utf-8')
            with open(os.path.join(report['runDir'], 'report.json'), 'wb') as fh:
                fh.write(payload)

    if code == EXIT_OK:
        if not args.quiet:
            summarise(report)
        sys.stdout.write('GALLERY-ACCEPT-OK %s\n' % run_id)
        sys.stdout.flush()
    else:
        sys.stderr.write('GALLERY-ACCEPT-FAILED %s code=%d %s\n'
                         % (run_id, code, report['verdict'].get('reason') or ''))
        for c in report['checks']:
            if not c['ok']:
                sys.stderr.write('  [%s] %s: %s\n' % (c['id'], c['name'], c['detail']))
        sys.stderr.flush()
    return code


def summarise(report):
    counts = report.get('counts') or {}
    print('runDir    : %s' % report['runDir'])
    print('project   : %s' % report['project'])
    print('checks    : %d 条全部成立（命令 %d 条）'
          % (counts.get('checks', 0), counts.get('commands', 0)))
    for mut in report['mutations']:
        print('mutation  : %-20s test=%s' % (mut['id'], mut['test']))
    print('边界      : 窗口入口只构建、不运行；原生窗口 / IME / 真实 DPI 未自动验证')


if __name__ == '__main__':
    try:
        raise SystemExit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        raise SystemExit(130)

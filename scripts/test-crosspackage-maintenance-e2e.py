#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""R14-C：crosspackage-preferences 影子工程的**维护生命周期** E2E。

R13-C 证明了一次目录 generate 与逐 View 产物等价。本轮不复测生成形态，只测
「页面源增删」这一维护路径：源没了以后，产物、Manifest 登记与成功缓存三者
会以不同节奏漂移，脚本必须把每一步的真实状态钉住。

用例 1 `lifecycle-maintenance`（自有影子工程，真实 CLI）：
  L1 两页同批生成（都 miss、检查覆盖 2 个目标包）→ L2 下一次真命中（跳过×2，
  零字节零 mtime 改动，`--check` 仍执行）→ L3 删掉 summary 页面源（入口显式
  去接线并证明仍能构建），留下 `summary_gen.go` 孤儿 → L4 让存活目标
  `view/edit` 检查失败后执行局部 `--clean`：rc=2，孤儿、Manifest、缓存逐字保留
  → L5 修好后同一条 `--clean` 命令成功：只删已登记且有归属证明的孤儿，缓存
  旧记录**保留**（条数不变，不作为删除判据）→ L6 恢复页面源与入口：summary
  必须报 `cache-missing` 重新生成（旧缓存记录不得造成假命中），随后全命中、
  入口构建通过、离屏 11 用例全绿。

用例 2 `adoption-and-scope`（三份独立工程，同一材料）：
  A 用户接管产物（保护头被改）→ 跳过并保留；
  B 保护头 `// Source:` 与登记不符（归属歧义）→ 跳过并保留；
  C 范围外存活页面（只对 edit 做局部 --clean）→ 一个都不删。

变异（`--make-mutated-repo DEST --mutate NAME`，可复现）：
  `skip-clean`          L4/L5 都不带 `--clean` → 「只删已登记孤儿」「清理完成」
                        判据翻红；
  `clean-without-check` L4/L5 只带 `--clean` → 「检查失败时孤儿仍保留」判据翻红。

边界：零原生窗口（入口只 `go build`，从不执行）、不安装、不联网；每个用例
自持公共锁并在 finally 按自有 token 释放，变异用例本身不取锁（内层运行自己取）。
"""
import argparse
import difflib
import hashlib
import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys
import uuid
from datetime import datetime

GO = r'C:\Program Files\Go\bin\go.exe'
LOCK = r'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock'
PROJECT = 'crosspackage-preferences'
TOOL = 'verify-projects.py'
THIS = 'test-crosspackage-maintenance-e2e.py'

# 与 R12-C/R13-C 同源：过发现/包名解析/命名预检/GXML 分析，恰在提交前检查爆出。
TYPE_ERROR_LINE = 'var _ = noSuchSymbolR14C\n'

EDIT_GEN = 'view/edit/edit_gen.go'
SUMMARY_GEN = 'view/summary/summary_gen.go'
MANIFEST = '.goxml/generate-manifest.json'
SUMMARY_SOURCE = 'view/summary/summary.gxml'
# 删掉一个页面源 = 删掉「源 + 该包的普通文件」，只留生成物当孤儿。
SUMMARY_PAGE_FILES = [SUMMARY_SOURCE, 'view/summary/doc.go', 'view/summary/sem_test.go']

# 局部清理：只声明存活目标所在的包，孤儿属于另一页 → 必须由 --clean 处理。
CLEAN_ARGS = ['--check', '--clean']
# R14C-CLEAN-GATE

# 入口去接线（原文来自模板 cmd/preferences/main.go.txt，__MOD__ 换成 go.mod 模块路径）。
DEWIRE_EDITS = [
    ('\t"__MOD__/view/edit"\n\t"__MOD__/view/summary"\n',
     '\t"__MOD__/view/edit"\n'),
    ('type shell struct {\n\tedit    *edit.GoxmlEditView\n\tsummary *summary.GoxmlSummaryView\n}\n',
     'type shell struct {\n\tedit *edit.GoxmlEditView\n}\n'),
    ('\t\tlayout.Flexed(1, func(gtx layout.Context) layout.Dimensions {\n'
     '\t\t\treturn column(func(gtx layout.Context) layout.Dimensions {\n'
     '\t\t\t\treturn s.summary.Layout(gtx, th)\n'
     '\t\t\t})(gtx)\n'
     '\t\t}),\n',
     ''),
    ('\tsv, err := summary.NewGoxmlSummaryView(m)\n'
     '\tif err != nil {\n'
     '\t\tfmt.Fprintln(os.Stderr, err)\n'
     '\t\tos.Exit(1)\n'
     '\t}\n'
     '\tview := &shell{edit: ev, summary: sv}\n',
     '\tview := &shell{edit: ev}\n'),
]

CLEAN_SUMMARY_RE = re.compile(r'清理完成：删除 (\d+)，跳过 (\d+)')
MISS_RE = re.compile(r'缓存未命中 (\S+)：(\S+)')

# 变异锚点。拼接写法是刻意的：锚点字符串不能在 MUTATIONS 表里自身命中，
# 所以标记行在整个脚本里只出现一次（紧跟在 CLEAN_ARGS 那行之后）。
_GATE = '# R14C-' + 'CLEAN-GATE'

MUTATIONS = {
    # 忘了带 --clean：孤儿留在盘上，清理类判据必须翻红。
    'skip-clean': [(_GATE, _GATE + "\nCLEAN_ARGS = ['--check']")],
    # 清理时不带 --check：目标包坏了也会删孤儿，「失败时孤儿仍保留」必须翻红。
    'clean-without-check': [(_GATE, _GATE + "\nCLEAN_ARGS = ['--clean']")],
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


def write_bytes(path, data):
    with open(path, 'wb') as fh:
        fh.write(data)


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
    write_bytes(path, text.replace(old, new).encode(encoding))


def build_shadow(src_repo, dest, mutate=None):
    """整份复制仓库（不含 .git）做影子仓库；mutate 只改本脚本的清理参数。"""
    shutil.copytree(src_repo, dest, ignore=shutil.ignore_patterns('.git'),
                    ignore_dangling_symlinks=True)
    if mutate:
        for old, new in MUTATIONS[mutate]:
            patch_file(os.path.join(dest, 'scripts', THIS), old, new)
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

    def last_markers(self):
        return self.report['cases'][self.case]['steps'][-1]['markers']


def markers_of(out_raw, err_raw, tool):
    """从原始字节里提取本轮关心的 CLI 标记（判据只看标记，不看日志全文）。"""
    out = out_raw.decode('utf-8', 'replace')
    err = err_raw.decode('utf-8', 'replace')
    check_passed, check_failed = tool.check_markers(out_raw, err_raw)
    lines = out.splitlines()
    summary = CLEAN_SUMMARY_RE.search(out)
    clean_skips = [line for line in lines
                   if line.startswith('跳过 ') and '（缓存命中）' not in line]
    return {
        'hit': '（缓存命中）' in out,
        'hitCount': out.count('（缓存命中）'),
        'miss': '缓存未命中' in out,
        'missReasons': {m.group(1): m.group(2) for m in MISS_RE.finditer(out)},
        'generated': '已生成' in out,
        'generatedCount': sum(1 for line in lines if line.startswith('已生成 ')),
        'generatedLines': [line for line in lines if line.startswith('已生成 ')],
        'checkPassed': check_passed,
        'checkFailed': check_failed,
        'checkTargets': tool.check_targets(out_raw),
        'typeError': 'undefined: noSuchSymbolR14C' in err,
        'cleaned': [line for line in lines if line.startswith('已清理 ')],
        'cleanSkipped': clean_skips,
        'cleanSummary': (int(summary.group(1)), int(summary.group(2))) if summary else None,
    }


# ---------------------------------------------------------------- 快照 / 登记 / 缓存


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


def diff_snapshot(a, b):
    """比较两份快照。返回差异描述列表。"""
    problems = []
    for k in sorted(set(a) & set(b)):
        if (a[k]['sha256'], a[k]['bytes']) != (b[k]['sha256'], b[k]['bytes']):
            problems.append('内容变了 %s（%s → %s）'
                            % (k, a[k]['sha256'][:12], b[k]['sha256'][:12]))
        elif a[k]['mtimeNs'] != b[k]['mtimeNs']:
            problems.append('被重写但字节相同 %s（mtime 变化）' % k)
    for k in sorted(set(b) - set(a)):
        problems.append('新增 %s' % k)
    for k in sorted(set(a) - set(b)):
        problems.append('消失 %s' % k)
    return problems


def check_unchanged(ctx, project, before, phase):
    problems = diff_snapshot(before, snapshot(project))
    ctx.check(not problems, '%s 不得改动工程文件：%r' % (phase, problems))


def manifest_entries(project):
    path = os.path.join(project, MANIFEST)
    if not os.path.isfile(path):
        return {}
    with open(path, encoding='utf-8') as fh:
        return {e.get('output'): e for e in (json.load(fh).get('entries') or [])}


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


# ---------------------------------------------------------------- 材料准备


def module_path(project):
    with open(os.path.join(project, 'go.mod'), encoding='utf-8') as fh:
        for line in fh:
            if line.startswith('module '):
                return line.split(None, 1)[1].strip()
    raise AssertionError('go.mod 里没有 module 行：%s' % project)


def backup_file(project, rel, case_dir):
    """把源文件按相对路径备份到用例目录，恢复时逐字节写回。"""
    dest = os.path.join(case_dir, 'backup', rel.replace('/', os.sep))
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    write_bytes(dest, read_bytes(os.path.join(project, rel)))
    return dest


def restore_file(project, rel, case_dir):
    src = os.path.join(case_dir, 'backup', rel.replace('/', os.sep))
    dest = os.path.join(project, rel)
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    write_bytes(dest, read_bytes(src))


def dewire_entry(project, case_dir):
    """把入口里对 summary 视图的引用去掉，并留下 diff 作为改动记录（含原因）。"""
    rel = 'cmd/preferences/main.go'
    path = os.path.join(project, rel)
    orig = read_bytes(path)
    save = backup_file(project, rel, case_dir)
    text = orig.decode('utf-8')
    mod = module_path(project)
    for old, new in DEWIRE_EDITS:
        old_s, new_s = old.replace('__MOD__', mod), new.replace('__MOD__', mod)
        if text.count(old_s) != 1:
            raise AssertionError('入口去接线锚点命中 %d 次（应为 1）：%r'
                                 % (text.count(old_s), old_s[:60]))
        text = text.replace(old_s, new_s)
    new_bytes = text.encode('utf-8')
    write_bytes(path, new_bytes)
    diff = list(difflib.unified_diff(
        orig.decode('utf-8').splitlines(True), text.splitlines(True),
        fromfile=save.replace('\\', '/'), tofile=path.replace('\\', '/'), n=2))
    write_bytes(os.path.join(case_dir, 'entry-removed-summary.diff'),
                ''.join(diff).encode('utf-8'))
    return diff


def rewrite_header_line(path, index, new_line):
    """改写保护头第 index(0 起) 行，模拟用户接管 / 归属歧义。"""
    data = read_bytes(path)
    lines = data.decode('utf-8').split('\n')
    old = lines[index]
    lines[index] = new_line
    write_bytes(path, '\n'.join(lines).encode('utf-8'))
    return old


# ---------------------------------------------------------------- 用例 1：生命周期


def case_lifecycle(ctx):
    """L1 批次生成 → L2 命中 → L3 删页 → L4 检查失败保孤儿 → L5 局部清理 → L6 恢复。"""

    def body():
        cli = ctx.path('goxml.exe')
        res, _, err = ctx.step('l0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], ctx.repo)
        if not ctx.check(res['ok'] and res['rc'] == 0,
                         'CLI 构建失败 rc=%s：%s'
                         % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
            return ctx.report['cases'][ctx.case]['failures']
        proj = make_project(ctx, '影子工程', ctx.repo)
        batch = [cli, 'generate', 'view', '--out-dir', 'view', '--check']

        # L1 两页同批生成
        res, _, err = ctx.step('l1-generate', batch, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 0, 'L1 批次生成应成功 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        ctx.check(set(m['missReasons']) == {EDIT_GEN, SUMMARY_GEN},
                  'L1 首次应两页都未命中：%r' % m['missReasons'])
        ctx.check(m['generatedCount'] == 2, 'L1 应报两页已生成，实得 %d' % m['generatedCount'])
        ctx.check(m['checkTargets'] == 2, 'L1 检查应覆盖 2 个目标包，实得 %r' % m['checkTargets'])
        snap1 = snapshot(proj)
        ent1 = manifest_entries(proj)
        cache1 = cache_records(proj)
        ctx.check(sorted(ent1) == [EDIT_GEN, SUMMARY_GEN],
                  'L1 Manifest 应登记两页，实得 %r' % sorted(ent1))
        ctx.check(len(cache1) == 2, 'L1 成功缓存应有 2 条，实得 %d' % len(cache1))

        # L2 真命中：不重写、检查仍执行
        res, _, err = ctx.step('l2-hit', batch, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 0, 'L2 全命中应成功 rc=%s' % res['rc'])
        ctx.check(m['hitCount'] == 2, 'L2 两页都应命中，实得 %d' % m['hitCount'])
        ctx.check(not m['generated'], 'L2 全命中不应重写产物')
        ctx.check(m['checkTargets'] == 2, 'L2 全命中也必须执行检查且覆盖 2 个目标包，实得 %r'
                  % m['checkTargets'])
        problems = diff_snapshot(snap1, snapshot(proj))
        ctx.check(not problems, 'L2 全命中不得改动工程任何文件：%r' % problems)

        # L3 删掉 summary 页面源，入口显式去接线（入口引用了被删视图）
        for rel in SUMMARY_PAGE_FILES:
            backup_file(proj, rel, ctx.dir)
        for rel in SUMMARY_PAGE_FILES:
            os.remove(os.path.join(proj, rel))
        ctx.check(os.path.isfile(os.path.join(proj, SUMMARY_GEN)),
                  'L3 删源后生成物应仍在盘上（孤儿）')
        ctx.check(sorted(manifest_entries(proj)) == [EDIT_GEN, SUMMARY_GEN],
                  'L3 删源本身不应改动 Manifest 登记')
        diff = dewire_entry(proj, ctx.dir)
        ctx.check(len(diff) > 0, 'L3 入口去接线必须真的改到文件')
        res, _, err = ctx.step('l3-entry-build',
                               [GO, 'build', '-o', ctx.path('preferences-deleted.exe'),
                                './cmd/preferences'], proj)
        ctx.check(res['rc'] == 0, 'L3 去接线后入口应能构建 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))

        # L4 存活目标检查失败 → 局部 --clean 不得删孤儿
        doc = os.path.join(proj, 'view', 'edit', 'doc.go')
        doc_orig = read_bytes(doc)
        write_bytes(doc, doc_orig + TYPE_ERROR_LINE.encode('utf-8'))
        snap4 = snapshot(proj)
        clean_argv = [cli, 'generate', 'view/edit/edit.gxml', '--out-dir', 'view/edit'] + CLEAN_ARGS
        res, _, err = ctx.step('l4-clean-check-fails', clean_argv, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 2, 'L4 检查失败应以 2 退出（实得 %s）' % res['rc'])
        ctx.check(m['checkFailed'], 'L4 stderr 应含「提交前检查失败」：%r'
                  % err.decode('utf-8', 'replace')[-300:])
        ctx.check(m['typeError'], 'L4 失败原因应是目标包类型错误 undefined: noSuchSymbolR14C')
        ctx.check(not m['cleaned'], 'L4 检查失败时不得删除任何产物，实得 %r' % (m['cleaned'],))
        ctx.check(m['cleanSummary'] is None,
                  'L4 检查失败时清理阶段根本不应执行，实得 %r' % (m['cleanSummary'],))
        ctx.check(os.path.isfile(os.path.join(proj, SUMMARY_GEN)),
                  'L4 检查失败时孤儿产物必须保留')
        ctx.check(sorted(manifest_entries(proj)) == [EDIT_GEN, SUMMARY_GEN],
                  'L4 检查失败时 Manifest 登记必须保留')
        ctx.check(sorted(cache_records(proj)) == sorted(cache1),
                  'L4 检查失败时成功缓存必须保留')
        check_unchanged(ctx, proj, snap4, 'L4 检查失败时')

        # L5 修好后同一条 --clean 成功：只删已登记且有归属证明的孤儿
        write_bytes(doc, doc_orig)
        before_clean = snapshot(proj)
        res, _, err = ctx.step('l5-clean-success', clean_argv, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 0, 'L5 修好后清理应成功 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        ctx.check(m['cleaned'] == ['已清理 ' + SUMMARY_GEN],
                  'L5 只应清理那一页孤儿，实得 %r' % (m['cleaned'],))
        ctx.check(m['cleanSummary'] == (1, 0),
                  'L5 清理汇总应为「删除 1，跳过 0」，实得 %r' % (m['cleanSummary'],))
        ctx.check(not os.path.isfile(os.path.join(proj, SUMMARY_GEN)),
                  'L5 孤儿产物应已删除')
        ctx.check(manifest_entries(proj) == {EDIT_GEN: ent1.get(EDIT_GEN)},
                  'L5 Manifest 应只移除孤儿登记并保留存活页原条目')
        for rel in (SUMMARY_GEN, MANIFEST):
            before_clean.pop(rel, None)
        after_clean = snapshot(proj)
        after_clean.pop(MANIFEST, None)
        problems = diff_snapshot(before_clean, after_clean)
        ctx.check(not problems, 'L5 除孤儿及 Manifest 外不得改动文件：%r' % problems)

        # L6 恢复页面源与入口：旧缓存记录不得造成假命中
        for rel in SUMMARY_PAGE_FILES:
            restore_file(proj, rel, ctx.dir)
        restore_file(proj, 'cmd/preferences/main.go', ctx.dir)
        res, _, err = ctx.step('l6-regenerate', batch, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 0, 'L6 恢复页面后应能重新生成 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        ctx.check(m['missReasons'].get(SUMMARY_GEN) == 'cache-missing',
                  'L6 恢复页面必须报 cache-missing（旧记录不得假命中），实得 %r'
                  % (m['missReasons'],))
        ctx.check(m['hitCount'] == 1, 'L6 另一页应仍命中，实得 %d' % m['hitCount'])
        ctx.check(m['generatedLines'] == ['已生成 ' + SUMMARY_GEN],
                  'L6 只应重新生成恢复的那一页，实得 %r' % (m['generatedLines'],))
        ctx.check(cache_records(proj) == cache1,
                  'L6 重新生成后缓存键和语义内容应恢复')
        after_restore = snapshot(proj)
        for rel in (EDIT_GEN, SUMMARY_GEN, MANIFEST):
            ctx.check(after_restore.get(rel, {}).get('sha256') == snap1.get(rel, {}).get('sha256')
                      and rel in after_restore,
                      'L6 重新生成后应恢复 %s 的原始字节' % rel)
        res, _, err = ctx.step('l6-hit-again', batch, proj)
        m = ctx.last_markers()
        ctx.check(res['rc'] == 0 and m['hitCount'] == 2 and not m['generated'],
                  'L6 再跑一次应两页全命中且零重写，实得 hit=%d gen=%r'
                  % (m['hitCount'], m['generatedLines']))
        check_unchanged(ctx, proj, after_restore, 'L6 两页全命中时')
        res, _, err = ctx.step('l6-entry-build',
                               [GO, 'build', '-o', ctx.path('preferences-restored.exe'),
                                './cmd/preferences'], proj)
        ctx.check(res['rc'] == 0, 'L6 恢复入口后应能构建 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        res, out, err = ctx.step('l6-test',
                                 [GO, 'test', '-count=1', '-v', '-timeout', '20m',
                                  './view/...', '-run', '^TestCross'], proj)
        markers = ctx.tool.test_markers(out)
        ctx.report['cases'][ctx.case]['offscreen'] = {
            'rc': res['rc'], 'pass': markers['pass'], 'skip': markers['skip'],
            'fail': markers['fail'], 'completed': len(set(markers['completed']))}
        ctx.check(res['rc'] == 0, 'L6 离屏测试应通过 rc=%s：%s'
                  % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
        problem = ctx.tool.test_verdict(markers)
        ctx.check(problem is None, 'L6 离屏判据不成立：%s' % problem)
        return ctx.report['cases'][ctx.case]['failures']

    return with_lock(ctx, 'lifecycle-maintenance', body)


# ---------------------------------------------------------------- 用例 2：归属与范围


def case_adoption_and_scope(ctx):
    """用户接管 / 归属歧义 / 范围外存活页面：三者都必须原样保留。"""

    def body():
        cli = ctx.path('goxml.exe')
        res, _, err = ctx.step('a0-build-cli', [GO, 'build', '-o', cli, './cmd/goxml'], ctx.repo)
        if not ctx.check(res['ok'] and res['rc'] == 0,
                         'CLI 构建失败 rc=%s：%s'
                         % (res['rc'], err.decode('utf-8', 'replace')[-300:])):
            return ctx.report['cases'][ctx.case]['failures']
        clean_argv = [cli, 'generate', 'view/edit/edit.gxml', '--out-dir', 'view/edit'] + CLEAN_ARGS

        def prepare(name):
            proj = make_project(ctx, name, ctx.repo)
            res, _, err = ctx.step('a1-generate-' + name,
                                   [cli, 'generate', 'view', '--out-dir', 'view', '--check'], proj)
            if not ctx.check(res['rc'] == 0, '%s 基线生成失败 rc=%s：%s'
                             % (name, res['rc'], err.decode('utf-8', 'replace')[-300:])):
                return proj, None
            return proj, snapshot(proj)

        # A 用户接管产物：保护头被改，绝不能删
        proj, snap = prepare('用户接管')
        if snap:
            for rel in SUMMARY_PAGE_FILES:
                os.remove(os.path.join(proj, rel))
            orig = rewrite_header_line(os.path.join(proj, SUMMARY_GEN), 0,
                                       '// Copyright 2026 Example Corp. All rights reserved.')
            ctx.report['cases'][ctx.case].setdefault('adoption', {})['headerReplaced'] = orig
            before_clean = snapshot(proj)
            ctx.check(before_clean[SUMMARY_GEN]['sha256'] != snap[SUMMARY_GEN]['sha256'],
                      'A 夹具必须改动原始保护头')
            res, _, err = ctx.step('a2-clean-user-adopted', clean_argv, proj)
            m = ctx.last_markers()
            ctx.check(res['rc'] == 0, 'A 用户接管时清理应正常结束 rc=%s：%s'
                      % (res['rc'], err.decode('utf-8', 'replace')[-300:]))
            ctx.check(any('保护头不符，视为用户文件' in line for line in m['cleanSkipped']),
                      'A 应以「保护头不符，视为用户文件」跳过，实得 %r' % (m['cleanSkipped'],))
            ctx.check(os.path.isfile(os.path.join(proj, SUMMARY_GEN)),
                      'A 用户接管的产物必须保留')
            check_unchanged(ctx, proj, before_clean, 'A 用户接管时')
            ctx.check(sorted(manifest_entries(proj)) == [EDIT_GEN, SUMMARY_GEN],
                      'A 跳过的条目不得从 Manifest 移除')
            ctx.check(m['cleanSummary'] == (0, 1),
                      'A 清理汇总应为「删除 0，跳过 1」，实得 %r' % (m['cleanSummary'],))

        # B 归属歧义：保护头在，但 // Source: 与登记不符，绝不能删
        proj, snap = prepare('归属歧义')
        if snap:
            for rel in SUMMARY_PAGE_FILES:
                os.remove(os.path.join(proj, rel))
            orig = rewrite_header_line(os.path.join(proj, SUMMARY_GEN), 1,
                                       '// Source: other_summary.gxml')
            ctx.report['cases'][ctx.case].setdefault('adoption', {})['sourceReplaced'] = orig
            before_clean = snapshot(proj)
            ctx.check(before_clean[SUMMARY_GEN]['sha256'] != snap[SUMMARY_GEN]['sha256'],
                      'B 夹具必须改动原始 Source')
            res, _, err = ctx.step('a3-clean-ambiguous', clean_argv, proj)
            m = ctx.last_markers()
            ctx.check(res['rc'] == 0, 'B 归属歧义时清理应正常结束 rc=%s' % res['rc'])
            ctx.check(any('保护头 Source 与登记不符，归属歧义' in line
                          for line in m['cleanSkipped']),
                      'B 应以「归属歧义」跳过，实得 %r' % (m['cleanSkipped'],))
            ctx.check(os.path.isfile(os.path.join(proj, SUMMARY_GEN)),
                      'B 归属歧义的产物必须保留')
            check_unchanged(ctx, proj, before_clean, 'B 归属歧义时')
            ctx.check(sorted(manifest_entries(proj)) == [EDIT_GEN, SUMMARY_GEN],
                      'B 跳过的条目不得从 Manifest 移除')
            ctx.check(m['cleanSummary'] == (0, 1),
                      'B 清理汇总应为「删除 0，跳过 1」，实得 %r' % (m['cleanSummary'],))

        # C 范围外存活页面：只对 edit 做局部清理，summary 一个都不许动
        proj, snap = prepare('范围外存活')
        if snap:
            res, _, err = ctx.step('a4-clean-out-of-scope', clean_argv, proj)
            m = ctx.last_markers()
            ctx.check(res['rc'] == 0, 'C 范围外清理应正常结束 rc=%s' % res['rc'])
            ctx.check(m['cleanSummary'] == (0, 0),
                      'C 源都还在时不应有删除或跳过，实得 %r' % (m['cleanSummary'],))
            ctx.check(not m['cleaned'], 'C 不应删除任何文件，实得 %r' % (m['cleaned'],))
            problems = diff_snapshot(snap, snapshot(proj))
            ctx.check(not problems, 'C 范围外存活页面必须逐字保留：%r' % problems)
        return ctx.report['cases'][ctx.case]['failures']

    return with_lock(ctx, 'adoption-and-scope', body)


# ---------------------------------------------------------------- 变异：清理参数退化


def run_mutation(ctx, name, want_reds):
    """在影子仓库上跑本脚本（被变异过），要求指定判据翻红。"""
    shadow = build_shadow(ctx.repo, ctx.path('shadow-' + name), name)
    rep = ctx.path('report-' + name + '.json')
    work = ctx.path('work-' + name)
    argv = [sys.executable, os.path.join(shadow, 'scripts', THIS),
            '--work-root', work, '--report', rep, '--repo', shadow,
            '--case', 'lifecycle-maintenance']
    proc = subprocess.run(argv, cwd=ctx.dir, env=dict(os.environ, PYTHONIOENCODING='utf-8'),
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    write_bytes(ctx.path('stdout-' + name + '.txt'), proc.stdout)
    write_bytes(ctx.path('stderr-' + name + '.txt'), proc.stderr)
    if not os.path.isfile(rep):
        return ['变异运行没有产出报告（rc=%d）：%r'
                % (proc.returncode, proc.stderr.decode('utf-8', 'replace')[-400:])]
    data = load_report(rep)
    case = (data.get('cases') or {}).get('lifecycle-maintenance') or {}
    fails = list(case.get('failures') or [])
    ctx.report['cases'][ctx.case].setdefault('mutation', {})[name] = {
        'status': data.get('status'), 'failureCount': len(fails), 'failures': fails}
    problems = []
    if (data.get('status') == 'INCOMPLETE' and fails
            and all(str(f).startswith('SKIP') for f in fails)):
        return ['SKIP: 变异运行未取到公共锁，无法判定']
    if proc.returncode != 1 or data.get('status') != 'FAIL' or case.get('status') != 'FAIL':
        return ['变异运行未形成可判别失败（rc=%d, status=%r, case=%r）'
                % (proc.returncode, data.get('status'), case.get('status'))]
    steps = case.get('steps') or []
    if case.get('lockReleased') is not True or not steps or not all(s.get('infraOk') for s in steps):
        return ['变异运行缺少可信的步骤证据或公共锁未释放']
    if not fails:
        problems.append('变异 %s 后判据竟然全绿——说明这些断言没有牙' % name)
        return problems
    for want in want_reds:
        if not any(want in f for f in fails):
            problems.append('变异 %s 后应出现「%s」翻红，实得 %r' % (name, want, fails))
    return problems


def case_mutation_skip_clean(ctx):
    """不带 --clean：孤儿留在盘上，「只删已登记孤儿」必须翻红。"""
    return run_mutation(ctx, 'skip-clean',
                        ['L5 只应清理那一页孤儿', 'L5 孤儿产物应已删除'])


def case_mutation_clean_without_check(ctx):
    """清理不设检查门：目标包坏了也删孤儿，「失败时孤儿仍保留」必须翻红。"""
    return run_mutation(ctx, 'clean-without-check',
                        ['L4 检查失败应以 2 退出', 'L4 检查失败时孤儿产物必须保留'])


# ---------------------------------------------------------------- 入口调用与锁


def make_project(ctx, name, repo):
    """用一键入口 --copy-only 造实验工程（材料与模板同源，不走 Go、不取锁）。"""
    proj = ctx.path(name)
    env = dict(os.environ, PYTHONIOENCODING='utf-8')
    proc = subprocess.run(
        [sys.executable, os.path.join(repo, 'scripts', TOOL), PROJECT,
         '--out-dir', proj, '--repo', repo, '--copy-only'],
        cwd=ctx.dir, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    write_bytes(ctx.path(name + '-copy-stdout.txt'), proc.stdout)
    write_bytes(ctx.path(name + '-copy-stderr.txt'), proc.stderr)
    if proc.returncode != 0:
        raise AssertionError('--copy-only 失败 rc=%d：%r'
                             % (proc.returncode, proc.stderr.decode('utf-8', 'replace')[-300:]))
    return proj


def with_lock(ctx, task, body):
    """自身工件用例：自持公共锁，finally 按自有 token 释放（不嵌套外层锁）。"""
    lockmod = load_helper(ctx.repo, 'verify-examples-lock.py', 'lock_%s' % uuid.uuid4().hex[:6])
    lock = lockmod.Lock(LOCK)
    try:
        lock.acquire('r14c-e2e', task, 'r14c-%s' % uuid.uuid4().hex[:8])
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


def load_report(path):
    with open(path, encoding='utf-8') as fh:
        return json.load(fh)


CASES = [
    ('lifecycle-maintenance', case_lifecycle),
    ('adoption-and-scope', case_adoption_and_scope),
    ('mutation-skip-clean', case_mutation_skip_clean),
    ('mutation-clean-without-check', case_mutation_clean_without_check),
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

    report = {'schema': 'r14c-crosspackage-maintenance-e2e-1', 'repo': repo,
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
    print('CROSS-MAINT-E2E-%s cases=%d fail=%d skip=%d'
          % (report['status'], len(report['cases']), failures, skipped))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))

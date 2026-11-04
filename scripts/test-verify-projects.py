#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""`scripts/verify-projects.py` 的专属测试（R11-C）。

只测**新脚本自己的职责**，不重跑旧证据工具的变异矩阵，也不复制整套双宿主测试：

C1 覆盖（本文件的 `--slice c1`，默认）：
  * 输出目录所有权：空目录 / 普通文件 / 符号链接 / 缺父目录，一律拒绝且**零新增**；
  * 空格 + Unicode 输出目录成功，文件清单与模板映射逐一相符；
  * `go.mod` 里是**原生绝对路径 + Go 转义**（`C:\\Users\\...`），`go mod edit -json`
    能解析并还原成原生路径（这一段会启动一次 `go`，故显式取公共锁；锁忙则如实跳过）；
  * 模板源码在成功/失败后字节不变（模板指纹前后比对）；
  * 注释行不被替换（`// 并把 __MODULE__ 换成 ...` 原样保留），代码行被替换；
  * 用法错误（未知项目 / 报告路径已存在 / 全流程缺 --lock-path / --copy-only+--check）
    退 2 且不建目录；`--json` 报告与 `--log-dir` 证据落盘。

C2 覆盖：
  * 全流程闭环：真实构建 CLI → 原生 CLI 生成两个 View → 只构建窗口入口 → 离屏测试，
    逐阶段核 argv/cwd/rc/收口/raw 哈希，并核「入口二进制从未作为 argv[0] 被执行」；
  * 锁忙：退 8，**连输出目录都不创建**，报告不记他人 token 指纹；
  * 离屏测试判据：0 用例 / SKIP 兜底 / no test files 一律不成立。

约定：
  * 只写 `--work-root` / `--report` 指定的位置；每个用例一个独立子目录，不复用。
  * 不安装、不联网、不开窗口、不跑采样；不改全局 TMP/TEMP/HOME。
  * 需要 Go 的探针**先取锁、finally 按自有 token 释放**；锁忙记 `skipped` 而不是假绿。

示例::

    python scripts/test-verify-projects.py \
        --work-root "D:/delivery/work" --report "D:/delivery/out/test-verify-projects.json"
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

# 模板 9 个文件 → 工程 10 个 + 从仓库根补的 go.sum；硬编码是**判据的一部分**：
# 若复制漏文件/多文件，这里必须红（用「按模板推导」的期望值会跟着漏）。
EXPECTED_FILES = [
    'README.md',
    'cmd/preferences/main.go',
    'go.mod',
    'go.sum',
    'model/preferences.go',
    'view/edit/doc.go',
    'view/edit/edit.gxml',
    'view/edit/sem_test.go',
    'view/summary/doc.go',
    'view/summary/sem_test.go',
    'view/summary/summary.gxml',
]
# 只有含占位符的代码/指令行会被替换；gxml/doc.go/README 没有占位符。
EXPECTED_SUBSTITUTED = [
    'cmd/preferences/main.go',
    'go.mod',
    'view/edit/sem_test.go',
    'view/summary/sem_test.go',
]


# 有判别力的变异：每个都只改**一条**判据，且必须编译/执行得通（否则不是变异，
# 是崩溃）。锚点唯一命中由 patch_file 强制。用 --make-mutated-repo 生成影子仓库。
MUTATION_ANCHORS = {
    # 绕过目录所有权：把「已存在即拒绝」改成 exist_ok=True 直接往里写。
    'ownership': [
        ('    if kind is not None:\n        raise Refused(EXIT_REFUSED,',
         '    if False:\n        raise Refused(EXIT_REFUSED,'),
        ('    try:\n        os.mkdir(path)',
         '    try:\n        os.makedirs(path, exist_ok=True)'),
    ],
    # 子命令失败仍报成功：阶段退出码判据被抹平（收口判据仍在）。
    'success-on-failure': [
        ("    if res['rc'] != 0:\n        raise Refused(EXIT_BUSINESS",
         "    if False:\n        raise Refused(EXIT_BUSINESS"),
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


def write_json(path, obj):
    parent = os.path.dirname(os.path.abspath(path))
    if parent and not os.path.isdir(parent):
        os.makedirs(parent)
    # A test runner must not overwrite a previous report or its user's .tmp file.
    with open(path, 'x', encoding='utf-8', newline='\n') as fh:
        json.dump(obj, fh, ensure_ascii=False, indent=2, sort_keys=True)
        fh.write('\n')


def fingerprint(root):
    """目录下所有文件的 (相对路径, 字节数, sha256)，用于证明「零新增 / 未被写」。"""
    out = []
    if not os.path.isdir(root):
        return out
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in sorted(filenames):
            full = os.path.join(dirpath, name)
            out.append({'path': os.path.relpath(full, root).replace('\\', '/'),
                        'bytes': os.path.getsize(full), 'sha256': sha256_file(full)})
    return out


class Ctx(object):
    """单个用例的上下文：独立目录、命令执行、证据落盘。"""

    def __init__(self, case, work_root, repo):
        self.case = case
        self.repo = repo
        self.dir = os.path.join(work_root, case)
        self.runs = 0
        if not os.path.isdir(self.dir):
            os.makedirs(self.dir)

    def path(self, *parts):
        return os.path.join(self.dir, *parts)

    def run_tool(self, out_dir, *extra, json_report=None, copy_only=True):
        argv = [sys.executable, os.path.join(self.repo, 'scripts', TOOL), PROJECT,
                '--out-dir', out_dir, '--repo', self.repo]
        if copy_only:
            argv.append('--copy-only')
        argv.extend(extra)
        if json_report:
            argv.extend(['--json', json_report])
        env = dict(os.environ)
        # 子进程 stdout/stderr 走 UTF-8，避免 Windows 管道用 cp936 把中文摘要搅乱。
        env['PYTHONIOENCODING'] = 'utf-8'
        proc = subprocess.run(argv, cwd=self.dir, env=env, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE)
        self.runs += 1
        tag = 'run%d' % self.runs
        with open(self.path(tag + '-stdout.txt'), 'wb') as fh:
            fh.write(proc.stdout)
        with open(self.path(tag + '-stderr.txt'), 'wb') as fh:
            fh.write(proc.stderr)
        with open(self.path(tag + '-argv.json'), 'w', encoding='utf-8', newline='\n') as fh:
            json.dump({'argv': argv, 'cwd': self.dir, 'rc': proc.returncode},
                      fh, ensure_ascii=False, indent=2)
            fh.write('\n')
        return {'rc': proc.returncode,
                'stdout': proc.stdout.decode('utf-8', 'replace'),
                'stderr': proc.stderr.decode('utf-8', 'replace'), 'argv': argv}


def load_tool_module(repo):
    """直接 import 被测脚本，用来比对「报告里的值」与「脚本内的规则」是否一致。"""
    path = os.path.join(repo, 'scripts', TOOL)
    spec = importlib.util.spec_from_file_location('verify_projects', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def load_lock_helper(repo):
    path = os.path.join(repo, 'scripts', 'verify-examples-lock.py')
    spec = importlib.util.spec_from_file_location('verify_examples_lock', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# ---------------------------------------------------------------- 用例


def case_refuse_empty_dir(ctx, tool):
    out = ctx.path('已有的空目录')
    os.makedirs(out)
    before = fingerprint(out)
    res = ctx.run_tool(out)
    fails = []
    if res['rc'] != 3:
        fails.append('空目录应拒绝并退 3，实得 rc=%d' % res['rc'])
    if 'VERIFY-PROJECTS-FAILED' not in res['stderr'] or 'code=3' not in res['stderr']:
        fails.append('stderr 缺少失败标记 code=3：%r' % res['stderr'][-200:])
    if fingerprint(out) != before:
        fails.append('拒绝后空目录被写入（应零新增）：%r' % fingerprint(out))
    return fails


def case_refuse_plain_file(ctx, tool):
    out = ctx.path('已有文件')
    with open(out, 'wb') as fh:
        fh.write('他人的文件\n'.encode('utf-8'))
    before = sha256_file(out)
    res = ctx.run_tool(out)
    fails = []
    if res['rc'] != 3:
        fails.append('已存在的普通文件应拒绝并退 3，实得 rc=%d' % res['rc'])
    if not os.path.isfile(out) or sha256_file(out) != before:
        fails.append('拒绝后原文件被改写/删除')
    return fails


def case_refuse_symlink(ctx, tool):
    target = ctx.path('链接目标')
    os.makedirs(target)
    link = ctx.path('符号链接')
    try:
        os.symlink(target, link, target_is_directory=True)
    except (OSError, NotImplementedError) as exc:
        return ['SKIP: 本机不支持创建符号链接（%s）' % exc]
    before = fingerprint(target)
    res = ctx.run_tool(link)
    fails = []
    if res['rc'] != 3:
        fails.append('符号链接应拒绝并退 3，实得 rc=%d' % res['rc'])
    if not os.path.islink(link):
        fails.append('拒绝后符号链接被替换成真实目录（应保持原样）')
    if fingerprint(target) != before:
        fails.append('拒绝后链接目标被写入（穿越写入）：%r' % fingerprint(target))
    return fails


def case_refuse_missing_parent(ctx, tool):
    out = ctx.path('不存在的父', '工程')
    res = ctx.run_tool(out)
    fails = []
    if res['rc'] != 3:
        fails.append('父目录缺失应拒绝并退 3，实得 rc=%d' % res['rc'])
    if os.path.lexists(out):
        fails.append('拒绝后仍创建了输出目录（不应递归造路径）')
    if os.path.exists(ctx.path('不存在的父')):
        fails.append('拒绝后仍创建了中间父目录')
    return fails


def case_success_spaces_unicode(ctx, tool):
    out = ctx.path('我的 工程 v2（副本）')
    report = ctx.path('report.json')
    res = ctx.run_tool(out, json_report=report)
    fails = []
    if res['rc'] != 0:
        fails.append('空格/Unicode 路径应成功，实得 rc=%d stderr=%r'
                     % (res['rc'], res['stderr'][-300:]))
        return fails
    if 'VERIFY-PROJECTS-OK' not in res['stdout']:
        fails.append('成功时缺少 VERIFY-PROJECTS-OK 完成标记')

    got = sorted(f['path'] for f in fingerprint(out) if not f['path'].startswith('.verify-projects/'))
    if got != EXPECTED_FILES:
        fails.append('工程文件清单不符：多=%r 缺=%r'
                     % (sorted(set(got) - set(EXPECTED_FILES)),
                        sorted(set(EXPECTED_FILES) - set(got))))

    # go.mod：模块行替换、replace 是原生绝对路径的合法 Go 字面量
    gomod = read_bytes(os.path.join(out, 'go.mod')).decode('utf-8')
    if 'module example.com/%s\n' % PROJECT not in gomod:
        fails.append('go.mod 的 module 行未替换：%r' % gomod.split('\n')[0:8])
    expect_replace = 'replace github.com/goxml/goxml => %s' % tool.go_quote(ctx.repo)
    if expect_replace not in gomod:
        fails.append('go.mod 的 replace 行不是预期的转义原生路径：期望 %r' % expect_replace)
    if '\\\\' not in gomod.split('replace')[1]:
        fails.append('go.mod 的 replace 路径没有转义反斜杠（原生 Windows 路径应是 \\\\）')
    if '=> "' not in gomod:
        fails.append('go.mod 的 replace 行缺少引号')

    # 代码行替换、注释行不替换（同一条规则的两个方向）
    main_go = read_bytes(os.path.join(out, 'cmd', 'preferences', 'main.go')).decode('utf-8')
    if '"example.com/%s/model"' % PROJECT not in main_go:
        fails.append('main.go 的 import 行未替换为工程模块路径')
    if '// 并把 __MODULE__ 换成 go.mod 里的模块路径。' not in main_go:
        fails.append('main.go 的注释行被替换了（注释应保留模板原话）')

    # go.sum 与仓库根逐字节一致（离线构建的前提）
    if sha256_file(os.path.join(out, 'go.sum')) != sha256_file(os.path.join(ctx.repo, 'go.sum')):
        fails.append('go.sum 与仓库根不一致')

    if not os.path.isfile(report):
        fails.append('--json 报告未落盘')
    else:
        with open(report, encoding='utf-8') as fh:
            data = json.load(fh)
        if not data.get('verdict', {}).get('ok'):
            fails.append('报告 verdict.ok 不是 true：%r' % data.get('verdict'))
        if data.get('verdict', {}).get('code') != 0:
            fails.append('报告 verdict.code 不是 0：%r' % data.get('verdict'))
        if data.get('mode') != 'copy-only':
            fails.append('报告 mode 不是 copy-only：%r' % data.get('mode'))
        if data.get('templateUntouched') is not True:
            fails.append('报告未记录模板未被改动（templateUntouched != true）')
        if sorted(data.get('substituted') or []) != EXPECTED_SUBSTITUTED:
            fails.append('替换清单不符：%r' % (data.get('substituted'),))
        if len(data.get('files') or []) != len(EXPECTED_FILES):
            fails.append('报告 files 数不是 %d：%d' % (len(EXPECTED_FILES),
                                                   len(data.get('files') or [])))
    return fails


def case_template_untouched(ctx, tool):
    """成功与拒绝两条路之后，模板都必须字节不变（不写源码材料）。"""
    before = fingerprint(os.path.join(ctx.repo, 'examples', 'gxml', 'testprojects', PROJECT))
    fails = []
    ok = ctx.run_tool(ctx.path('新工程'))
    if ok['rc'] != 0:
        fails.append('正常复制应成功，实得 rc=%d' % ok['rc'])
    bad = ctx.run_tool(ctx.path('新工程'))  # 第二轮：目录已存在 → 拒绝
    if bad['rc'] != 3:
        fails.append('同一目录第二次运行应拒绝并退 3，实得 rc=%d' % bad['rc'])
    after = fingerprint(os.path.join(ctx.repo, 'examples', 'gxml', 'testprojects', PROJECT))
    if after != before:
        fails.append('模板源码被改动：前 %d 个文件 / 后 %d 个文件' % (len(before), len(after)))
    if not before:
        fails.append('模板指纹为空——判据无效')
    return fails


def case_usage_errors(ctx, tool):
    fails = []
    # 全流程缺 --lock-path：退 2，且不建目录（公共锁必须显式给）
    out0 = ctx.path('无锁全流程')
    res0 = ctx.run_tool(out0, copy_only=False)
    if res0['rc'] != 2:
        fails.append('全流程缺 --lock-path 应退 2，实得 %d' % res0['rc'])
    if os.path.lexists(out0):
        fails.append('缺 --lock-path 时仍创建了输出目录')
    # --copy-only 与 --check 互斥：退 2
    out1 = ctx.path('copy-only加check')
    res1 = ctx.run_tool(out1, '--check')
    if res1['rc'] != 2:
        fails.append('--copy-only --check 应退 2，实得 %d' % res1['rc'])
    if os.path.lexists(out1):
        fails.append('--copy-only --check 时仍创建了输出目录')
    # 未知项目：退 2，不建目录
    out = ctx.path('未知项目输出')
    env = dict(os.environ)
    env['PYTHONIOENCODING'] = 'utf-8'
    proc = subprocess.run([sys.executable, os.path.join(ctx.repo, 'scripts', TOOL),
                           'no-such-project', '--out-dir', out, '--repo', ctx.repo],
                          cwd=ctx.dir, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode != 2:
        fails.append('未知项目应退 2，实得 %d' % proc.returncode)
    if os.path.lexists(out):
        fails.append('未知项目也创建了输出目录')
    # 报告路径已存在：退 2，且不建输出目录（不覆盖历史证据）
    existing = ctx.path('已有报告.json')
    with open(existing, 'wb') as fh:
        fh.write(b'{"old": true}\n')
    out2 = ctx.path('报告冲突输出')
    res = ctx.run_tool(out2, json_report=existing)
    if res['rc'] != 2:
        fails.append('报告路径已存在应退 2，实得 %d' % res['rc'])
    if read_bytes(existing) != b'{"old": true}\n':
        fails.append('已存在的报告被覆盖')
    if os.path.lexists(out2):
        fails.append('报告冲突时仍创建了输出目录')
    return fails


def case_refusal_report(ctx, tool):
    """拒绝时也必须留下报告与证据（失败保留证据）。"""
    out = ctx.path('已存在')
    os.makedirs(out)
    report = ctx.path('refuse-report.json')
    res = ctx.run_tool(out, json_report=report)
    fails = []
    if res['rc'] != 3:
        fails.append('应退 3，实得 %d' % res['rc'])
    if not os.path.isfile(report):
        fails.append('拒绝时未落盘报告')
        return fails
    with open(report, encoding='utf-8') as fh:
        data = json.load(fh)
    if data.get('verdict', {}).get('code') != 3:
        fails.append('报告 verdict.code 不是 3：%r' % data.get('verdict'))
    if data.get('verdict', {}).get('ok') is not False:
        fails.append('报告 verdict.ok 不是 false')
    detail = data.get('verdict', {}).get('detail') or {}
    if detail.get('existingKind') != 'dir':
        fails.append('报告的 existingKind 不是 dir：%r' % detail)
    if data.get('lock', {}).get('acquired'):
        fails.append('未进入任何阶段却记录了持锁')
    return fails


def case_custom_log_dir(ctx, tool):
    out = ctx.path('自定义证据工程')
    log_dir = ctx.path('外部证据')
    res = ctx.run_tool(out, '--log-dir', log_dir)
    fails = []
    if res['rc'] != 0:
        fails.append('自定义 --log-dir 应成功，实得 %d' % res['rc'])
        return fails
    inv = os.path.join(log_dir, 'copy-inventory.json')
    if not os.path.isfile(inv):
        fails.append('--log-dir 下没有 copy-inventory.json')
    if os.path.isdir(os.path.join(out, '.verify-projects')):
        fails.append('指定 --log-dir 后仍在工程内建了证据目录（两处证据会互相矛盾）')
    if os.path.isdir(log_dir) and fingerprint(os.path.join(out, '.verify-projects')):
        fails.append('工程内证据目录非空')
    return fails


def case_gomod_parses(ctx, tool):
    """`go mod edit -json` 解析生成的 go.mod，证明转义后的路径是合法 Go 字面量。

    会启动一次 `go`（只解析元数据，不编译）——按纪律先取公共锁；锁忙记为 skipped，
    不把「没跑到」写成通过。
    """
    out = ctx.path('路径含 空格 的工程')
    res = ctx.run_tool(out)
    if res['rc'] != 0:
        return ['生成失败，无法校验 go.mod：rc=%d' % res['rc']]
    lockmod = load_lock_helper(ctx.repo)
    lock = lockmod.Lock(LOCK)
    run_id = 'tvp-%s' % uuid.uuid4().hex[:8]
    try:
        lock.acquire('r11c-test-verify-projects', 'C1 go.mod 解析探针', run_id)
    except lockmod.LockHeldError as exc:
        return ['SKIP: 公共锁被他人持有（holder=%r），按纪律不启动 go'
                % ((exc.holder or {}).get('agent'),)]
    try:
        proc = subprocess.run([GO, 'mod', 'edit', '-json', 'go.mod'], cwd=out,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              env=dict(os.environ, CGO_ENABLED='0'))
        with open(ctx.path('gomod-edit.json'), 'wb') as fh:
            fh.write(proc.stdout)
        with open(ctx.path('gomod-edit-stderr.txt'), 'wb') as fh:
            fh.write(proc.stderr)
        if proc.returncode != 0:
            return ['go mod edit -json 解析失败 rc=%d：%r'
                    % (proc.returncode, proc.stderr.decode('utf-8', 'replace')[-400:])]
        data = json.loads(proc.stdout.decode('utf-8'))
        repl = data['Replace'][0]['New']['Path']
        fails = []
        if os.path.normcase(repl) != os.path.normcase(os.path.abspath(ctx.repo)):
            fails.append('replace 还原后的路径不是仓库原生的绝对路径：%r != %r'
                         % (repl, os.path.abspath(ctx.repo)))
        if '\\\\' in repl:
            fails.append('还原后的路径仍带转义反斜杠，说明转义层级错了：%r' % repl)
        return fails
    finally:
        released = lock.release()
        if not released:
            # 释放失败不能被上面的 return 吞掉：直接把证据写进报告目录。
            with open(ctx.path('LOCK-NOT-RELEASED.txt'), 'w', encoding='utf-8') as fh:
                fh.write('release_error=%s path=%s\n' % (lock.release_error, lock.path))


def case_lock_busy_no_go(ctx, tool):
    """他人持锁：退 8，**不创建输出目录**、不启动任何 Go 进程。"""
    lockmod = load_lock_helper(ctx.repo)
    lock = lockmod.Lock(LOCK)
    run_id = 'tvp-hold-%s' % uuid.uuid4().hex[:8]
    try:
        lock.acquire('r11c-test-verify-projects', 'C2 锁忙边界', run_id)
    except lockmod.LockHeldError as exc:
        return ['SKIP: 公共锁已被他人持有，无法制造「我持锁」场景（holder=%r）'
                % ((exc.holder or {}).get('agent'),)]
    try:
        out = ctx.path('锁忙时的输出目录')
        report = ctx.path('busy-report.json')
        res = ctx.run_tool(out, '--lock-path', LOCK, json_report=report, copy_only=False)
        fails = []
        if res['rc'] != 8:
            fails.append('锁忙应退 8，实得 %d（stderr=%r）' % (res['rc'], res['stderr'][-300:]))
        if os.path.lexists(out):
            fails.append('锁忙时仍创建了输出目录（应在取锁之前不落任何文件）')
        if os.path.isdir(os.path.join(out, '.verify-projects')):
            fails.append('锁忙时仍建了证据目录')
        if os.path.isfile(report):
            with open(report, encoding='utf-8') as fh:
                data = json.load(fh)
            if not data.get('lock', {}).get('busy'):
                fails.append('报告未记录 lock.busy=true')
            if data.get('stages'):
                fails.append('锁忙却记录了阶段：%r' % [s['name'] for s in data['stages']])
            if data.get('lock', {}).get('tokenFingerprint'):
                fails.append('锁忙时记录了 token 指纹（他人锁不该被我记录）')
        else:
            fails.append('锁忙时没有落盘报告')
        return fails
    finally:
        if not lock.release():
            with open(ctx.path('LOCK-NOT-RELEASED.txt'), 'w', encoding='utf-8') as fh:
                fh.write('release_error=%s\n' % lock.release_error)


def case_test_verdict_unit(ctx, tool):
    """`test_verdict` 的判据方向：0 用例 / SKIP 兜底 / no test files 都必须不成立。"""
    fails = []
    expected = set()
    view = os.path.join(ctx.repo, 'examples', 'gxml', 'testprojects', PROJECT, 'view')
    import re
    for package in ('edit', 'summary'):
        with open(os.path.join(view, package, 'sem_test.go.txt'), encoding='utf-8') as fh:
            expected.update(re.findall(r'func (TestCross\w+)\(', fh.read()))
    if len(expected) != 11:
        fails.append('独立模板fixture应有11个case')
    ok = {'pass': 11, 'skip': 0, 'fail': 0, 'noTestFiles': 0,
          'completed': sorted(expected), 'passedNames': sorted(expected)}
    if tool.test_verdict(ok) is not None:
        fails.append('正常标记应判成立，实得 %r' % tool.test_verdict(ok))
    for name, markers in [
            ('零用例', {'pass': 0, 'skip': 0, 'fail': 0, 'noTestFiles': 0}),
            ('SKIP 兜底', {'pass': 11, 'skip': 1, 'fail': 0, 'noTestFiles': 0}),
            ('有 FAIL', {'pass': 11, 'skip': 0, 'fail': 1, 'noTestFiles': 0}),
            ('没有测试文件', {'pass': 0, 'skip': 0, 'fail': 0, 'noTestFiles': 1})]:
        if tool.test_verdict(markers) is None:
            fails.append('%s 应判不成立，却返回 None' % name)
    # 真解析一遍 `go test -v` 的输出片段，确认标记不是靠字段名猜的
    raw = ('=== RUN   TestCrossA\n--- PASS: TestCrossA (0.01s)\n'
           '=== RUN   TestCrossB\n--- SKIP: TestCrossB (0.00s)\n').encode('utf-8')
    markers = tool.test_markers(raw)
    if markers != {'pass': 1, 'skip': 1, 'fail': 0, 'noTestFiles': 0,
                   'completed': [], 'passedNames': ['TestCrossA']}:
        fails.append('test_markers 解析不符：%r' % (markers,))
    # Compilable early-return output is still PASS, but lacks the final marker.
    early = b'\n'.join(('--- PASS: %s (0.00s)' % n).encode() for n in expected)
    if tool.test_verdict(tool.test_markers(early)) is None:
        fails.append('11个提前返回PASS无完成标记仍被接受')
    missing = dict(ok, completed=sorted(expected)[1:])
    duplicate = dict(ok, completed=sorted(expected) + [sorted(expected)[0]])
    wrong = dict(ok, completed=[n + 'Suffix' for n in expected])
    for markers in (missing, duplicate, wrong):
        if tool.test_verdict(markers) is None:
            fails.append('缺失/重复/错名完成标记被接受')
    return fails


def find_key(obj, name, path=''):
    """递归找 JSON 里叫 name 的键，返回命中路径列表（用来证明明文 token 没落盘）。"""
    hits = []
    if isinstance(obj, dict):
        for k, v in obj.items():
            here = path + '/' + str(k)
            if k == name:
                hits.append(here)
            hits.extend(find_key(v, name, here))
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            hits.extend(find_key(v, name, path + '/%d' % i))
    return hits


def case_full_pipeline(ctx, tool):
    """全流程闭环：真实构建 CLI → 生成两个 View → 只构建入口 → 离屏测试。"""
    out = ctx.path('全流程 空格 工程')
    report = ctx.path('full-report.json')
    res = ctx.run_tool(out, '--lock-path', LOCK, json_report=report, copy_only=False)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有（未启动 Go），本轮无法验证全流程']
    fails = []
    if res['rc'] != 0:
        fails.append('全流程应成功，实得 rc=%d stderr=%r' % (res['rc'], res['stderr'][-400:]))
        return fails
    if 'VERIFY-PROJECTS-OK' not in res['stdout']:
        fails.append('缺少 VERIFY-PROJECTS-OK 完成标记')

    with open(report, encoding='utf-8') as fh:
        data = json.load(fh)
    names = [s['name'] for s in data['stages']]
    want = ['copy', 'build-cli', 'generate:view/edit/edit.gxml',
            'generate:view/summary/summary.gxml', 'build-entry', 'test']
    if names != want:
        fails.append('阶段顺序不符：%r' % (names,))
    for stage in data['stages']:
        if stage['name'] == 'copy':
            continue
        if not stage.get('started') or stage.get('rc') != 0:
            fails.append('阶段 %s 未启动或退出码非 0：%r' % (stage['name'], stage.get('rc')))
        if stage.get('infraOk') is not True:
            fails.append('阶段 %s 收口未证实：%r' % (stage['name'], stage.get('infraReasons')))
        out_info = stage.get('stdout') or {}
        if not out_info.get('path') or not os.path.isfile(out_info['path']):
            fails.append('阶段 %s 缺 stdout.raw 证据' % stage['name'])
        if out_info.get('sha256') is None:
            fails.append('阶段 %s 的 stdout 没有哈希' % stage['name'])
        if not stage.get('argv') or not stage.get('cwd'):
            fails.append('阶段 %s 缺 argv/cwd' % stage['name'])

    # 生成物与入口二进制真实存在（构建产物落在证据目录，不污染工程根）
    for rel in ('view/edit/edit_gen.go', 'view/summary/summary_gen.go'):
        if not os.path.isfile(os.path.join(out, rel)):
            fails.append('缺少生成物 %s' % rel)
    exe = os.path.join(out, '.verify-projects', 'preferences.exe')
    if not os.path.isfile(exe):
        fails.append('窗口入口没有被构建（缺 preferences.exe）')
    # 只构建、不运行：入口二进制绝不能是任何阶段的 argv[0]
    for stage in data['stages']:
        argv0 = (stage.get('argv') or [None])[0]
        if argv0 and str(argv0).endswith('preferences.exe'):
            fails.append('阶段 %s 执行了窗口入口（应只构建）' % stage['name'])

    test_stage = [s for s in data['stages'] if s['name'] == 'test'][0]
    markers = test_stage.get('markers') or {}
    if markers.get('pass', 0) < 1 or markers.get('skip') or markers.get('fail'):
        fails.append('离屏测试标记不符：%r' % (markers,))

    lock = data.get('lock', {})
    if not lock.get('acquired') or lock.get('released') is not True:
        fails.append('锁未按本轮取得并释放：%r' % {k: lock.get(k) for k in ('acquired', 'released')})
    fp = lock.get('tokenFingerprint') or ''
    if not fp.startswith('sha256:') or len(fp) != len('sha256:') + 64:
        fails.append('token 指纹格式不符：%r' % fp)
    if data.get('check', {}).get('forwarded') is not False:
        fails.append('默认流程不应记录 --check 转发：%r' % data.get('check'))
    if data.get('templateUntouched') is not True:
        fails.append('模板不在运行后保持字节不变')

    # 明文 token 不得出现在报告里（含证据目录内那份副本）
    hits = find_key(data, 'token')
    if hits:
        fails.append('报告里出现了明文 token 字段：%r' % hits)
    inner = os.path.join(out, '.verify-projects', 'report.json')
    if not os.path.isfile(inner):
        fails.append('证据目录内没有 report.json')
    else:
        with open(inner, encoding='utf-8') as fh:
            if find_key(json.load(fh), 'token'):
                fails.append('证据目录内的报告出现明文 token 字段')
    return fails


def build_shadow(src_repo, dest, mutate=None):
    """整份复制仓库（不含 .git）做「影子仓库」；`mutate` 只改被测脚本。

    为什么整份复制而不是拼最小材料：`build-cli` 阶段要真的编出 `cmd/goxml`，
    最小材料编不出来，那些边界就会以「CLI 没编出来」而不是以「我要测的那一步」
    失败——那样测到的就不是新脚本的职责了。仓库只有约 9.5 MB，复制成本可接受。
    """
    shutil.copytree(src_repo, dest, ignore=shutil.ignore_patterns('.git'),
                    ignore_dangling_symlinks=True)
    if mutate:
        script = os.path.join(dest, 'scripts', TOOL)
        for old, new in MUTATION_ANCHORS[mutate]:
            patch_file(script, old, new)
    return dest


def shadow_repo(ctx, name, mutate=None):
    return build_shadow(ctx.repo, ctx.path(name), mutate)


def patch_file(path, old, new, encoding='utf-8'):
    """按锚点替换；锚点必须**唯一命中**，否则直接失败（防止变异悄悄落空）。"""
    with open(path, 'rb') as fh:
        text = fh.read().decode(encoding)
    if text.count(old) != 1:
        raise AssertionError('锚点在 %s 命中 %d 次（应为 1）：%r'
                             % (path, text.count(old), old))
    with open(path, 'wb') as fh:
        fh.write(text.replace(old, new).encode(encoding))


def case_boundary_user_dir(ctx, tool):
    """用户已有目录（含自己的标记文件）：拒绝且一个字节都不动。"""
    out = ctx.path('我的已有目录')
    os.makedirs(os.path.join(out, 'sub'))
    marker = os.path.join(out, '.git')
    with open(marker, 'wb') as fh:
        fh.write(b'gitdir: elsewhere\n')
    with open(os.path.join(out, 'sub', 'mine.txt'), 'wb') as fh:
        fh.write('用户自己的文件\n'.encode('utf-8'))
    before = fingerprint(out)
    res = ctx.run_tool(out)
    fails = []
    if res['rc'] != 3:
        fails.append('用户已有目录应拒绝并退 3，实得 %d' % res['rc'])
    if fingerprint(out) != before:
        fails.append('拒绝后用户目录被改动：%r' % fingerprint(out))
    return fails


def case_boundary_stale_foreign_lock(ctx, tool):
    """别人的锁（含已死 pid）：退 8，且**不删不改**那把锁。"""
    out = ctx.path('外锁输出')
    lock_path = ctx.path('foreign.lock')
    payload = {
        'token': uuid.uuid4().hex,
        'agent': 'another-agent', 'task': 'someone-elses-work', 'runId': 'other-run',
        'hostPid': 999999,  # 不存在的 pid：即便被判「可疑陈旧」，也不该由我删
        'hostIdentity': {'pid': 999999, 'createFiletime': 1, 'identityStrength': 'pid+createFiletime'},
        'acquiredAt': '2026-01-01T00:00:00+0800', 'acquiredAtEpoch': 1.0,
    }
    with open(lock_path, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(payload, fh, ensure_ascii=False, indent=2)
    before = read_bytes(lock_path)
    res = ctx.run_tool(out, '--lock-path', lock_path, copy_only=False)
    fails = []
    if res['rc'] != 8:
        fails.append('他人锁应退 8，实得 %d（%r）' % (res['rc'], res['stderr'][-200:]))
    if read_bytes(lock_path) != before:
        fails.append('别人的锁被改写/删除（绝不能代替他人释放）')
    if os.path.lexists(out):
        fails.append('他人持锁时仍创建了输出目录')
    return fails


def case_boundary_missing_template(ctx, tool):
    shadow = shadow_repo(ctx, 'shadow-no-template')
    template = os.path.join(shadow, 'examples', 'gxml', 'testprojects', PROJECT)
    shutil.rmtree(template)
    out = ctx.path('缺模板输出')
    res = ctx.run_tool(out, '--repo', shadow, '--lock-path', LOCK, copy_only=False)
    fails = []
    if res['rc'] != 3:
        fails.append('缺模板应退 3，实得 %d（%r）' % (res['rc'], res['stderr'][-200:]))
    if os.path.lexists(out):
        fails.append('缺模板时仍创建了输出目录')
    return fails


def case_boundary_generate_fails(ctx, tool):
    shadow = shadow_repo(ctx, 'shadow-bad-gxml')
    gxml = os.path.join(shadow, 'examples', 'gxml', 'testprojects', PROJECT,
                        'view', 'edit', 'edit.gxml')
    patch_file(gxml, '{Status}', '{NoSuchSymbolXyz}')
    out = ctx.path('生成失败输出')
    report = ctx.path('generate-fail.json')
    res = ctx.run_tool(out, '--repo', shadow, '--lock-path', LOCK,
                       json_report=report, copy_only=False)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，无法验证生成失败边界']
    fails = []
    if res['rc'] != 4:
        fails.append('生成失败应退 4，实得 %d（%r）' % (res['rc'], res['stderr'][-300:]))
    if 'stderr' in res and 'code=4' not in res['stderr']:
        fails.append('失败标记里没有 code=4：%r' % res['stderr'][-200:])
    if 'VERIFY-PROJECTS-OK' in res['stdout']:
        fails.append('生成失败却打印了成功标记')
    if os.path.isfile(report):
        with open(report, encoding='utf-8') as fh:
            data = json.load(fh)
        names = [s['name'] for s in data['stages']]
        if 'build-entry' in names or 'test' in names:
            fails.append('生成失败后仍继续跑了后续阶段：%r' % (names,))
        if data.get('verdict', {}).get('code') != 4:
            fails.append('报告 verdict.code 不是 4：%r' % data.get('verdict'))
    else:
        fails.append('生成失败时没有落盘报告')
    return fails


def case_boundary_build_fails(ctx, tool):
    shadow = shadow_repo(ctx, 'shadow-bad-main')
    main_tpl = os.path.join(shadow, 'examples', 'gxml', 'testprojects', PROJECT,
                            'cmd', 'preferences', 'main.go.txt')
    with open(main_tpl, 'ab') as fh:
        fh.write('\nvar _ = noSuchSymbolXyz\n'.encode('utf-8'))
    out = ctx.path('构建失败输出')
    report = ctx.path('build-fail.json')
    res = ctx.run_tool(out, '--repo', shadow, '--lock-path', LOCK,
                       json_report=report, copy_only=False)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，无法验证构建失败边界']
    fails = []
    if res['rc'] != 4:
        fails.append('构建失败应退 4，实得 %d（%r）' % (res['rc'], res['stderr'][-300:]))
    if 'VERIFY-PROJECTS-OK' in res['stdout']:
        fails.append('构建失败却打印了成功标记')
    if os.path.isfile(report):
        with open(report, encoding='utf-8') as fh:
            data = json.load(fh)
        names = [s['name'] for s in data['stages']]
        if names[-1] != 'build-entry':
            fails.append('构建失败应在 build-entry 停住，实际阶段：%r' % (names,))
        if 'test' in names:
            fails.append('构建失败后仍跑了测试')
    else:
        fails.append('构建失败时没有落盘报告')
    return fails


def case_boundary_timeout(ctx, tool):
    """超时：退 5，且 rc 是强制值、不能当成被测程序的退出码。"""
    out = ctx.path('超时输出')
    report = ctx.path('timeout.json')
    res = ctx.run_tool(out, '--lock-path', LOCK, '--timeout', '0.4',
                       json_report=report, copy_only=False)
    if res['rc'] == 8:
        return ['SKIP: 公共锁被他人持有，无法验证超时边界']
    fails = []
    if res['rc'] != 5:
        fails.append('超时应退 5，实得 %d（%r）' % (res['rc'], res['stderr'][-300:]))
    if 'VERIFY-PROJECTS-OK' in res['stdout']:
        fails.append('超时却打印了成功标记')
    if os.path.isfile(report):
        with open(report, encoding='utf-8') as fh:
            data = json.load(fh)
        timed = [s for s in data['stages'] if s.get('timedOut')]
        if not timed:
            fails.append('报告里没有任何阶段标记超时')
        if data.get('verdict', {}).get('code') != 5:
            fails.append('报告 verdict.code 不是 5：%r' % data.get('verdict'))
    else:
        fails.append('超时时没有落盘报告')
    return fails


def case_review_check_status(ctx, tool):
    a = {'name': 'generate:a', 'started': True, 'rc': 0, 'infraOk': True,
         'checkReported': True}
    b = dict(a, name='generate:b')
    tests = [([], False, None), ([dict(a, started=False)], False, None),
             ([a, b], True, True), ([a, dict(b, checkReported=False)], True, None),
             ([dict(a, rc=2)], True, False), ([dict(a, infraOk=False)], True, False)]
    for stages, forwarded, passed in tests:
        status = tool.check_status(stages, True)
        if status['forwarded'] is not forwarded or status['passed'] is not passed:
            return ['check报告未区分转发/实际成功/失败：%r' % status]
    success = '提交前检查通过：1 个目标包'
    failure = 'goxml generate: 提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）'
    cases = [
        (success + '\r\n', '', (True, False)),
        ('', failure + '\nundefined: bad', (False, True)),
        ('已生成 ' + success + '.go', 'goxml generate: 缺少文件 提交前检查失败.gxml', (False, False)),
        ('', 'goxml generate: 解析失败\n' + failure, (False, False)),
    ]
    for stdout, stderr, expected in cases:
        actual = tool.check_markers(stdout.encode('utf-8'), stderr.encode('utf-8'))
        if actual != expected:
            return ['check 标记识别错误：%r，期望 %r' % (actual, expected)]
    if tool.check_status([a], False) != {'forwarded': False}:
        return ['未请求check却声称已转发']
    return []


def case_review_log_ownership(ctx, tool):
    logs = ctx.path('user-logs')
    os.mkdir(logs)
    with open(os.path.join(logs, 'report.json'), 'wb') as fh:
        fh.write(b'USER HISTORICAL REPORT')
    occupied = ctx.path('user-output')
    os.mkdir(occupied)
    before = fingerprint(logs)
    result = ctx.run_tool(occupied, '--log-dir', logs)
    return ([] if result['rc'] == 3 and fingerprint(logs) == before else
            ['拒绝输出目录却覆盖他人证据报告'])


def case_review_report_exclusive(ctx, tool):
    p = ctx.path('report.json')
    with open(p, 'wb') as fh:
        fh.write(b'USER REPORT')
    with open(p + '.tmp', 'wb') as fh:
        fh.write(b'USER TMP')
    try:
        tool.write_json(p, {'bad': True})
        return ['未拒绝覆盖他人报告']
    except FileExistsError:
        pass
    if read_bytes(p) != b'USER REPORT' or read_bytes(p + '.tmp') != b'USER TMP':
        return ['报告或同名tmp被改写']
    owned = {}
    own = ctx.path('own.json')
    tool.write_json(own, {'v': 1}, owned)
    tool.write_json(own, {'v': 2}, owned)
    if json.loads(read_bytes(own)) != {'v': 2}:
        return ['自有报告重复写入未工作']
    link = ctx.path('dangling-report.json')
    missing = ctx.path('does-not-exist.json')
    try:
        os.symlink(missing, link)
    except OSError:
        return ['SKIP: 宿主未提供符号链接创建能力']
    try:
        tool.write_json(link, {'bad': True})
        return ['悬空报告链接未拒绝']
    except (OSError, tool.Refused):
        pass
    if not os.path.islink(link) or os.path.exists(missing):
        return ['悬空报告链接被替换或目标被写入']
    return []


def case_review_report_structured(ctx, tool):
    target = ctx.path('report-target')
    os.mkdir(target)
    link = ctx.path('report-parent-link')
    try:
        os.symlink(target, link, target_is_directory=True)
    except OSError:
        return ['SKIP: 宿主未提供符号链接创建能力']
    out = ctx.path('own-output')
    result = ctx.run_tool(out, json_report=os.path.join(link, 'report.json'))
    if result['rc'] != 7 or 'Traceback' in result['stderr'] or os.listdir(target):
        return ['重解析报告路径未结构化拒绝或写穿：%r' % result]
    with open(os.path.join(out, '.verify-projects', 'report.json'), encoding='utf-8') as fh:
        report = json.load(fh)
    if report['verdict']['ok'] or report['verdict']['code'] != 7:
        return ['报告落盘失败后自有报告仍声称成功']
    occupied = ctx.path('existing-output')
    os.mkdir(occupied)
    result = ctx.run_tool(occupied, json_report=os.path.join(link, 'report2.json'))
    if result['rc'] != 3 or 'Traceback' in result['stderr'] or os.listdir(target):
        return ['报告失败吞掉原拒绝码或写穿报告路径']
    return []


def case_review_ancestor_symlink(ctx, tool):
    dest = ctx.path('target')
    os.mkdir(dest)
    link = ctx.path('parent-link')
    try:
        os.symlink(dest, link, target_is_directory=True)
    except OSError:
        return ['SKIP: 宿主未提供符号链接创建能力']
    result = ctx.run_tool(os.path.join(link, 'output'))
    if result['rc'] != 3 or os.listdir(dest):
        return ['父目录符号链接未被拒绝或被写穿']
    return []


CASES = [
    ('review-report-structured', case_review_report_structured),
    ('review-check-status', case_review_check_status),
    ('review-log-ownership', case_review_log_ownership),
    ('review-report-exclusive', case_review_report_exclusive),
    ('review-ancestor-symlink', case_review_ancestor_symlink),
    ('refuse-empty-dir', case_refuse_empty_dir),
    ('refuse-plain-file', case_refuse_plain_file),
    ('refuse-symlink', case_refuse_symlink),
    ('refuse-missing-parent', case_refuse_missing_parent),
    ('success-spaces-unicode', case_success_spaces_unicode),
    ('template-untouched', case_template_untouched),
    ('usage-errors', case_usage_errors),
    ('refusal-report', case_refusal_report),
    ('custom-log-dir', case_custom_log_dir),
    ('gomod-parses', case_gomod_parses),
    ('lock-busy-no-go', case_lock_busy_no_go),
    ('test-verdict-unit', case_test_verdict_unit),
    ('full-pipeline', case_full_pipeline),
    ('boundary-user-dir', case_boundary_user_dir),
    ('boundary-stale-foreign-lock', case_boundary_stale_foreign_lock),
    ('boundary-missing-template', case_boundary_missing_template),
    ('boundary-generate-fails', case_boundary_generate_fails),
    ('boundary-build-fails', case_boundary_build_fails),
    ('boundary-timeout', case_boundary_timeout),
]


def case_review_runner_report_exclusive(ctx, tool):
    path = ctx.path('foreign-report.json')
    with open(path, 'wb') as fh:
        fh.write(b'USER-REPORT')
    with open(path + '.tmp', 'wb') as fh:
        fh.write(b'USER-TMP')
    failures = []
    try:
        write_json(path, {'unexpected': True})
        failures.append('runner overwrote an existing report')
    except FileExistsError:
        pass
    if read_bytes(path) != b'USER-REPORT' or read_bytes(path + '.tmp') != b'USER-TMP':
        failures.append('runner changed foreign report or tmp bytes')
    return failures


CASES.append(('review-runner-report-exclusive', case_review_runner_report_exclusive))


def main(argv):
    parser = argparse.ArgumentParser(prog='test-verify-projects.py',
                                    description='verify-projects.py 专属测试（C1）')
    parser.add_argument('--work-root', default=None, help='用例工作根目录（不存在则创建；跑用例时必填）')
    parser.add_argument('--report', default=None, help='汇总报告 JSON 路径')
    parser.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    parser.add_argument('--case', action='append', default=None, help='只跑指定用例（可多次）')
    parser.add_argument('--make-mutated-repo', default=None,
                        help='在指定路径生成「变异后的影子仓库」并退出（不跑用例）')
    parser.add_argument('--mutate', default=None, help='变异名：%s' % ', '.join(sorted(MUTATION_ANCHORS)))
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
    if not os.path.isdir(args.work_root):
        os.makedirs(args.work_root)
    tool = load_tool_module(repo)

    wanted = args.case or [name for name, _ in CASES]
    report = {'schema': 'r11c-test-verify-projects-1', 'repo': repo,
              'startedAt': now(), 'cases': [], 'status': None}
    failures = 0
    skipped = 0
    for name, func in CASES:
        if name not in wanted:
            continue
        ctx = Ctx(name, args.work_root, repo)
        entry = {'case': name, 'status': 'passed', 'failures': [], 'workDir': ctx.dir}
        try:
            problems = func(ctx, tool)
        except Exception as exc:  # 用例自身炸了也要记成失败，不能静默通过
            problems = ['%s: %s' % (type(exc).__name__, exc)]
        real = [p for p in problems if not p.startswith('SKIP:')]
        skips = [p for p in problems if p.startswith('SKIP:')]
        if real:
            entry['status'] = 'failed'
            entry['failures'] = real
            failures += 1
        elif skips:
            entry['status'] = 'skipped'
            entry['note'] = skips[0]
            skipped += 1
        report['cases'].append(entry)
        print('%-24s %-8s %s' % (name, entry['status'],
                                 entry['failures'][0] if real else entry.get('note', '')))

    report['endedAt'] = now()
    report['failures'] = failures
    report['skipped'] = skipped
    report['status'] = 'PASS' if failures == 0 else 'FAIL'
    if args.report:
        write_json(args.report, report)
    print(json.dumps({'status': report['status'], 'failed': failures, 'skipped': skipped,
                      'report': args.report}, ensure_ascii=False))
    # 有跳过但无失败：判 PASS 但明确写出跳过数，避免「全绿」被误读成全覆盖。
    return 0 if failures == 0 else 1


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))

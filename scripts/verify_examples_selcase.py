# -*- coding: utf-8 -*-
r"""R6-C 选择器里的**单 case 执行**：raw 完整落盘 + 进程树收口 + 基础设施判据。

两个选择器（workbench / forms）原先各写一份 `subprocess.run(..., stdout=PIPE)` +
`outTail=text[-1200:]`，失败后只剩 1200 字符，无法复核。这里把这段抽成一份实现：

  * 每个 case 一个**独占目录**（`<log_dir>/<case_id>`，重复则 `-2`/`-3`），绝不覆盖历史；
  * stdout/stderr 原始字节直接写文件（继承句柄，不经管道），保留 CRLF/NUL/非 UTF-8/
    超时部分输出；SHA-256 对落盘字节算；
  * 解码只用于 `outTail`（人读），不改写 raw；
  * 能力不足**启动前拒绝**（`containment='win-job'` 时不支持就 refused，进程数 0）；
  * 基础设施失败（fail-closed / 清理失败 / 活动数未知 / 句柄关闭失败 / 超时）一律
    `infraOk=False`，由上层把该 case 判 FAIL 并给非零退出——不硬编码成功。

判据分工：本模块只回答"这次执行是否可信"；业务判据（rc、目录清单、生成物大小）
仍由各选择器自己写，语义与 W1-W5 / F1-F5 保持一致。
"""
import importlib.util
import json
import os
import shutil
import tempfile

RUNNER_FORMAT = 'r6c-selcase-1'


def _load_from(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def load_proc(repo):
    """只用**被测树**里的捕获层，避免"测试的是另一份实现"。"""
    return _load_from(os.path.join(repo, 'scripts', 'verify_examples_proc.py'),
                      'verify_examples_proc_for_selector')


class Runner(object):
    def __init__(self, repo, ps, script, log_dir, python_exe, go_exe,
                 containment='win-job', keep_output=False, case_timeout=1800.0, fault=None):
        self.repo = os.path.abspath(repo)
        self.ps = ps
        self.script = script
        self.log_dir = os.path.abspath(log_dir)
        self.python_exe = python_exe
        self.go_exe = go_exe
        self.containment = containment
        self.keep_output = keep_output
        self.case_timeout = case_timeout
        # 仅供"上层是否真的消费故障"的验证；正常门禁必须传 None。
        self.fault = fault or None
        self.proc = load_proc(self.repo)
        os.makedirs(self.log_dir, exist_ok=True)

    def build_argv(self, out_root, extra):
        return [self.ps, '-NoProfile', '-File', self.script,
                '-RepoRoot', self.repo, '-OutputRoot', out_root, '-SkipLock',
                '-PythonExe', self.python_exe, '-GoExe', self.go_exe] + list(extra)

    def run_case(self, cid, extra, timeout=None):
        # 输出根必须**不存在**：脚本对已有目录（含空目录）一律 rc 5。
        base = tempfile.mkdtemp(prefix='goxml-sel-' + cid + '-')
        out_root = os.path.join(base, cid)
        argv = self.build_argv(out_root, extra)
        res = self.proc.run_capture(argv, cid, self.log_dir, cwd=self.repo,
                                    timeout=self.case_timeout if timeout is None else timeout,
                                    containment=self.containment, fault=self.fault)

        kids = sorted(os.listdir(out_root)) if os.path.isdir(out_root) else []
        # 示例子目录：排除脚本自己的 bin/ 与根层的 marker/日志/入口产物。
        exdirs = sorted(d for d in kids
                        if os.path.isdir(os.path.join(out_root, d)) and d != 'bin')
        # 产物名不按目录名推导：starter 的页面是 home.gxml（生成 home_gen.go），
        # 窗口入口则是根层的 <示例>-entry.exe。
        entry = {}
        for name in exdirs:
            pth = os.path.join(out_root, name)
            gens = ([f for f in os.listdir(pth) if f.endswith('_gen.go')]
                    if os.path.isdir(pth) else [])
            gen = os.path.join(pth, gens[0]) if gens else ''
            exe = os.path.join(out_root, name + '-entry.exe')
            entry[name] = {
                'genFiles': gens,
                'genBytes': os.path.getsize(gen) if gen and os.path.isfile(gen) else 0,
                'entryExists': os.path.isfile(exe),
                'entryBytes': os.path.getsize(exe) if os.path.isfile(exe) else 0,
            }

        row = {
            'format': RUNNER_FORMAT,
            'id': cid,
            'argv': argv,
            'rc': res['rc'],
            'timedOut': res['timedOut'],
            'started': res['started'],
            'pid': res['pid'],
            'elapsedSec': res['elapsedSec'],
            'startedUtc': res['startedUtc'],
            'endedUtc': res['endedUtc'],
            'containment': res['containment'],
            'containmentReason': res.get('containmentReason'),
            'refused': res.get('refused'),
            'refusalReason': res.get('refusalReason'),
            'infraOk': res['ok'],
            'infraReasons': res['okReasons'],
            'cleanupError': res['cleanupError'],
            'activeAfter': res['activeAfter'],
            'activeKnown': res.get('activeKnown'),
            'closeErrors': res['closeErrors'],
            'stdoutEncoding': (res['displayTail'] or {}).get('encoding'),
            'raw': {'stdout': res['stdout'], 'stderr': res['stderr']},
            'caseDir': res['caseDir'],
            'resultJson': res['resultPath'],
            'outputRootEntries': kids,
            'exampleDirs': exdirs,
            'perExample': entry,
            'outTail': (res['displayTail'] or {}).get('tail', ''),
        }

        if self.keep_output:
            row['outputRoot'] = out_root
            row['outputRootRetained'] = True
        else:
            row['outputRootRetained'] = False
            shutil.rmtree(base, ignore_errors=True)  # 只删本次自建的构建输出目录
        return row


def case_passed(row, business):
    """单个 case 的最终判据 = 基础设施可信 **且** 业务条件成立。

    两件事必须分开：`business` 由各选择器自己写（rc、目录清单、产物大小），
    `infraOk` 只回答"这次执行是否可信"。缺字段一律**不放行**（`is True` 严格判定）。
    """
    return bool(row.get('infraOk') is True and business is True)


def summarize(rows):
    """给选择器用的简短汇总（不参与判据）。"""
    return json.dumps([{'id': r['id'], 'rc': r['rc'], 'infraOk': r['infraOk'],
                        'bytes': r['raw']['stdout']['bytes']} for r in rows],
                      ensure_ascii=False)

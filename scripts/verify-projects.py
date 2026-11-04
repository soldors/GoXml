#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""把 testprojects 里的工程模板变成可构建工程：一条命令完成复制、占位符替换、
真实生成、构建、可选离屏测试。

它替代的是教程里那串手动步骤（find 改名、sed 填占位符、一次目录 generate、go build），
**不是** `goxml init`/`watch`，也不新增业务工程：模板、教程、测试仍是同一份材料
（`examples/gxml/testprojects/<project>/`），这里只是把「照着做」变成「跑一次」。

用法::

    # 默认：复制 + 占位符 + 真实生成 + 只构建窗口入口 + 可选离屏测试
    python scripts/verify-projects.py crosspackage-preferences \
        --out-dir "D:/work/我的偏好工程" \
        --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"

    # 只复制+替换占位符，不碰 Go（排障、离线、CI 前置检查用）
    python scripts/verify-projects.py crosspackage-preferences \
        --out-dir "D:/work/我的偏好工程" --copy-only

    # 全流程但跳过离屏测试（只生成+构建）
    python scripts/verify-projects.py crosspackage-preferences \
        --out-dir "D:/work/我的偏好工程" --lock-path <lock> --skip-tests

公共锁必须**显式**传入（全流程会启动 Go）：他人持锁时一个 Go 进程都不启动，
直接以 8 退出；锁只按本轮自己的 token 释放，绝不按 mtime 猜。

退出码（业务判定与收口判定分开，详见 `--json` 报告）::

    0  全部阶段成功且锁已释放
    2  用法错误（未知项目、缺参数、模块路径非法）
    3  拒绝：输出目录不是本轮独占创建 / 模板缺失 / 模板被改动
    4  业务失败：某个阶段退出码非 0
    5  业务失败：某个阶段超时
    6  收口失败：子进程树未能确认收口（复用 run_capture 的 ok=False）
    7  记录不完整：阶段结果缺字段 / 无法落盘
    8  锁忙：他人持锁，**一个 Go 进程都没启动**
    9  阶段全部成功但锁未按自有 token 释放

设计约束（R11-C）：

* 只依赖标准库；进程收口与 raw 捕获复用 `scripts/verify_examples_proc.py`，
  锁复用 `scripts/verify-examples-lock.py`，**不复制** Job/锁/台账实现。
* 公网/系统无关：不安装、不联网取依赖、不开窗口、不跑采样。
* 公共锁显式传入并按 token 释放；锁忙时**不启动任何 Go 进程**，也不删他人锁。
* 台账/报告里只出现 token 的 sha256 指纹，不出现明文 token。
* 输出目录必须本轮独占创建：任何已存在路径（目录、文件、符号链接、重解析点）
  一律拒绝且零新增；模板源码永不被写。
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
import os
import re
import sys
import stat

SCHEMA = 'r11c-verify-projects-1'

EXIT_OK = 0
EXIT_USAGE = 2
EXIT_REFUSED = 3
EXIT_BUSINESS = 4
EXIT_TIMEOUT = 5
EXIT_CONTAINMENT = 6
EXIT_RECORD = 7
EXIT_LOCK_BUSY = 8
EXIT_LOCK_NOT_RELEASED = 9

# 项目白名单：只列 testprojects 下**本工具负责**的工程模板。
# 它刻意与 `examples/gxml/testdata` 的五个 `-Example` 注册项无关：
# 那里是示例选择器，这里是「可复制出去的完整工程」。
PROJECTS = {
    'crosspackage-preferences': {
        # 生成的入口包（相对工程根）。只 build，不运行。
        'entry': 'cmd/preferences',
        # 离屏测试目标（相对工程根；只跑这些包，不跑未过滤的全仓）。
        'testTargets': ['./view/...'],
        # 离屏测试的用例前缀（模板自带，与教程同源）。
        'testRun': '^TestCross',
        'completionMarker': 'CROSS-DONE',
        'expectedTests': [
            'TestCrossEditDraftKeepsSaved', 'TestCrossEditMaxLenCountsRunes',
            'TestCrossEditPortFilterDropsNonDigits', 'TestCrossEditConfirmWritesSaved',
            'TestCrossEditConfirmRejectedKeepsSaved', 'TestCrossEditResetRestoresSaved',
            'TestCrossEditReturnPayloadOnly', 'TestCrossEditStatusBranch',
            'TestCrossEditInstancesIsolated', 'TestCrossSummaryFollowsSharedModel',
            'TestCrossSummaryInstancesIsolated',
        ],
    },
    # R15-C：For + 稳定 Key 的任务板。与 preferences 共用同一套复制/生成/构建/离屏流水线，
    # 但用例名与完成标记独立，避免把另一工程的用例集合当成本工程的判据。
    'crosspackage-taskboard': {
        'entry': 'cmd/taskboard',
        'testTargets': ['./view/...'],
        'testRun': '^TestTaskboard',
        'completionMarker': 'TASKBOARD-DONE',
        'expectedTests': [
            'TestTaskboardListReorderHitsCurrentRow',
            'TestTaskboardListDeleteKeepsIdentity',
            'TestTaskboardListSummaryFollowsSharedModel',
            'TestTaskboardListInstancesIsolated',
            'TestTaskboardSummaryRendersCounts',
            'TestTaskboardSummaryInstancesIsolated',
        ],
    },
}

PLACEHOLDER_MODULE = '__MODULE__'
PLACEHOLDER_REPO = '__GOXML_DIR__'
# 只有这些后缀参与占位符替换：README 里提到占位符是**文档**，不该被改写。
SUBST_SUFFIXES = ('.go', '.mod')

MODULE_RE = re.compile(r'^[A-Za-z0-9][A-Za-z0-9._~/-]*$')

# 本机 Go 工具链（不读 PATH，避免命中别的 go）。可用 --go 覆盖。
GO_DEFAULT = r'C:\Program Files\Go\bin\go.exe'


class Refused(Exception):
    """带退出码的拒绝：调用方不再继续任何有副作用的动作。"""

    def __init__(self, code, message, **detail):
        Exception.__init__(self, message)
        self.code = code
        self.message = message
        self.detail = detail


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds')


def sha256_bytes(data):
    return hashlib.sha256(data).hexdigest()


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def token_fingerprint(token):
    """台账只留指纹：R9-C 的教训——公开记录里不允许出现明文 token。"""
    if not token:
        return None
    return 'sha256:' + hashlib.sha256(token.encode('utf-8')).hexdigest()


def go_quote(path):
    r"""把**原生**绝对路径写成合法 Go 字符串字面量（含两侧引号）。

    go.mod 的 `replace ... => "..."` 用 Go 字面量语法：Windows 的反斜杠在那里是
    转义符，把 `C:\Users` 原样拼进去会得到坏字面量（`\U` 不是合法转义）。这里
    不把反斜杠折成正斜杠（那正是手工步骤要人做的事，本工具的存在意义是免掉它），
    而是按 Go 规则**转义**：`\` → `\\`。

    实测（`go mod edit -json`）：`=> "C:\\Users\\admin\\..."` 解析后还原成
    `C:\Users\admin\...`，rc=0。JSON 的引号/反斜杠转义与 Go 的字符串字面量规则
    在「不含控制字符的路径」上一致，非 ASCII 字符两端都原样保留（`ensure_ascii=False`）。
    """
    return json.dumps(str(path), ensure_ascii=False)


def substitute(data, module, repo):
    """替换 `__MODULE__` 与带引号的 `__GOXML_DIR__`，**跳过整行注释**。

    模板里的注释本身在讲「这个占位符是什么」（例如 main.go 顶部
    `// 并把 __MODULE__ 换成 go.mod 里的模块路径。`），替换它会把说明改成一句
    自相矛盾的话。注释是给读模板的人看的历史说明，不参与本轮替换；
    真正的代码/指令行（import、module、replace）才替换。

    按字节逐行处理，保持原有行尾（不经文本模式，避免 LF→CRLF）。
    """
    sub_module = PLACEHOLDER_MODULE.encode('utf-8')
    sub_repo = ('"%s"' % PLACEHOLDER_REPO).encode('utf-8')
    new_module = module.encode('utf-8')
    new_repo = go_quote(repo).encode('utf-8')
    out = []
    for line in data.split(b'\n'):
        if line.lstrip().startswith(b'//'):
            out.append(line)
            continue
        out.append(line.replace(sub_module, new_module).replace(sub_repo, new_repo))
    return b'\n'.join(out)


def load_helper(repo, filename, module_name):
    """从 `<repo>/scripts/` 载入随仓助手（收口/锁），不复制它们的实现。

    路径按 **repo** 解析而不是按本文件位置：变异测试把本脚本复制到别处运行，
    助手仍然来自真实仓库，这样变异只影响被测逻辑。
    """
    path = os.path.join(repo, 'scripts', filename)
    if not os.path.isfile(path):
        raise Refused(EXIT_REFUSED, '仓库缺少助手脚本 %s' % path)
    spec = importlib.util.spec_from_file_location(module_name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def default_repo():
    """本脚本在 <repo>/scripts/ 下，据此推导仓库根。"""
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


# ---------------------------------------------------------------- 输出目录所有权


def existing_kind(path):
    """已存在路径的类型（用于拒绝消息与证据）。不存在返回 None。

    用 `lexists` 而不是 `exists`：悬空符号链接也要算「已存在」，
    否则会在一个指向别处的链接后面写文件。
    """
    if not os.path.lexists(path):
        return None
    if os.path.islink(path):
        return 'symlink'
    if os.path.isdir(path):
        return 'dir'
    return 'file'


def claim_output_dir(path):
    """**本轮独占创建**输出目录；已存在就拒绝，且保证零新增。

    不用 `os.makedirs(exist_ok=True)`：那条路会把「别人的目录」当成自己的继续写。
    这里只允许单层 `mkdir`：父目录必须已存在（不替调用方递归造路径），
    目录已存在（含空目录、他人内容、symlink/junction 等重解析点）一律拒绝。
    """
    ensure_no_reparse(path)
    kind = existing_kind(path)
    if kind is not None:
        raise Refused(EXIT_REFUSED,
                      '输出目录已存在（%s），本工具只写本轮独占创建的新目录：%s' % (kind, path),
                      outDir=path, existingKind=kind)
    parent = os.path.dirname(os.path.abspath(path))
    if not os.path.isdir(parent):
        raise Refused(EXIT_REFUSED, '输出目录的父目录不存在（不替你递归创建）：%s' % parent,
                      outDir=path, missingParent=parent)
    try:
        os.mkdir(path)
    except FileExistsError:
        raise Refused(EXIT_REFUSED, '输出目录在创建瞬间被他人占用：%s' % path,
                      outDir=path, existingKind=existing_kind(path))
    except OSError as exc:
        raise Refused(EXIT_REFUSED, '无法创建输出目录 %s：%s' % (path, exc), outDir=path)
    return path


def ensure_no_reparse(path):
    """Reject redirection in the target and its existing ancestry."""
    current = os.path.abspath(path)
    while True:
        if os.path.lexists(current):
            info = os.lstat(current)
            if stat.S_ISLNK(info.st_mode) or getattr(info, 'st_file_attributes', 0) & 0x400:
                raise Refused(EXIT_REFUSED, '路径含符号链接/重解析点：%s' % current)
        parent = os.path.dirname(current)
        if parent == current:
            break
        current = parent


# ---------------------------------------------------------------- 模板与复制


def template_dir(repo, project):
    return os.path.join(repo, 'examples', 'gxml', 'testprojects', project)


def inventory(root, skip_dirs=()):
    """列目录下所有文件的 (相对路径, 字节数, sha256)；跳过 skip_dirs（相对路径）。"""
    out = []
    skip = {os.path.normcase(os.path.normpath(os.path.join(root, s))) for s in skip_dirs}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames
                             if os.path.normcase(os.path.normpath(os.path.join(dirpath, d))) not in skip)
        for name in sorted(filenames):
            full = os.path.join(dirpath, name)
            rel = os.path.relpath(full, root).replace('\\', '/')
            out.append({'path': rel, 'bytes': os.path.getsize(full), 'sha256': sha256_file(full)})
    return out


def template_fingerprint(repo, project):
    return inventory(template_dir(repo, project))


def copy_template(template, out_dir, module, repo):
    """把模板复制成工程：`*.go.txt` → `*.go`、`go.mod.txt` → `go.mod`、
    替换两个占位符、补 `go.sum`。

    逐文件字节级读写：不做行尾归一化（`.gitattributes` 是 `eol=lf`，文本模式会
    在 Windows 上把 LF 变成 CRLF，进而让 gofmt/字节比对失真）。
    """
    if not os.path.isdir(template):
        raise Refused(EXIT_REFUSED, '模板目录不存在：%s' % template, template=template)
    written = []
    replaced = []
    for dirpath, dirnames, filenames in os.walk(template):
        dirnames.sort()
        rel_dir = os.path.relpath(dirpath, template)
        target_dir = out_dir if rel_dir == '.' else os.path.join(out_dir, rel_dir)
        if not os.path.isdir(target_dir):
            os.makedirs(target_dir)
        for name in sorted(filenames):
            src = os.path.join(dirpath, name)
            dst_name = name
            if name.endswith('.go.txt'):
                dst_name = name[:-len('.txt')]
            elif name == 'go.mod.txt':
                dst_name = 'go.mod'
            dst = os.path.join(target_dir, dst_name)
            with open(src, 'rb') as fh:
                data = fh.read()
            if dst_name.endswith(SUBST_SUFFIXES):
                original = data
                data = substitute(data, module, repo)
                if data != original:
                    replaced.append(os.path.relpath(dst, out_dir).replace('\\', '/'))
            with open(dst, 'wb') as fh:
                fh.write(data)
            written.append(os.path.relpath(dst, out_dir).replace('\\', '/'))
    # go.sum 来自仓库根：模板不携带第三方校验和，避免它随依赖升级漂移。
    src_sum = os.path.join(repo, 'go.sum')
    if os.path.isfile(src_sum):
        with open(src_sum, 'rb') as fh:
            sum_bytes = fh.read()
        with open(os.path.join(out_dir, 'go.sum'), 'wb') as fh:
            fh.write(sum_bytes)
        written.append('go.sum')
    if not written:
        raise Refused(EXIT_REFUSED, '模板目录为空：%s' % template, template=template)
    return {'written': written, 'substituted': replaced}


def discover_gxml(out_dir):
    """工程里所有 .gxml（排序后生成，顺序稳定便于复核）。"""
    found = []
    for dirpath, dirnames, filenames in os.walk(out_dir):
        dirnames.sort()
        for name in sorted(filenames):
            if name.lower().endswith('.gxml'):
                found.append(os.path.relpath(os.path.join(dirpath, name), out_dir).replace('\\', '/'))
    return sorted(found)


def common_parent_dir(rels):
    """本批 .gxml 的共同父目录（项目相对路径，`/` 分隔）；根目录返回 `.`。

    目录模式的输出映射是 `outRoot + dirname(rel 相对发现根)`（CLI 的
    `resolveInput`），所以要让目录模式的落点与「逐文件 `--out-dir dirname(file)`」
    完全一致，必须让 **发现根 = 输出根 = 共同父目录**：

        generate view --out-dir view     # rel=edit/edit.gxml → view/edit/edit_gen.go
        generate view/edit/edit.gxml --out-dir view/edit   # 同一个文件

    反例（错用工程根做输出根）：`generate view --out-dir .` 会把产物写到
    `./edit/edit_gen.go`——路径对不上模板，Go 包也不在原来那个包里。
    """
    parts = [rel.split('/')[:-1] for rel in rels]
    if not parts:
        return '.'
    common = []
    for layer in zip(*parts):
        if len(set(layer)) != 1:
            break
        common.append(layer[0])
    return '/'.join(common) if common else '.'


# ---------------------------------------------------------------- 阶段执行


def stage_case_id(name):
    """阶段名 → 目录名用的安全 id（`generate:view/edit/edit.gxml` 这类名字不能直接当目录）。"""
    return re.sub(r'[^0-9A-Za-z._-]+', '-', name).strip('-')


def run_stage(report, procmod, name, argv, cwd, log_dir, timeout, env_overrides):
    """跑一个真实阶段并**按类别**判定；返回 stage 记录。

    收口（进程树是否真的没了）与业务（被测命令自己的退出码）分开判：
      * 超时 → 5；进程树收口未证实 → 6；退出码非 0 → 4。
    `run_capture` 的 `ok` **只**表示基础设施可信（contained/activeAfter/closeErrors
    逐个校验），不表示业务成功——所以这里先看 ok，再看 rc，两者都留证据。
    """
    res = procmod.run_capture(argv, stage_case_id(name), log_dir, cwd=cwd,
                              timeout=timeout, env_overrides=env_overrides,
                              containment='auto')
    stage = {
        'name': name, 'kind': 'subprocess', 'argv': argv, 'cwd': res['cwd'],
        'started': res['started'], 'rc': res['rc'], 'rcSource': res['rcSource'],
        'timedOut': res['timedOut'], 'containment': res['containment'],
        'contained': res['contained'], 'activeAfter': res['activeAfter'],
        'infraOk': res['ok'], 'infraReasons': res['okReasons'],
        'elapsedSec': res['elapsedSec'], 'caseDir': res['caseDir'],
        'stdout': res['stdout'], 'stderr': res['stderr'], 'note': None,
    }
    report['stages'].append(stage)
    if res['timedOut']:
        raise Refused(EXIT_TIMEOUT, '阶段 %s 超时（%.0fs），rc 是强制值不是被测程序退出码'
                      % (name, timeout), stage=name)
    if not res['ok']:
        raise Refused(EXIT_CONTAINMENT, '阶段 %s 进程树收口未证实：%s'
                      % (name, ','.join(res['okReasons'] or [])), stage=name)
    if res['rc'] != 0:
        raise Refused(EXIT_BUSINESS, '阶段 %s 退出码 %s' % (name, res['rc']), stage=name)
    return stage


def read_raw(info):
    """读 run_capture 留下的原始字节（证据是文件，不是内存里的字符串）。"""
    if not info or not info.get('path') or not os.path.isfile(info['path']):
        return b''
    with open(info['path'], 'rb') as fh:
        return fh.read()


def test_markers(raw, completion_marker='CROSS-DONE'):
    """统计 `go test -v` 的用例标记，作为「真的跑了、没被 SKIP 兜底」的判据。

    completion_marker 由工程规格给出：preferences 用 CROSS-DONE，taskboard 用
    TASKBOARD-DONE。默认保持旧口径，既有调用方不必改。
    """
    text = raw.decode('utf-8', 'replace')
    done_re = re.compile(r': ' + re.escape(completion_marker) + r' (Test\w+)$')
    counts = {'pass': 0, 'skip': 0, 'fail': 0, 'noTestFiles': 0,
              'completed': [], 'passedNames': []}
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith('--- PASS:'):
            counts['pass'] += 1
            counts['passedNames'].append(stripped.split()[2])
        elif stripped.startswith('--- SKIP:'):
            counts['skip'] += 1
        elif stripped.startswith('--- FAIL:'):
            counts['fail'] += 1
        elif stripped.startswith('?') and 'no test files' in stripped:
            counts['noTestFiles'] += 1
        match = done_re.search(stripped)
        if match:
            counts['completed'].append(match.group(1))
    return counts


# ---------------------------------------------------------------- 报告


def write_json(path, obj, owned=None):
    """Exclusively create new reports; only update this invocation's own file."""
    if owned is None:
        owned = {}
    path = os.path.abspath(path)
    ensure_no_reparse(path)
    parent = os.path.dirname(os.path.abspath(path))
    if not os.path.isdir(parent):
        raise OSError('report parent does not exist: ' + parent)
    flags = os.O_WRONLY | getattr(os, 'O_BINARY', 0)
    if path not in owned:
        flags |= os.O_CREAT | os.O_EXCL
    fd = os.open(path, flags, 0o600)
    try:
        info = os.fstat(fd)
        identity = (info.st_dev, info.st_ino)
        if path in owned and owned[path] != identity:
            raise OSError('report ownership changed: ' + path)
        owned[path] = identity
        os.ftruncate(fd, 0)
        data = (json.dumps(obj, ensure_ascii=False, indent=2, sort_keys=True) + '\n').encode('utf-8')
        with os.fdopen(fd, 'wb') as fh:
            fd = None
            fh.write(data)
            fh.flush()
    finally:
        if fd is not None:
            os.close(fd)
    return path


def new_run_id(project):
    stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
    return 'vp-%s-%s-%s' % (project, stamp, os.urandom(3).hex())


# ---------------------------------------------------------------- 参数


class Parser(argparse.ArgumentParser):
    """argparse 默认 `error()` 退 2，与我们的 EXIT_USAGE 一致；但把用法打到 stderr 时
    不打印 traceback。这里只是显式化，避免以后有人改成 sys.exit(1)。"""

    def error(self, message):
        self.print_usage(sys.stderr)
        sys.stderr.write('%s: error: %s\n' % (self.prog, message))
        raise SystemExit(EXIT_USAGE)


def build_parser():
    p = Parser(prog='verify-projects.py', description=__doc__.split('\n')[0],
               formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('project', help='工程模板名，可取：%s' % ', '.join(sorted(PROJECTS)))
    p.add_argument('--out-dir', required=True, help='输出目录：必须不存在，由本轮独占创建')
    p.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    p.add_argument('--module', default=None, help='工程的 Go 模块路径（默认 example.com/<project>）')
    p.add_argument('--copy-only', action='store_true', help='只复制并替换占位符，不跑任何 Go 阶段')
    p.add_argument('--lock-path', default=None,
                   help='公共锁路径（全流程必填；copy-only 不用锁）')
    p.add_argument('--lock-agent', default='workbuddy')
    p.add_argument('--lock-task', default='verify-projects')
    p.add_argument('--go', default=GO_DEFAULT, help='Go 工具链路径（默认 %s）' % GO_DEFAULT)
    p.add_argument('--timeout', type=float, default=1800.0, help='单个阶段超时秒数（默认 1800）')
    p.add_argument('--skip-tests', action='store_true', help='全流程但跳过离屏 go test')
    p.add_argument('--check', action='store_true',
                   help='把 --check 原样转发给 generate（只记「已转发」，不宣称新检查通过）')
    p.add_argument('--json', default=None, help='把本轮报告写到这个 JSON 路径（已存在则拒绝）')
    p.add_argument('--log-dir', default=None, help='证据目录（默认 <out-dir>/.verify-projects）')
    p.add_argument('--run-id', default=None, help='本轮标识（默认自动生成，写入报告与完成标记）')
    p.add_argument('--quiet', action='store_true', help='只打印最终结论与完成标记')
    return p


def main(argv):
    parser = build_parser()
    args = parser.parse_args(argv)

    project = args.project
    if project not in PROJECTS:
        sys.stderr.write('未知工程 %r；可用：%s\n' % (project, ', '.join(sorted(PROJECTS))))
        return EXIT_USAGE

    repo = os.path.abspath(args.repo) if args.repo else default_repo()
    if not os.path.isfile(os.path.join(repo, 'go.mod')):
        sys.stderr.write('不是 Go 模块仓库（缺 go.mod）：%s\n' % repo)
        return EXIT_USAGE

    module = args.module or ('example.com/' + project)
    if not MODULE_RE.match(module):
        sys.stderr.write('模块路径不合法：%r\n' % module)
        return EXIT_USAGE

    out_dir = os.path.abspath(args.out_dir)
    if args.json and os.path.lexists(args.json):
        sys.stderr.write('报告路径已存在（不覆盖历史证据）：%s\n' % args.json)
        return EXIT_USAGE
    # 全流程会启动 Go：公共锁必须由调用方**显式**给出，不给就拒绝开跑。
    if not args.copy_only and not args.lock_path:
        sys.stderr.write('全流程必须显式给 --lock-path（公共锁）；只复制请用 --copy-only\n')
        return EXIT_USAGE
    if args.check and args.copy_only:
        sys.stderr.write('--check 只对 generate 有意义，--copy-only 下不适用\n')
        return EXIT_USAGE
    run_id = args.run_id or new_run_id(project)

    report = {
        'schema': SCHEMA,
        'runId': run_id,
        'repo': repo,
        'project': project,
        'module': module,
        'outDir': out_dir,
        'mode': 'copy-only' if args.copy_only else 'full',
        'check': check_status([], args.check),
        'lock': {'path': None, 'tokenFingerprint': None, 'acquired': False,
                 'released': None, 'busy': False},
        'stages': [],
        'files': [],
        'templateUntouched': None,
        'startedUtc': utc(),
        'endedUtc': None,
        'elapsedSec': None,
        'verdict': {'ok': False, 'code': None, 'reason': None},
    }

    started = datetime.datetime.now(datetime.timezone.utc)
    owned_reports = {}
    try:
        code = run(args, report, repo, project, module, out_dir, run_id)
    except Refused as exc:
        report['verdict'].update({'ok': False, 'code': exc.code, 'reason': exc.message})
        if exc.detail:
            report['verdict']['detail'] = exc.detail
        code = exc.code
    except Exception as exc:  # 未预料的异常也要留证据，不能静默成功
        report['verdict'].update({'ok': False, 'code': EXIT_RECORD,
                                  'reason': '%s: %s' % (type(exc).__name__, exc)})
        code = EXIT_RECORD
    finally:
        report['check'] = check_status(report['stages'], args.check)
        report['endedUtc'] = utc()
        report['elapsedSec'] = round(
            (datetime.datetime.now(datetime.timezone.utc) - started).total_seconds(), 3)
        if code == EXIT_OK:
            report['verdict'].update({'ok': True, 'code': 0, 'reason': None})
        if args.json:
            try:
                write_json(args.json, report, owned_reports)
            except (OSError, Refused) as exc:
                sys.stderr.write('报告写入失败：%s\n' % exc)
                if code == EXIT_OK:
                    code = EXIT_RECORD
                    report['verdict'].update(ok=False, code=code, reason='报告写入失败：%s' % exc)
        # 证据跟着工程走：只要证据目录已经建出来，就再落一份同样的报告，
        # 免得「报告在调用方仓库、原始输出在工程里」两处对不上。
        log_dir = report.get('logDir')
        if report.get('logDirOwned') is True:
            try:
                write_json(os.path.join(log_dir, 'report.json'), report, owned_reports)
            except (OSError, Refused) as exc:
                sys.stderr.write('证据报告写入失败：%s\n' % exc)
                if code == EXIT_OK:
                    code = EXIT_RECORD
                    report['verdict'].update(ok=False, code=code, reason='证据报告写入失败：%s' % exc)
        # Keep any already published owned copy honest if another report failed.
        if code != EXIT_OK and owned_reports:
            for published in list(owned_reports):
                try:
                    write_json(published, report, owned_reports)
                except (OSError, Refused):
                    pass

    if code == EXIT_OK:
        if not args.quiet:
            summarise(report)
        sys.stdout.write('VERIFY-PROJECTS-OK %s %s\n' % (run_id, project))
        sys.stdout.flush()
    else:
        sys.stderr.write('VERIFY-PROJECTS-FAILED %s code=%d %s\n'
                         % (run_id, code, report['verdict'].get('reason') or ''))
        sys.stderr.flush()
    return code


def summarise(report):
    """人读摘要：逐项输出位置 + 只构建未运行的边界。"""
    print('repo      : %s' % report['repo'])
    print('project   : %s' % report['project'])
    print('outDir    : %s' % report['outDir'])
    print('mode      : %s' % report['mode'])
    print('files     : %d' % len(report['files']))
    for stage in report['stages']:
        print('stage %-14s rc=%-4s %s' % (stage['name'],
                                          'timeout' if stage.get('timedOut') else stage.get('rc'),
                                          stage.get('note') or ''))
    print('lock      : %s released=%s' % (report['lock']['tokenFingerprint'],
                                          report['lock']['released']))
    print('边界      : 窗口入口只构建、不运行；原生窗口与 IME 未自动验证')


def test_verdict(markers, spec=None):
    """离屏测试的**业务判据**：返回失败原因，None 表示成立。

    `go test` 退出码 0 不等于「真的验过」：一个用例都没跑（pass=0）、被 `t.Skip`
    兜底（skip>0）、包内根本没有测试文件（noTestFiles）都必须判不成立。

    spec 是 PROJECTS 里的工程规格：不同工程有自己的用例名集合与完成标记。
    省略 spec 时保持 R11-C 起的默认口径（preferences 的 11 个 CROSS-DONE），
    让既有调用方（工程 E2E 脚本）不受新增工程影响。
    """
    if markers['pass'] < 1 or markers['skip'] or markers['fail'] or markers['noTestFiles']:
        return ('离屏测试判据不成立（pass=%d skip=%d fail=%d noTestFiles=%d）'
                % (markers['pass'], markers['skip'], markers['fail'], markers['noTestFiles']))
    expected = set(spec['expectedTests']) if spec and spec.get('expectedTests') else {
        'TestCrossEditDraftKeepsSaved', 'TestCrossEditMaxLenCountsRunes',
        'TestCrossEditPortFilterDropsNonDigits', 'TestCrossEditConfirmWritesSaved',
        'TestCrossEditConfirmRejectedKeepsSaved', 'TestCrossEditResetRestoresSaved',
        'TestCrossEditReturnPayloadOnly', 'TestCrossEditStatusBranch',
        'TestCrossEditInstancesIsolated', 'TestCrossSummaryFollowsSharedModel',
        'TestCrossSummaryInstancesIsolated',
    }
    marker = (spec or {}).get('completionMarker', 'CROSS-DONE')
    completed = markers.get('completed', [])
    passed = markers.get('passedNames', [])
    if (len(completed) != len(expected) or set(completed) != expected
            or len(passed) != len(expected) or set(passed) != expected
            or markers['pass'] != len(expected)):
        return '离屏测试完成标记或PASS集合不完整（要求 %d 个唯一 %s）' % (len(expected), marker)
    return None


def check_markers(stdout, stderr):
    stderr_lines = stderr.decode('utf-8', 'replace').splitlines()
    # The first top-level diagnostic identifies the failed phase. Paths and
    # nested compiler diagnostics can contain the same words.
    first_error = next((line for line in stderr_lines if line.startswith('goxml generate: ')), '')
    return (
        check_targets(stdout) is not None,
        first_error == 'goxml generate: 提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）',
    )


def check_targets(raw_stdout):
    """`提交前检查通过：N 个目标包` 里的 N；没有该标记返回 None。

    R13-C 起一键入口用**一次目录 generate** 覆盖全部 View，这个数字就是「本次检查
    实际覆盖几个目标包」：两页同批时应为 2，逐页调用时每条命令只会报 1。
    它把「只启动一次 generate」从 argv 数量延伸到检查覆盖面，两者一起才说明批次是真的。
    """
    text = raw_stdout.decode('utf-8', 'replace')
    markers = [m for line in text.splitlines()
               if (m := re.fullmatch(r'提交前检查通过：([1-9][0-9]*) 个目标包', line))]
    counts = [int(m.group(1)) for m in markers]
    return counts[0] if len(counts) == 1 else None


def observe_check_markers(stage):
    """把 --check 的两个观察标记记到 generate 阶段上（成功/失败路径都要记）。

    * checkReported：stdout 出现「提交前检查通过」（原生检查真的执行并成功）；
    * checkFailureObserved：可信的退出码 2 与 stderr 的原生编译失败首行同时成立。
    R12-C：只有两者都在失败路径上留痕，「参数已转发」与「原生检查已执行并失败」
    才能在一键入口的报告里区分开。
    R13-C：再记 checkTargets（检查覆盖的目标包数），供「一次批覆盖两页」取证。
    """
    stdout = read_raw(stage.get('stdout'))
    passed, failed = check_markers(stdout, read_raw(stage.get('stderr')))
    stage['checkReported'] = passed
    stage['checkFailureObserved'] = (
        failed and stage['rcSource'] == 'observed' and stage['rc'] == 2 and stage['infraOk']
    )
    stage['checkTargets'] = check_targets(stdout) if passed else None


def check_status(stages, requested):
    """Differentiate flag forwarding from a new CLI's observed check success."""
    if not requested:
        return {'forwarded': False}
    generated = [s for s in stages if s.get('name', '').startswith('generate:')]
    if not any(s.get('started') for s in generated):
        return {'forwarded': False, 'passed': None, 'note': '尚未启动带 --check 的生成子进程'}
    failed = [s for s in generated if s.get('rc') != 0 or not s.get('infraOk')]
    if failed:
        note = '生成子进程失败或收口未证，未证明提交前检查通过'
        if any(s.get('checkFailureObserved') for s in failed):
            note = '生成子进程失败，且观察到原生提交前检查已执行并失败'
        return {'forwarded': True, 'passed': False, 'note': note}
    if generated and all(s.get('checkReported') is True for s in generated):
        return {'forwarded': True, 'passed': True,
                'note': '每个生成子进程均成功并输出提交前检查通过标记'}
    return {'forwarded': True, 'passed': None,
            'note': '仅证明参数转发；未观察到所有生成子进程的提交前检查成功标记'}


def prepare_project(report, repo, project, module, out_dir):
    """**独占创建**输出目录并复制模板；返回复制摘要。两个分支共用同一套所有权规则。"""
    claim_output_dir(out_dir)
    copied = copy_template(template_dir(repo, project), out_dir, module, repo)
    report['stages'].append({'name': 'copy', 'kind': 'python', 'argv': None, 'cwd': out_dir,
                             'rc': 0, 'timedOut': False,
                             'note': '复制 %d 个文件，替换 %d 个'
                             % (len(copied['written']), len(copied['substituted']))})
    report['substituted'] = copied['substituted']
    report['gxml'] = discover_gxml(out_dir)
    if not report['gxml']:
        raise Refused(EXIT_RECORD, '复制后工程里没有任何 .gxml，模板不完整：%s' % out_dir)
    return copied


def run_generate_stage(report, procmod, name, argv, out_dir, log_dir, timeout, observe):
    """跑一次 generate 阶段；带 --check 时把观察标记记到阶段上（成功/失败都记）。"""
    try:
        stage = run_stage(report, procmod, name, argv, out_dir, log_dir, timeout,
                          procmod.GO_ENV_FIXED)
    except Refused:
        # run_stage 已把失败阶段记入 report；失败路径同样要留 --check 的观察证据。
        if observe and report['stages'] and report['stages'][-1].get('name') == name:
            observe_check_markers(report['stages'][-1])
        raise
    if observe:
        observe_check_markers(stage)
    return stage


def generate_batch(report, procmod, cli_path, out_dir, log_dir, args):
    """一次目录 generate 覆盖本批全部 View（R13-C）。

    为什么不是逐页调用（R12-C 的形态）：

    * **只启动一次**：两个 View 同属一个模块、一个 Manifest，一条命令就能覆盖；
    * **全或无**：`--check` 在整批候选齐备、写盘之前统一执行，任一目标包编不过则
      **两页的旧产物、Manifest、成功缓存都保持原样**——逐页调用做不到这一点：第一页
      成功即已落盘，第二页失败就留下「一页新、一页旧」的混合状态（本轮实测，
      dev-log 有对照数据）；
    * **检查覆盖面是两页**：`提交前检查通过：2 个目标包`，逐页调用只会各报 1。

    发现根与输出根都取共同父目录，落点才与逐文件模式一致（见 `common_parent_dir`）。
    """
    batch_dir = common_parent_dir(report['gxml'])
    argv = [cli_path, 'generate', batch_dir, '--out-dir', batch_dir]
    if args.check:
        argv.append('--check')
    return run_generate_stage(report, procmod, 'generate:' + batch_dir, argv,
                              out_dir, log_dir, args.timeout, args.check)


def run(args, report, repo, project, module, out_dir, run_id):
    template = template_dir(repo, project)
    if not os.path.isdir(template):
        # 缺模板在**碰任何目录/锁之前**就拒绝：调用方拿到的退出码不掺杂副作用。
        raise Refused(EXIT_REFUSED, '模板目录不存在：%s' % template, template=template)

    before = template_fingerprint(repo, project)
    log_dir = args.log_dir or os.path.join(out_dir, '.verify-projects')
    report['logDir'] = log_dir
    report['logDirOwned'] = False
    ensure_no_reparse(log_dir)
    if os.path.lexists(log_dir):
        raise Refused(EXIT_REFUSED, '证据目录已存在，不覆盖历史记录：%s' % log_dir)

    if args.copy_only:
        prepare_project(report, repo, project, module, out_dir)
        # 证据目录建在工程内部、名字带点：`go build ./...` 不会把它当包，
        # 用户也能一眼看出这是本工具留下的证据而不是他的源码。
        claim_output_dir(log_dir)
        report['logDirOwned'] = True
        report['files'] = inventory(
            out_dir, skip_dirs=(os.path.relpath(log_dir, out_dir).replace('\\', '/'),))
        write_json(os.path.join(log_dir, 'copy-inventory.json'),
                   {'runId': run_id, 'files': report['files']})
        report['templateUntouched'] = (template_fingerprint(repo, project) == before)
        if not report['templateUntouched']:
            raise Refused(EXIT_REFUSED, '模板在复制过程中被改动——本工具不应写模板源码')
        return EXIT_OK

    procmod = load_helper(repo, 'verify_examples_proc.py', 'verify_examples_proc')
    lockmod = load_helper(repo, 'verify-examples-lock.py', 'verify_examples_lock')

    # 锁在**创建输出目录之前**拿：锁忙时本轮连一个文件都不该落地。
    lock_path = os.path.abspath(args.lock_path)
    report['lock']['path'] = lock_path
    lock = lockmod.Lock(lock_path)
    try:
        lock.acquire(args.lock_agent, args.lock_task, run_id)
    except lockmod.LockHeldError as exc:
        holder = exc.holder or {}
        report['lock']['busy'] = True
        # 只记持有者的**身份字段**，不抄别人的 token：别人的锁连指纹都不该由我记录。
        report['lock']['holder'] = {k: holder.get(k) for k in
                                    ('agent', 'task', 'runId', 'hostPid', 'acquiredAt')}
        raise Refused(EXIT_LOCK_BUSY,
                      '公共锁被他人持有，未创建输出目录、未启动任何 Go 进程：%s' % lock_path,
                      lockPath=lock_path, holder=report['lock']['holder'])
    report['lock']['acquired'] = True
    # 报告里只出现指纹：明文 token 既不落盘也不进子进程环境。
    report['lock']['tokenFingerprint'] = token_fingerprint(lock.token)

    released = None
    try:
        prepare_project(report, repo, project, module, out_dir)
        claim_output_dir(log_dir)
        report['logDirOwned'] = True

        # 1) 真实构建 CLI：只用仓内源码，离线（GOPROXY=off）+ 只读模块
        cli_name = 'goxml.exe' if os.name == 'nt' else 'goxml'
        cli_path = os.path.join(log_dir, cli_name)
        run_stage(report, procmod, 'build-cli',
                  [args.go, 'build', '-o', cli_path, './cmd/goxml'],
                  repo, log_dir, args.timeout, procmod.GO_ENV_FIXED)

        # 2) 一次目录 generate 覆盖本批全部 View（R13-C；发现根=输出根=共同父目录）
        generate_batch(report, procmod, cli_path, out_dir, log_dir, args)

        # 3) 只构建窗口入口——**永不执行**（执行就会开原生窗口）
        entry = PROJECTS[project]['entry']
        # 产物名按入口包名派生：preferences → preferences.exe（保持旧证据路径不变），
        # taskboard → taskboard.exe。写死的工程名会随第二个工程悄悄失真。
        exe_name = os.path.basename(entry) + ('.exe' if os.name == 'nt' else '')
        run_stage(report, procmod, 'build-entry',
                  [args.go, 'build', '-o', os.path.join(log_dir, exe_name), './' + entry],
                  out_dir, log_dir, args.timeout, procmod.GO_ENV_FIXED)

        # 4) 可选离屏测试：只跑模板自带的目标包，不跑未过滤的全仓
        if not args.skip_tests:
            spec = PROJECTS[project]
            argv = ([args.go, 'test', '-count=1', '-v', '-timeout', '20m']
                    + list(spec['testTargets']) + ['-run', spec['testRun']])
            stage = run_stage(report, procmod, 'test', argv,
                              out_dir, log_dir, args.timeout, procmod.GO_ENV_FIXED)
            markers = test_markers(read_raw(stage['stdout']),
                                   spec.get('completionMarker', 'CROSS-DONE'))
            stage['markers'] = markers
            problem = test_verdict(markers, spec)
            if problem:
                raise Refused(EXIT_BUSINESS, problem, stage='test', markers=markers)
    finally:
        released = lock.release()
        report['lock']['released'] = released
        report['lock']['releaseError'] = lock.release_error or None

    report['files'] = inventory(
        out_dir, skip_dirs=(os.path.relpath(log_dir, out_dir).replace('\\', '/'),))
    report['templateUntouched'] = (template_fingerprint(repo, project) == before)
    if not report['templateUntouched']:
        raise Refused(EXIT_REFUSED, '模板在运行过程中被改动——本工具不应写模板源码')
    if not released:
        # 阶段都过了但锁没按自有 token 释放：这是收口缺陷，不能算通过。
        raise Refused(EXIT_LOCK_NOT_RELEASED,
                      '锁未按本轮 token 释放：%s' % (lock.release_error or '未知原因'),
                      lockPath=lock_path)
    return EXIT_OK


if __name__ == '__main__':
    try:
        raise SystemExit(main(sys.argv[1:]))
    except SystemExit:
        raise
    except KeyboardInterrupt:
        raise SystemExit(130)

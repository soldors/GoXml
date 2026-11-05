#!/usr/bin/env python3
# -*- coding: utf-8 -*-
r"""`scripts/verify-gallery-accept.py` 的**低成本**自检（R22-C）。

它只测「入口自己」的判据，**不跑任何 Go**：整套是秒级的，可以随每次改动跑。
真正的验收（生成 / 构建 / 离屏测试 / 缓存 / 资产 / 变异）在
`scripts/verify-gallery-accept.py` 里，需要公共锁与几分钟。

覆盖六类反例，它们正是「入口能不能被当成证据」的分界：

  1. **重复目录拒绝**：`--run-dir` 已存在 → rc 3，且那个目录里一个新条目都不许多；
  2. **旧报告拒绝**：`--json` 已存在 → rc 2，且连运行目录都不创建；
  3. **用法错误**：缺 `--lock-path` / 未知参数 → rc 2；
  4. **锁忙**：别人持锁 → rc 8，零新增（锁本身拿不到时记 skipped，不假绿）；
  5. **缺必要用例就失败**：台账判据对「少一个用例 / SKIP / FAIL / 多一个用例 /
     完成标记缺项 / 没有测试文件」都必须红，且文案要点名缺的是哪一个；
  6. **保旧判据不是只比字节**：同样字节但 mtime 变了，必须算「动过」。

另外把两个会直接毁掉变异证据的助手也一起钉住：变异锚点必须唯一、副本目录必须新建。

用法::

    python scripts/test-verify-gallery-accept.py \
        --work-root "D:/work/r22c-selftest" \
        --report "D:/work/r22c-selftest/report.json"

退出码：0 全部通过 · 1 有用例失败 · 2 用法错误
"""
import argparse
import importlib.util
import json
import os
import subprocess
import sys

EXIT_OK = 0
EXIT_FAILED = 1
EXIT_USAGE = 2

SCRIPT = 'verify-gallery-accept.py'
LOCK_DEFAULT = r'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock'


def load_module(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class Case(object):
    def __init__(self, name, detail):
        self.name = name
        self.detail = detail or ''
        self.status = 'fail'


class Runner(object):
    def __init__(self, work_root, repo, lock_path):
        self.work_root = work_root
        self.repo = repo
        self.lock_path = lock_path
        self.script = os.path.join(repo, 'scripts', SCRIPT)
        self.mod = load_module(self.script, 'r22c_gallery_accept_under_test')
        self.cases = []
        self._n = 0

    # -- 基础设施 ---------------------------------------------------------

    def case_dir(self, label):
        self._n += 1
        path = os.path.join(self.work_root, '%02d-%s' % (self._n, label))
        os.makedirs(path)
        return path

    def run(self, name, fn):
        case = Case(name, '')
        try:
            fn(case)
            if case.status != 'skipped':
                case.status = 'pass'
        except AssertionError as exc:
            case.status = 'fail'
            case.detail = '%s' % (exc,)
        except Exception as exc:  # 未预料异常也算失败，不能静默通过
            case.status = 'fail'
            case.detail = '%s: %s' % (type(exc).__name__, exc)
        self.cases.append({'name': case.name, 'status': case.status, 'detail': case.detail})
        return case.status == 'pass'

    def spawn(self, argv, timeout=120):
        return subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              timeout=timeout, env=dict(os.environ))

    # -- 用例 -------------------------------------------------------------

    def t_run_dir_exists(self, case):
        d = self.case_dir('run-dir-exists')
        target = os.path.join(d, 'same')
        os.mkdir(target)
        before = sorted(os.listdir(target))
        proc = self.spawn([sys.executable, self.script, '--run-dir', target,
                           '--lock-path', self.lock_path])
        after = sorted(os.listdir(target))
        assert proc.returncode == 3, '重复目录应 rc=3，实际 %d（stderr=%s）' % (
            proc.returncode, proc.stderr.decode('utf-8', 'replace')[:200])
        assert before == after, '被拒绝的目录里多出了条目：%s' % (set(after) - set(before),)
        assert not os.path.exists(os.path.join(target, 'report.json')), \
            '拒绝路径往别人的目录里写了报告'
        case.detail = 'rc=3，目录内容未变'

    def t_json_exists(self, case):
        d = self.case_dir('json-exists')
        report = os.path.join(d, 'report.json')
        with open(report, 'wb') as fh:
            fh.write(b'{}\n')
        run_dir = os.path.join(d, 'fresh')
        proc = self.spawn([sys.executable, self.script, '--run-dir', run_dir,
                           '--lock-path', self.lock_path, '--json', report])
        assert proc.returncode == 2, '旧报告路径应 rc=2，实际 %d' % proc.returncode
        assert not os.path.exists(run_dir), '拒绝路径仍然创建了运行目录'
        case.detail = 'rc=2，运行目录未创建'

    def t_usage_errors(self, case):
        d = self.case_dir('usage')
        run_dir = os.path.join(d, 'fresh')
        proc = self.spawn([sys.executable, self.script, '--run-dir', run_dir])
        assert proc.returncode == 2, '缺 --lock-path 应 rc=2，实际 %d' % proc.returncode
        assert not os.path.exists(run_dir), '用法错误仍然创建了运行目录'
        proc = self.spawn([sys.executable, self.script, '--run-dir', run_dir,
                           '--lock-path', self.lock_path, '--no-such-flag'])
        assert proc.returncode == 2, '未知参数应 rc=2，实际 %d' % proc.returncode
        case.detail = '缺参数 / 未知参数都 rc=2 且零新增'

    def t_lock_busy(self, case):
        lockmod = self.mod.load_helper(self.repo, 'verify-examples-lock.py',
                                       'r22c_selftest_lock')
        holder = lockmod.Lock(self.lock_path)
        try:
            holder.acquire('selftest', 'gallery-accept-negative', 'selftest-holder')
        except lockmod.LockHeldError as exc:
            case.status = 'skipped'
            case.detail = '公共锁已被他人持有（%s），本条如实跳过' % (exc.holder,)
            return
        try:
            d = self.case_dir('lock-busy')
            run_dir = os.path.join(d, 'fresh')
            proc = self.spawn([sys.executable, self.script, '--run-dir', run_dir,
                               '--lock-path', self.lock_path])
            assert proc.returncode == 8, '锁忙应 rc=8，实际 %d（stderr=%s）' % (
                proc.returncode, proc.stderr.decode('utf-8', 'replace')[:200])
            assert not os.path.exists(run_dir), '锁忙仍然创建了运行目录（应在取锁前拒绝）'
            case.detail = 'rc=8，零新增'
        finally:
            holder.release()

    def _expected_text(self, spec, drop=None, extra=None, skip=None, fail=None,
                       marker_drop=None):
        lines = []
        for name in spec['expectedTests']:
            if name == drop:
                continue
            lines.append('=== RUN   %s' % name)
            if name == skip:
                lines.append('--- SKIP: %s (0.00s)' % name)
            elif name == fail:
                lines.append('--- FAIL: %s (0.00s)' % name)
            else:
                lines.append('--- PASS: %s (0.01s)' % name)
                if name != marker_drop:
                    # 台账的完成标记行形如 `<file>:<line>: ROUTE-DONE TestX`（行尾锚定）。
                    lines.append('    route_test.go:1: %s %s'
                                 % (spec['completionMarker'], name))
        for name in (extra or []):
            lines.append('=== RUN   %s' % name)
            lines.append('--- PASS: %s (0.01s)' % name)
        return ('\n'.join(lines) + '\n').encode('utf-8')

    def t_audit_accepts_complete(self, case):
        spec = self.spec
        audit = self.mod.audit_test_run(self._expected_text(spec), spec)
        assert not audit['problems'], '完整台账被判不成立：%s' % (audit['problems'],)
        assert len(audit['passed']) == len(spec['expectedTests']), '通过集数量不对'
        case.detail = '%d 个用例全部 PASS，无问题' % len(spec['expectedTests'])

    def t_audit_missing_case(self, case):
        spec = self.spec
        missing = spec['expectedTests'][3]
        audit = self.mod.audit_test_run(self._expected_text(spec, drop=missing), spec)
        assert audit['problems'], '少一个用例却没判不成立（空判据）'
        joined = ' | '.join(audit['problems'])
        assert missing in joined, '失败文案没点名缺的用例 %s：%s' % (missing, joined)
        case.detail = '点名缺少 %s' % missing

    def t_audit_skip_and_fail(self, case):
        spec = self.spec
        name = spec['expectedTests'][1]
        audit = self.mod.audit_test_run(self._expected_text(spec, skip=name), spec)
        assert any('SKIP' in p for p in audit['problems']), 'SKIP 未判不成立：%s' % (
            audit['problems'],)
        audit = self.mod.audit_test_run(self._expected_text(spec, fail=name), spec)
        assert any('FAIL' in p for p in audit['problems']), 'FAIL 未判不成立：%s' % (
            audit['problems'],)
        case.detail = 'SKIP / FAIL 各自判不成立'

    def t_audit_extra_case(self, case):
        spec = self.spec
        audit = self.mod.audit_test_run(
            self._expected_text(spec, extra=['TestRouteNotInSpec']), spec)
        assert any('规格外' in p for p in audit['problems']), '多跑的用例没被判不成立：%s' % (
            audit['problems'],)
        case.detail = '规格外用例被判不成立'

    def t_audit_marker_missing(self, case):
        spec = self.spec
        dropped = spec['expectedTests'][2]
        audit = self.mod.audit_test_run(self._expected_text(spec, marker_drop=dropped), spec)
        assert any(spec['completionMarker'] in p for p in audit['problems']), \
            '完成标记缺项没被判不成立：%s' % (audit['problems'],)
        case.detail = '%s 完成标记缺项被判不成立' % spec['completionMarker']

    def t_audit_no_test_files(self, case):
        spec = self.spec
        raw = self._expected_text(spec) + '?   example.com/x/none\t[no test files]\n'.encode()
        audit = self.mod.audit_test_run(raw, spec)
        assert any('没有任何测试文件' in p for p in audit['problems']), \
            'no test files 没被判不成立：%s' % (audit['problems'],)
        case.detail = 'no test files 被判不成立'

    def t_snapshot_sees_mtime_only(self, case):
        d = self.case_dir('snapshot')
        path = os.path.join(d, 'home_gen.go')
        with open(path, 'wb') as fh:
            fh.write(b'package home\n')
        first = self.mod.snapshot(d, ['home_gen.go'])
        st = os.stat(path)
        # 同样字节、不同 mtime：只比字节会误判成「没动」。
        os.utime(path, ns=(st.st_atime_ns, st.st_mtime_ns + 5000000))
        second = self.mod.snapshot(d, ['home_gen.go'])
        diff = self.mod.changed(first, second)
        assert diff == ['home_gen.go'], '只改 mtime 没被抓到（保旧判据退化成只比字节）'
        os.remove(path)
        third = self.mod.snapshot(d, ['home_gen.go'])
        assert self.mod.changed(second, third) == ['home_gen.go'], '文件消失没被抓到'
        case.detail = 'mtime-only 与删除都能被抓到'

    def t_mutate_anchor_unique(self, case):
        d = self.case_dir('anchor')
        path = os.path.join(d, 'wire.go')
        with open(path, 'wb') as fh:
            fh.write(b'return home(m)\n')
        try:
            self.mod.mutate_text(path, 'return detail(m)\n', 'x')
        except self.mod.Refused as exc:
            assert exc.code == 3, '锚点不存在应以 3 拒绝，实际 %d' % exc.code
        else:
            raise AssertionError('锚点出现 0 次却改成功了（变异可能改错地方）')
        with open(path, 'wb') as fh:
            fh.write(b'a\nreturn home(m)\nreturn home(m)\n')
        try:
            self.mod.mutate_text(path, 'return home(m)\n', 'x')
        except self.mod.Refused as exc:
            assert exc.code == 3, '锚点不唯一应以 3 拒绝，实际 %d' % exc.code
        else:
            raise AssertionError('锚点出现 2 次却改成功了（变异会改到意外的位置）')
        with open(path, 'rb') as fh:
            assert fh.read() == b'a\nreturn home(m)\nreturn home(m)\n', '拒绝时不得写文件'
        case.detail = '0 次 / 2 次都拒绝且不写文件'

    def t_copy_project_refuses_existing(self, case):
        d = self.case_dir('copy')
        src = os.path.join(d, 'src')
        os.mkdir(src)
        with open(os.path.join(src, 'f.txt'), 'wb') as fh:
            fh.write(b'x')
        dst = os.path.join(d, 'dst')
        os.mkdir(dst)
        try:
            self.mod.copy_project(src, dst)
        except self.mod.Refused as exc:
            assert exc.code == 3, '已存在副本目录应以 3 拒绝，实际 %d' % exc.code
        else:
            raise AssertionError('副本目录已存在却继续复制（会污染历史变异树）')
        case.detail = '已存在副本目录被拒绝'

    def t_run_id_and_report_path(self, case):
        a = self.mod.new_run_id()
        b = self.mod.new_run_id()
        assert a != b, '两次生成的 runId 相同（陈旧报告无法区分）'
        assert self.mod.SCHEMA == 'r22c-gallery-accept-1', '报告 schema 变了却没同步自检'
        case.detail = 'runId 唯一；schema=%s' % self.mod.SCHEMA

    # -- 入口 -------------------------------------------------------------

    def run_all(self):
        self.spec = self.mod.load_helper(self.repo, 'verify-projects.py',
                                         'r22c_selftest_vp').PROJECTS['routed-gallery']
        plan = [
            ('重复目录拒绝', self.t_run_dir_exists),
            ('旧报告拒绝', self.t_json_exists),
            ('用法错误', self.t_usage_errors),
            ('锁忙零新增', self.t_lock_busy),
            ('台账：完整', self.t_audit_accepts_complete),
            ('台账：缺用例', self.t_audit_missing_case),
            ('台账：SKIP/FAIL', self.t_audit_skip_and_fail),
            ('台账：多用例', self.t_audit_extra_case),
            ('台账：完成标记缺项', self.t_audit_marker_missing),
            ('台账：无测试文件', self.t_audit_no_test_files),
            ('快照：mtime/删除', self.t_snapshot_sees_mtime_only),
            ('变异锚点唯一', self.t_mutate_anchor_unique),
            ('副本目录新建', self.t_copy_project_refuses_existing),
            ('runId 与 schema', self.t_run_id_and_report_path),
        ]
        for name, fn in plan:
            self.run(name, fn)
        return self.cases


def main(argv):
    ap = argparse.ArgumentParser(prog='test-verify-gallery-accept.py',
                                 description=__doc__.split('\n')[0])
    ap.add_argument('--work-root', required=True, help='本轮工作目录（不存在则创建）')
    ap.add_argument('--report', required=True, help='报告路径（已存在则拒绝）')
    ap.add_argument('--repo', default=None, help='仓库根（默认按本脚本位置推导）')
    ap.add_argument('--lock-path', default=LOCK_DEFAULT)
    args = ap.parse_args(argv)

    repo = os.path.abspath(args.repo) if args.repo else os.path.dirname(
        os.path.dirname(os.path.abspath(__file__)))
    if not os.path.isfile(os.path.join(repo, 'scripts', SCRIPT)):
        sys.stderr.write('找不到被测脚本：%s\n' % os.path.join(repo, 'scripts', SCRIPT))
        return EXIT_USAGE
    if os.path.lexists(args.report):
        sys.stderr.write('报告路径已存在（不覆盖历史证据）：%s\n' % args.report)
        return EXIT_USAGE
    work_root = os.path.abspath(args.work_root)
    if os.path.lexists(work_root):
        # 只肯用空目录：非空就不动它（这里的自检目录由调用方负责新建）。
        if not os.path.isdir(work_root) or os.listdir(work_root):
            sys.stderr.write('工作目录必须是空目录（不删任何已有内容）：%s\n' % work_root)
            return EXIT_USAGE
    else:
        os.makedirs(work_root)

    runner = Runner(work_root, repo, args.lock_path)
    cases = runner.run_all()
    failed = [c for c in cases if c['status'] == 'fail']
    skipped = [c for c in cases if c['status'] == 'skipped']
    report = {
        'schema': 'r22c-gallery-accept-selftest-1',
        'repo': repo,
        'workRoot': work_root,
        'counts': {'total': len(cases), 'passed': len(cases) - len(failed) - len(skipped),
                   'failed': len(failed), 'skipped': len(skipped)},
        'cases': cases,
    }
    with open(args.report, 'w', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2, sort_keys=True)
        fh.write('\n')

    for c in cases:
        print('%-4s %-24s %s' % (c['status'].upper(), c['name'], c['detail']))
    if failed:
        sys.stderr.write('GALLERY-ACCEPT-TEST-FAILED failed=%d skipped=%d\n'
                         % (len(failed), len(skipped)))
        return EXIT_FAILED
    sys.stdout.write('GALLERY-ACCEPT-TEST-OK total=%d skipped=%d\n'
                     % (len(cases), len(skipped)))
    return EXIT_OK


if __name__ == '__main__':
    try:
        raise SystemExit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        raise SystemExit(130)

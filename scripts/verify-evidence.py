#!/usr/bin/env python
# -*- coding: utf-8 -*-
r"""R8-C **只读证据校验器**：逐行严格解析 JSONL 台账，按自有 schema 判定。

它只读，**不修复**：不回补时间戳、不顺手把坏 JSON 改对、不把 `valid` 改成 true、
不吞坏行。发现历史别的 schema（R6 case result / R6 选择器 row / R7 手写台账形态）
一律标 `unsupported` —— 不匹配**不等于**造假。

判据（缺一项就不算通过，绝不默认成功）：

  * 每行必须是**严格** JSON 对象（`json.loads` 严格模式；CRLF/NUL/非法 UTF-8 只报告
    字节事实，不改写字节后再判定）；
  * 缺失必填字段 / 类型不对（**bool 不是 int**）→ FAIL；
  * 重复 id → FAIL；
  * 时间：endedUtc < startedUtc → FAIL；elapsedSec 为空/负 → INCOMPLETE（不补造时间）；
  * rc 来源：`timedOut` 与 `rcSource` 必须自洽；超时那个 rc 是**我们强制给的**，
    写成 `observed` 即 FAIL；
  * raw：按**落盘字节**重算 bytes/SHA-256，与台账自述不符 → FAIL；
  * 未完成 / 收口未知（未启动、contained 未证、activeAfter 未知、cleanupError）
    → INCOMPLETE，不是"通过"。

退出码：

  0 PASS（每一行都既有效又完整）
  1 FAIL（存在造假/自相矛盾/坏行/缺字段）
  2 用法错误
  3 INCOMPLETE（没有 FAIL，但有行证据不完整：未启动/超时/收口未知/未知形态）
  4 UNSUPPORTED（全部行都是历史别的 schema，本校验器不做判定）

用法：

  python verify-evidence.py --jsonl <file> [--jsonl <file2>] [--json <out|->] [--quiet]
"""
import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import evidence_schema as schema  # noqa: E402

EXIT_PASS = 0
EXIT_FAIL = 1
EXIT_USAGE = 2
EXIT_INCOMPLETE = 3
EXIT_UNSUPPORTED = 4

MAX_PRINTED_ISSUES = 60


def read_bytes(path):
    with open(path, 'rb') as fh:
        return fh.read()


def parse_line(raw_line):
    """严格解析一行。返回 `(obj, issues)`；`obj` 为 None 表示这一行没法当记录看。"""
    issues = []
    crlf = raw_line.endswith(b'\r')
    body = raw_line[:-1] if crlf else raw_line
    if crlf:
        issues.append({'code': 'crlf-line-ending', 'detail': '行尾是 CRLF（字节事实，不做归一化）',
                       'severity': schema.SEVERITY_WARN})
    if b'\x00' in body:
        issues.append({'code': 'nul-byte', 'detail': '行内含 %d 个 NUL 字节' % body.count(b'\x00'),
                       'severity': schema.SEVERITY_WARN})
    try:
        text = body.decode('utf-8')
    except UnicodeDecodeError as exc:
        issues.append({'code': 'non-utf8', 'detail': '%s' % exc, 'severity': schema.SEVERITY_FAIL})
        return None, issues
    try:
        obj = schema.strict_json_loads(text)
    except ValueError as exc:
        issues.append({'code': 'bad-json', 'detail': '%s' % exc, 'severity': schema.SEVERITY_FAIL})
        return None, issues
    if not isinstance(obj, dict):
        issues.append({'code': 'not-an-object', 'detail': 'type=%s' % type(obj).__name__,
                       'severity': schema.SEVERITY_FAIL})
        return None, issues
    return obj, issues


def check_line(obj, base_issues, check_disk=True):
    status, issues = schema.validate_full(obj, check_disk=check_disk)
    return status, list(base_issues) + list(issues)


def scan_file(path, check_disk=True):
    raw = read_bytes(path)
    report = {
        'path': os.path.abspath(path),
        'bytes': len(raw),
        'sha256': schema.sha256_file(path),
        'nulBytes': raw.count(b'\x00'),
        'crlfLines': 0,
        'endsWithNewline': raw.endswith(b'\n') if raw else True,
        'blankLines': 0,
        # 固定为 bool：调用方不该用 `.get(...) 默认值` 把"没报告过"和"报告为假"混为一谈。
        'lastLineUnterminated': False,
        'lines': [],
        'counts': {'pass': 0, 'fail': 0, 'incomplete': 0, 'unsupported': 0},
    }
    parts = raw.split(b'\n')
    trailing_empty = False
    if parts and parts[-1] == b'':
        trailing_empty = True
        parts = parts[:-1]
    if parts and not trailing_empty:
        # 最后一行没有换行：可能是被硬中断截断的半行——这是**必须判出来**的事实。
        # 键在上面已初始化为 False，这里只需要翻 True，两个取值都是真实 bool。
        report['lastLineUnterminated'] = True

    for idx, raw_line in enumerate(parts, start=1):
        if raw_line.endswith(b'\r'):
            report['crlfLines'] += 1
        if not raw_line.strip():
            report['blankLines'] += 1
            report['lines'].append({'line': idx, 'status': schema.STATUS_PASS, 'id': None,
                                    'issues': [{'code': 'blank-line', 'detail': '空行（不计入记录）',
                                                'severity': schema.SEVERITY_WARN}],
                                    'blank': True})
            continue
        obj, base = parse_line(raw_line)
        if obj is None:
            report['lines'].append({'line': idx, 'status': schema.STATUS_FAIL, 'id': None,
                                    'issues': base})
            report['counts'][schema.STATUS_FAIL] += 1
            continue
        status, issues = check_line(obj, base, check_disk)
        # 带 warn 的行若本来 pass，仍按 pass 计数（warn 只报告）。
        report['lines'].append({'line': idx, 'status': status, 'id': obj.get('id'),
                                'schema': obj.get('schema'), 'phase': obj.get('phase'),
                                'issues': issues})
        report['counts'][status] += 1
    return report


def find_duplicates(reports):
    """重复 id 判红：第一条算"先到先得"，其后每条都追加 `duplicate-id` FAIL。

    定位必须用 **(report 索引, 行号)**，不能只用 `(path, line)`：
    `build_report([a, a])` 这种 direct API 调用下，两份报告的 path 与 line **完全相同**，
    按 `(path, line)` 认"第一条"会让两条**都**命中 skip 分支 → 一条都不翻红，
    而 `dups` 里又点了这个 id 的名——输出自相矛盾。带上 report 索引后，
    同一份文件被传两次也能正确判红（CLI 入口另有拒绝，但 library 调用绕不过 CLI 那一层）。

    注意路径里可能带盘符冒号，`where` 一律用 (path, line) 元组拼装，不做字符串切分。
    """
    seen = {}
    for rep_idx, rep in enumerate(reports):
        for entry in rep['lines']:
            rid = entry.get('id')
            if not isinstance(rid, str):
                continue
            seen.setdefault(rid, []).append((rep_idx, rep['path'], entry['line']))

    dups = []
    dup_ids = set()
    for rid, where in seen.items():
        if len(where) > 1:
            dup_ids.add(rid)
            dups.append({'id': rid, 'where': ['%s:%d' % (p, n) for _i, p, n in where]})

    for rep_idx, rep in enumerate(reports):
        for entry in rep['lines']:
            rid = entry.get('id')
            if not isinstance(rid, str) or rid not in dup_ids:
                continue
            first_idx, first_path, first_line = seen[rid][0]
            if rep_idx == first_idx and entry['line'] == first_line:
                continue
            entry['issues'] = list(entry['issues']) + [
                {'code': 'duplicate-id',
                 'detail': 'id=%s 已在 %s:%d 出现过' % (rid, first_path, first_line),
                 'severity': schema.SEVERITY_FAIL}]
            if entry['status'] != schema.STATUS_FAIL:
                rep['counts'][entry['status']] -= 1
                entry['status'] = schema.STATUS_FAIL
                rep['counts'][schema.STATUS_FAIL] += 1
    return dups


def overall_status(reports):
    statuses = []
    for rep in reports:
        for entry in rep['lines']:
            if entry.get('blank'):
                continue
            statuses.append(entry['status'])
    return schema.combine(statuses)


def build_report(paths, check_disk=True):
    reports = [scan_file(p, check_disk) for p in paths]
    dups = find_duplicates(reports)
    status = overall_status(reports)
    totals = {'files': len(reports), 'lines': 0, 'pass': 0, 'fail': 0,
              'incomplete': 0, 'unsupported': 0, 'blank': 0}
    for rep in reports:
        totals['lines'] += len([e for e in rep['lines'] if not e.get('blank')])
        totals['blank'] += len([e for e in rep['lines'] if e.get('blank')])
        for key in ('pass', 'fail', 'incomplete', 'unsupported'):
            totals[key] += rep['counts'].get(key, 0)
    return {
        'schema': schema.SCHEMA_VERSION,
        'verifier': {'name': 'verify-evidence.py', 'path': os.path.abspath(__file__),
                     'schemaModule': os.path.join(HERE, 'evidence_schema.py'),
                     'python': sys.executable, 'checkDisk': bool(check_disk)},
        'files': reports,
        'duplicateIds': dups,
        'totals': totals,
        'verdict': status.upper(),
        'exitCode': {'pass': EXIT_PASS, 'fail': EXIT_FAIL, 'incomplete': EXIT_INCOMPLETE,
                     'unsupported': EXIT_UNSUPPORTED}[status],
    }


def print_text(report, quiet=False):
    lines = ['verdict=%s exit=%d' % (report['verdict'], report['exitCode'])]
    for rep in report['files']:
        c = rep['counts']
        n = len([e for e in rep['lines'] if not e.get('blank')])
        lines.append('%s  bytes=%d sha=%s lines=%d pass=%d fail=%d incomplete=%d unsupported=%d'
                     % (rep['path'], rep['bytes'], rep['sha256'][:12], n,
                        c['pass'], c['fail'], c['incomplete'], c['unsupported']))
        if rep.get('lastLineUnterminated'):
            lines.append('  ! 最后一行没有换行：可能被硬中断截断（半行）')
        if rep['nulBytes']:
            lines.append('  ! 含 %d 个 NUL 字节' % rep['nulBytes'])
        if rep['crlfLines']:
            lines.append('  ! %d 行以 CRLF 结尾（字节事实，未归一化）' % rep['crlfLines'])
        printed = 0
        for entry in rep['lines']:
            for issue in entry['issues']:
                if issue['severity'] == schema.SEVERITY_WARN:
                    continue
                if printed >= MAX_PRINTED_ISSUES:
                    lines.append('  ... 其余问题省略（见 --json 完整输出）')
                    break
                lines.append('  line %d [%s] %s: %s'
                             % (entry['line'], entry['status'], issue['code'], issue['detail']))
                printed += 1
    for dup in report['duplicateIds']:
        lines.append('  duplicate-id %s -> %s' % (dup['id'], ', '.join(dup['where'])))
    if not quiet:
        sys.stdout.write('\n'.join(lines) + '\n')
    return lines


def write_json(path, report):
    if path == '-':
        sys.stdout.write(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + '\n')
        return
    if os.path.exists(path):
        sys.stderr.write('refusing to overwrite existing report: %s\n' % path)
        raise SystemExit(EXIT_USAGE)
    parent = os.path.dirname(os.path.abspath(path))
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, 'x', encoding='utf-8', newline='\n') as fh:
        json.dump(report, fh, ensure_ascii=False, indent=2, sort_keys=True)


def main(argv):
    ap = argparse.ArgumentParser(prog='verify-evidence.py', description='只读证据校验器')
    ap.add_argument('--jsonl', action='append', default=[], required=True,
                    help='台账 JSONL 路径（可重复）')
    ap.add_argument('--json', default=None, help='机器可读结果（`-` 表示 stdout；已存在则拒绝）')
    ap.add_argument('--no-disk', action='store_true',
                    help='不按磁盘复核 raw/result.json（只做形态与自洽检查）')
    ap.add_argument('--quiet', action='store_true', help='不打印简明文本')
    args = ap.parse_args(argv)

    missing = [p for p in args.jsonl if not os.path.isfile(p)]
    if missing:
        sys.stderr.write('no such file: %s\n' % ', '.join(missing))
        return EXIT_USAGE

    # 同一份台账传两次是**用法错误**，不是"每一行都跟自己重复"：
    # 那样会打出满屏 duplicate-id 噪音，而 verdict 反而仍是 PASS（行级 status 不翻红，
    # 因为两次的 (path,line) 定位相同、都被当成"先到先得"的那一条）。
    # 与其让"点了名却判通过"自相矛盾，不如在入口就拒绝。
    seen_paths = {}
    for p in args.jsonl:
        key = os.path.normcase(os.path.realpath(p))
        if key in seen_paths:
            sys.stderr.write('usage error: 同一份台账传了多次：%s（另一次写作 %s）\n'
                             % (p, seen_paths[key]))
            return EXIT_USAGE
        seen_paths[key] = p

    report = build_report(args.jsonl, check_disk=not args.no_disk)
    print_text(report, quiet=args.quiet)
    if args.json:
        write_json(args.json, report)
    return report['exitCode']


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))

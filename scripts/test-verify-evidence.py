# -*- coding: utf-8 -*-
r"""R8-C 证据校验器（`verify-evidence.py` / `evidence_schema.py`）的分支毒化测试。

每条毒化都是**自己造出来的坏记录**，断言"必须判红且给出对应 code"；
另有一条健康对照必须判绿——否则毒化测试本身就是恒真的空判据。

覆盖：缺失字段 / 类型错（含 bool 冒充 int）/ 重复 id / raw 字节与 SHA 被改 /
raw 文件丢失 / 半行 / CRLF / 非 UTF-8 / 未知 schema / 历史 schema（unsupported）/
rc 来源自相矛盾 / 时间倒序 / elapsedSec 未知 / unknown 形态夹带事实 /
recordOk=False / phase=prepared / 业务自相矛盾 / 空文件 / 全 unsupported。

用法：
  python test-verify-evidence.py --work <工作根目录>
每次调用在工作根下建立独占子目录，旧报告、raw与用户文件不覆盖或删除。
输出：单行 JSON 汇总 + 失败明细；退出码 0 全绿 / 1 有红。
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import traceback
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import evidence_schema as schema  # noqa: E402

VERIFIER = os.path.join(HERE, 'verify-evidence.py')

ROWS = []


def check(name, cond, observed=''):
    ok = bool(cond)
    ROWS.append({'check': name, 'ok': ok, 'state': 'pass' if ok else 'fail', 'observed': observed})
    if not ok:
        print('FAIL %-46s %s' % (name, observed))
    return ok


def check_eq(name, actual, expected):
    return check(name, actual == expected, 'got=%r want=%r' % (actual, expected))


# --------------------------------------------------------------------------- 造记录


def write_file(path, data):
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, 'wb') as fh:
        fh.write(data)
    return path


def healthy(work, rid, **over):
    """一条**健康**记录：raw/result.json 都在磁盘上，字节与 SHA 自洽。"""
    case = os.path.join(work, 'cases', rid)
    os.makedirs(case, exist_ok=True)
    out = write_file(os.path.join(case, 'stdout.raw'), b'hello\n')
    err = write_file(os.path.join(case, 'stderr.raw'), b'')
    result = write_file(os.path.join(case, 'result.json'), b'{}')
    planned = write_file(os.path.join(case, 'planned.json'), b'{}')
    rec = {
        'schema': schema.SCHEMA_VERSION,
        'id': rid,
        'phase': schema.PHASE_COMPLETED,
        'recordOk': True,
        'recordReasons': [],
        'argv': ['python', '-c', 'print(1)'],
        'cwd': work,
        'plannedPath': planned,
        'caseDir': case,
        'resultPath': result,
        'pid': 4242,
        'pidCreationTimeUtc': '2026-11-02T00:00:00.000+00:00',
        'startedUtc': '2026-11-02T00:00:01.000+00:00',
        'endedUtc': '2026-11-02T00:00:02.000+00:00',
        'elapsedSec': 1.0,
        'started': True,
        'rc': 0,
        'rcSource': schema.RC_SOURCE_OBSERVED,
        'timedOut': False,
        'stillRunning': False,
        'containment': 'win-job',
        'contained': True,
        'activeAfter': 0,
        'activeKnown': True,
        'closeErrors': [],
        'cleanupError': None,
        'failClosed': False,
        'infraOk': True,
        'infraReasons': [],
        'raw': {
            'stdout': {'path': out, 'bytes': len(b'hello\n'),
                       'sha256': hashlib.sha256(b'hello\n').hexdigest()},
            'stderr': {'path': err, 'bytes': 0, 'sha256': hashlib.sha256(b'').hexdigest()},
        },
        'business': {'pass': True, 'expectRc': 0, 'expectTimedOut': False,
                     'observedRc': 0, 'observedTimedOut': False, 'started': True},
        'env': {'allowlist': {'CGO_ENABLED': '0'}, 'overrides': {}, 'digest': 'x'},
        'lock': {'mode': 'none', 'acquired': False, 'released': None},
        'tool': {'name': 'record-command.py', 'pid': 1},
    }
    rec.update(over)
    write_helper_result(rec)
    return rec


def write_helper_result(rec):
    """Synthetic helper facts must be complete; an empty {} is not healthy evidence."""
    keys = ('argv', 'cwd', 'pid', 'pidCreationTimeUtc', 'startedUtc', 'endedUtc',
            'elapsedSec', 'started', 'rc', 'rcSource', 'timedOut', 'stillRunning',
            'containment', 'contained', 'activeAfter', 'activeKnown', 'closeErrors',
            'cleanupError', 'failClosed')
    result = {key: rec[key] for key in keys}
    result.update(format='r6c-2', caseId='c1', caseDir=rec['caseDir'],
                  resultPath=rec['resultPath'], ok=rec['infraOk'],
                  okReasons=rec['infraReasons'], synthetic=True,
                  stdout=rec['raw']['stdout'], stderr=rec['raw']['stderr'])
    write_file(rec['resultPath'], json.dumps(result).encode('utf-8'))


def fresh_path(path):
    """Reserve an exclusive report directory; never delete an existing report.

    A supplied path is a naming hint, not proof of ownership of its current bytes.
    The report file itself remains absent so the verifier can create it exclusively.
    """
    parent = os.path.dirname(os.path.abspath(path))
    os.makedirs(parent, exist_ok=True)
    owned = tempfile.mkdtemp(prefix='report-', dir=parent)
    return os.path.join(owned, os.path.basename(path))


def line_of(obj):
    return json.dumps(obj, ensure_ascii=False, sort_keys=True,
                      separators=(',', ':')).encode('utf-8')


def write_jsonl(path, lines):
    """`lines` 里的元素可以是 bytes（原样）或 dict（序列化）。"""
    buf = b''
    for item in lines:
        buf += (item if isinstance(item, bytes) else line_of(item)) + b'\n'
    return write_file(path, buf)


def status_and_codes(obj, check_disk=True):
    status, issues = schema.validate_full(obj, check_disk=check_disk)
    return status, [i['code'] for i in issues]


def load_verifier_module():
    """按路径加载 `verify-evidence.py`（文件名带连字符，不能直接 `import`）。

    专为 **direct API** 回归服务：CLI 入口能拒绝"同一份台账传两次"，
    但库调用**绕不过** CLI 那一层，所以判据必须在库这一层也成立。
    """
    import importlib.util
    spec = importlib.util.spec_from_file_location('verify_evidence_direct', VERIFIER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run_verifier(paths, extra=None):
    argv = [sys.executable, VERIFIER]
    for p in paths:
        argv += ['--jsonl', p]
    argv += extra or []
    proc = subprocess.run(argv, cwd=HERE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return proc.returncode, proc.stdout.decode('utf-8', 'replace'), proc.stderr.decode('utf-8', 'replace')


# --------------------------------------------------------------------------- 用例


def test_healthy_control(work):
    rec = healthy(work, 'ok-1')
    status, codes = status_and_codes(rec)
    check_eq('healthy.status', status, schema.STATUS_PASS)
    check_eq('healthy.codes', codes, [])
    p = write_jsonl(os.path.join(work, 'ok.jsonl'), [rec])
    rc, out, err = run_verifier([p])
    check_eq('healthy.exit-code', rc, 0)
    check('healthy.verdict-text', 'verdict=PASS' in out, 'out=%r err=%r' % (out[:120], err[:200]))


# 每条毒化：(名字, 怎么改, 期望 status, 必须命中的 code)
POISONS = [
    ('missing-field', lambda r: r.pop('rcSource'), schema.STATUS_FAIL, 'missing-field'),
    ('missing-raw', lambda r: r.pop('raw'), schema.STATUS_FAIL, 'missing-field'),
    ('rc-string', lambda r: r.__setitem__('rc', '0'), schema.STATUS_FAIL, 'bad-type'),
    ('active-after-bool', lambda r: r.__setitem__('activeAfter', True), schema.STATUS_FAIL,
     'bad-type'),
    ('record-ok-string', lambda r: r.__setitem__('recordOk', 'true'), schema.STATUS_FAIL,
     'bad-type'),
    ('rc-null-observed', lambda r: (r.__setitem__('rc', None),
                                    r.__setitem__('rcSource', schema.RC_SOURCE_OBSERVED)),
     schema.STATUS_FAIL, 'rc-null-source-mismatch'),
    ('forced-rc-without-timeout', lambda r: r.__setitem__('rcSource',
                                                          schema.RC_SOURCE_FORCED_TIMEOUT),
     schema.STATUS_FAIL, 'forced-rc-without-timeout'),
    ('timeout-claimed-observed', lambda r: (r.__setitem__('timedOut', True),
                                            r.__setitem__('rc', 8)),
     schema.STATUS_FAIL, 'timeout-rc-source-mismatch'),
    # Windows 退出码是 32 位：999 合法，越界要用负数或 >0xFFFFFFFF 才越界
    ('rc-out-of-range', lambda r: r.__setitem__('rc', -1), schema.STATUS_FAIL, 'rc-out-of-range'),
    ('unknown-rc-source', lambda r: r.__setitem__('rcSource', 'magic'), schema.STATUS_FAIL,
     'unknown-rc-source'),
    ('not-started-but-rc', lambda r: (r.__setitem__('started', False),
                                      r.__setitem__('rc', 0)),
     schema.STATUS_FAIL, 'not-started-but-claims-rc'),
    ('not-started-but-pid', lambda r: (r.__setitem__('started', False),
                                       r.__setitem__('pid', 9)),
     schema.STATUS_FAIL, 'not-started-but-claims-pid'),
    ('started-without-pid', lambda r: r.__setitem__('pid', None), schema.STATUS_FAIL,
     'started-without-pid'),
    ('time-order', lambda r: (r.__setitem__('startedUtc', '2026-11-02T00:00:09.000+00:00'),
                              r.__setitem__('endedUtc', '2026-11-02T00:00:02.000+00:00')),
     schema.STATUS_FAIL, 'time-order'),
    ('bad-time', lambda r: r.__setitem__('endedUtc', 'not-a-time'), schema.STATUS_FAIL,
     'bad-end-time'),
    ('unknown-schema', lambda r: r.__setitem__('schema', 'r9-whatever'), schema.STATUS_FAIL,
     'unknown-schema'),
    ('bad-phase', lambda r: r.__setitem__('phase', 'done'), schema.STATUS_FAIL, 'bad-phase'),
    ('business-pass-rc-mismatch', lambda r: (r.__setitem__('rc', 3),
                                             r['business'].__setitem__('observedRc', 3),
                                             r['business'].__setitem__('pass', True)),
     schema.STATUS_FAIL, 'business-pass-rc-mismatch'),
    ('record-ok-with-reasons', lambda r: r.__setitem__('recordReasons', ['x']),
     schema.STATUS_FAIL, 'record-ok-with-reasons'),
    ('elapsed-unknown', lambda r: r.__setitem__('elapsedSec', None),
     schema.STATUS_INCOMPLETE, 'elapsed-unknown'),
    ('record-not-ok', lambda r: r.__setitem__('recordOk', False),
     schema.STATUS_INCOMPLETE, 'record-incomplete'),
    ('cleanup-error', lambda r: r.__setitem__('cleanupError', 'synthetic'),
     schema.STATUS_INCOMPLETE, 'cleanup-error'),
    ('close-errors', lambda r: r.__setitem__('closeErrors', ['job:5']),
     schema.STATUS_INCOMPLETE, 'close-errors'),
    ('not-contained', lambda r: r.__setitem__('contained', False),
     schema.STATUS_INCOMPLETE, 'not-contained'),
    ('active-unknown', lambda r: r.__setitem__('activeKnown', False),
     schema.STATUS_INCOMPLETE, 'active-count-unknown'),
    ('active-after-null', lambda r: r.__setitem__('activeAfter', None),
     schema.STATUS_INCOMPLETE, 'active-after-unknown'),
    ('refused', lambda r: (r.__setitem__('refused', True),
                           r.__setitem__('refusalReason', 'unsupported-platform')),
     schema.STATUS_INCOMPLETE, 'refused-before-launch'),
    ('timed-out', lambda r: (r.__setitem__('timedOut', True),
                             r.__setitem__('rc', 8),
                             r.__setitem__('rcSource', schema.RC_SOURCE_FORCED_TIMEOUT),
                             r.__setitem__('infraOk', False),
                             r['business'].__setitem__('observedRc', 8),
                             r['business'].__setitem__('observedTimedOut', True),
                             r['business'].__setitem__('pass', False)),
     schema.STATUS_INCOMPLETE, 'timed-out'),
    ('phase-prepared', lambda r: r.__setitem__('phase', schema.PHASE_PREPARED),
     schema.STATUS_INCOMPLETE, 'phase-prepared'),
]


def test_poisons(work):
    for name, mutate, want_status, want_code in POISONS:
        rec = healthy(work, 'pz-' + name)
        mutate(rec)
        # This matrix isolates in-memory schema rules. Disk/result disagreement
        # is exercised separately below and by test-evidence-review.py.
        status, issues = schema.validate_record(rec)
        codes = [issue['code'] for issue in issues]
        check('poison.%s.status' % name, status == want_status,
              'status=%s want=%s codes=%r' % (status, want_status, codes))
        check('poison.%s.code' % name, want_code in codes, 'codes=%r want=%r' % (codes, want_code))


def test_disk_poisons(work):
    # raw 自述与磁盘不符：SHA 被改 / 字节数被改 / 文件被删
    rec = healthy(work, 'pz-sha')
    rec['raw']['stdout']['sha256'] = '0' * 64
    status, codes = status_and_codes(rec)
    check('poison.raw-sha-mismatch', status == schema.STATUS_FAIL and 'raw-sha-mismatch' in codes,
          'status=%s codes=%r' % (status, codes))

    rec = healthy(work, 'pz-bytes')
    rec['raw']['stdout']['bytes'] = 999
    status, codes = status_and_codes(rec)
    check('poison.raw-bytes-mismatch',
          status == schema.STATUS_FAIL and 'raw-bytes-mismatch' in codes,
          'status=%s codes=%r' % (status, codes))

    rec = healthy(work, 'pz-raw-gone')
    os.unlink(rec['raw']['stdout']['path'])
    status, codes = status_and_codes(rec)
    check('poison.raw-file-missing', status == schema.STATUS_FAIL and 'raw-file-missing' in codes,
          'status=%s codes=%r' % (status, codes))

    rec = healthy(work, 'pz-result-gone')
    os.unlink(rec['resultPath'])
    status, codes = status_and_codes(rec)
    check('poison.missing-result-file',
          status == schema.STATUS_INCOMPLETE and 'missing-result-file' in codes,
          'status=%s codes=%r' % (status, codes))

    # 真改磁盘字节（改内容不改大小）：SHA 必须跟着变红
    rec = healthy(work, 'pz-tampered-content')
    path = rec['raw']['stdout']['path']
    write_file(path, b'HELLO\n')
    status, codes = status_and_codes(rec)
    check('poison.tampered-content', status == schema.STATUS_FAIL and 'raw-sha-mismatch' in codes,
          'status=%s codes=%r' % (status, codes))


def test_unknown_phase(work):
    rec = healthy(work, 'unknown-1')
    rec['phase'] = schema.PHASE_UNKNOWN
    rec['launchOutcome'] = 'unknown'
    rec['started'] = None
    rec['rc'] = None
    rec['rcSource'] = None
    rec['recordOk'] = False
    rec['business'] = {'pass': False, 'expectRc': 0, 'expectTimedOut': False,
                       'observedRc': None, 'observedTimedOut': None, 'started': None}
    for key in ('startedUtc', 'endedUtc', 'elapsedSec', 'timedOut', 'stillRunning',
                'containment', 'contained', 'activeAfter', 'activeKnown', 'closeErrors',
                'cleanupError', 'failClosed', 'infraOk', 'infraReasons', 'raw'):
        rec.pop(key, None)
    status, codes = status_and_codes(rec)
    check('unknown-phase.incomplete', status == schema.STATUS_INCOMPLETE,
          'status=%s codes=%r' % (status, codes))
    check('unknown-phase.not-pass', status != schema.STATUS_PASS, 'status=%s' % status)

    bad = dict(rec)
    bad['started'] = True
    status, codes = status_and_codes(bad)
    check('unknown-phase-claims-facts',
          status == schema.STATUS_FAIL and 'unknown-phase-claims-facts' in codes,
          'status=%s codes=%r' % (status, codes))

    bad2 = dict(rec)
    bad2['launchOutcome'] = 'completed'
    status, codes = status_and_codes(bad2)
    check('unknown-phase-without-outcome',
          status == schema.STATUS_FAIL and 'missing-launch-outcome' in codes,
          'status=%s codes=%r' % (status, codes))


def test_legacy_and_bytes(work):
    # 历史形态：不因为不匹配就叫造假
    legacy = {'seq': 1, 'slice': 'c1', 'cmd': ['python', 'x'], 'rc': 0}
    status, codes = status_and_codes(legacy)
    check_eq('legacy.r7-shape.status', status, schema.STATUS_UNSUPPORTED)
    check('legacy.r7-shape.code', 'unsupported-schema' in codes, 'codes=%r' % codes)

    legacy2 = {'format': 'r6c-2', 'caseId': 'x', 'caseDir': 'y', 'rc': 0}
    status, codes = status_and_codes(legacy2)
    check_eq('legacy.r6c2.status', status, schema.STATUS_UNSUPPORTED)

    legacy3 = {'format': 'r6c-selcase-1', 'id': 'P1', 'infraOk': True}
    status, codes = status_and_codes(legacy3)
    check_eq('legacy.selcase.status', status, schema.STATUS_UNSUPPORTED)

    # 没有 schema 也没有历史标记 → 判红（不是"认不出来就放过"）
    status, codes = status_and_codes({'id': 'x', 'rc': 0})
    check('legacy.unidentifiable-fails', status == schema.STATUS_FAIL
          and 'missing-schema' in codes, 'status=%s codes=%r' % (status, codes))

    # 字节级：半行 / CRLF / 非 UTF-8 / NUL
    p_half = write_file(os.path.join(work, 'half.jsonl'),
                        line_of(healthy(work, 'half-1'))[:40])
    rc, out, err = run_verifier([p_half])
    check_eq('bytes.half-line.exit', rc, 1)
    check('bytes.half-line.code', 'bad-json' in out, 'out=%r' % out[:200])
    half_report = fresh_path(os.path.join(work, 'half-report.json'))
    rc_h, _oh, _eh = run_verifier([p_half], ['--json', half_report])
    # 本次必须真的写出报告；否则下面 open() 读到的可能上一次的旧文件。
    check_eq('bytes.half-line.report-fresh', rc_h, 1)
    with open(half_report, encoding='utf-8') as fh:
        observed_half = json.load(fh)
    check('bytes.half-line.unterminated',
          observed_half['files'][0].get('lastLineUnterminated') is True,
          'file=%r' % observed_half['files'][0])

    good = line_of(healthy(work, 'crlf-1'))
    p_crlf = write_jsonl(os.path.join(work, 'crlf.jsonl'), [good + b'\r'])
    rc, out, err = run_verifier([p_crlf])
    check('bytes.crlf.reported', 'CRLF' in out, 'out=%r' % out[:300])
    check_eq('bytes.crlf.still-pass', rc, 0)

    p_utf8 = write_jsonl(os.path.join(work, 'utf8.jsonl'),
                         [b'{"schema":"r8c-evidence-1","id":"\x81\x30"}'])
    rc, out, err = run_verifier([p_utf8])
    check_eq('bytes.non-utf8.exit', rc, 1)
    check('bytes.non-utf8.code', 'non-utf8' in out, 'out=%r' % out[:200])

    p_nul = write_jsonl(os.path.join(work, 'nul.jsonl'),
                        [line_of(healthy(work, 'nul-1')) + b'\x00'])
    rc, out, err = run_verifier([p_nul])
    check('bytes.nul.reported', 'NUL' in out or rc == 1, 'rc=%d out=%r' % (rc, out[:200]))


def test_strict_input(work):
    """输入放宽类毒化：NaN / Infinity / 重复 JSON 键 / 无时区时间——都不能判 PASS。

    Python 的 `json` 默认接受 NaN/Infinity（不是 JSON 数字）并让重复键"后值覆盖前值"，
    这两者都会让一份自相矛盾的台账看起来合法。判据必须落在**字节**上：
    改的是落盘那一行，不是内存里的对象。
    """
    # 1) NaN：不是 JSON 数字
    line = line_of(healthy(work, 'num-nan'))
    patched = line.replace(b'"elapsedSec":1.0', b'"elapsedSec":NaN')
    check('strict.nan-patched', patched != line, 'line=%r' % line[:160])
    p = write_file(os.path.join(work, 'nan.jsonl'), patched + b'\n')
    rc, out, err = run_verifier([p])
    check_eq('strict.nan.exit', rc, 1)
    check('strict.nan.code', 'bad-json' in out, 'out=%r' % out[:300])

    # 2) Infinity：同上（NaN/Infinity/-Infinity 三个都是 parse_constant 的入口）
    line = line_of(healthy(work, 'num-inf'))
    patched = line.replace(b'"elapsedSec":1.0', b'"elapsedSec":Infinity')
    check('strict.inf-patched', patched != line, 'line=%r' % line[:160])
    p = write_file(os.path.join(work, 'inf.jsonl'), patched + b'\n')
    rc, out, err = run_verifier([p])
    check_eq('strict.inf.exit', rc, 1)
    check('strict.inf.code', 'bad-json' in out, 'out=%r' % out[:300])

    # 3) 重复 JSON 键：读的人无法判断哪个是事实
    line = line_of(healthy(work, 'num-dupkey'))
    patched = line[:-1] + b',"infraOk":true}'
    check('strict.dupkey-patched', b'"infraOk":true}' in patched, 'line=%r' % patched[-80:])
    p = write_file(os.path.join(work, 'dupkey.jsonl'), patched + b'\n')
    rc, out, err = run_verifier([p])
    check_eq('strict.dupkey.exit', rc, 1)
    check('strict.dupkey.code', 'bad-json' in out, 'out=%r' % out[:300])

    # 4) 时间戳不带时区：跨机器读不出"什么时候"，不能算有事实
    naive = healthy(work, 'no-tz')
    naive['startedUtc'] = '2026-11-02T00:00:01.000'
    naive['endedUtc'] = '2026-11-02T00:00:02.000'
    write_helper_result(naive)
    p = write_jsonl(os.path.join(work, 'notz.jsonl'), [naive])
    rc, out, err = run_verifier([p])
    check_eq('strict.no-tz.exit', rc, 1)
    check('strict.no-tz.code', 'timezone-missing' in out, 'out=%r' % out[:300])


def test_clock_granularity(work):
    """`elapsedSec` 是**单调差**（`time.monotonic()` 之差），不是墙钟差。

    系统时钟会被 NTP 调整、也会跳变，所以墙钟差与单调差不一致是**正常事实**，
    校验器不得拿 `endedUtc - startedUtc` 去核对 `elapsedSec`——那会把真实记录判红。
    单调端点（t0/t1 的绝对读数）不持久化：它们是进程内读数，跨进程/跨平台不可比。
    """
    rec = healthy(work, 'clock-skew')
    rec['startedUtc'] = '2026-11-02T00:00:01.000+00:00'
    rec['endedUtc'] = '2026-11-02T00:00:11.000+00:00'   # 墙钟差 10s
    rec['elapsedSec'] = 1.0                              # 单调差 1.0s
    write_helper_result(rec)
    status, issues = schema.validate_record(rec)
    codes = [i['code'] for i in issues]
    check_eq('clock.elapsed-not-wallclock', status, schema.STATUS_PASS)
    # 不列"已知 code 黑名单"——黑名单对新加的过度校验不敏感，等于判据有洞。
    # 直接要求：健康记录（只是墙钟差与单调差不一致）不得产生**任何** issue。
    check_eq('clock.no-wallclock-issue', codes, [])
    p = write_jsonl(os.path.join(work, 'clock.jsonl'), [rec])
    rc, out, err = run_verifier([p])
    check_eq('clock.verifier-pass', rc, 0)

    # 非负与有限仍然要判：单调差本身不能是负数或 NaN
    bad = healthy(work, 'clock-negative')
    bad['elapsedSec'] = -1.0
    write_helper_result(bad)
    status, issues = schema.validate_record(bad)
    check('clock.negative-red', 'negative-elapsed' in [i['code'] for i in issues],
          'codes=%r' % [i['code'] for i in issues])

    nan = healthy(work, 'clock-nan')
    nan['elapsedSec'] = float('nan')
    write_helper_result(nan)
    status, issues = schema.validate_record(nan)
    check('clock.nonfinite-red', 'nonfinite-elapsed' in [i['code'] for i in issues],
          'codes=%r' % [i['code'] for i in issues])


def test_aggregate(work):
    """多份 JSONL 聚合：**跨文件**才是聚合的意义所在。

    - 跨文件重复 id 必须判红，且定位要能点出**两个文件**（否则只知道"有重复"）；
    - 每份文件有独立报告（自己的路径/字节/SHA/计数），汇总跨文件累加；
    - 同一份台账传两次是**用法错误**（退出码 2），不是"每行跟自己重复"。
    """
    a = write_jsonl(os.path.join(work, 'agg-a.jsonl'), [healthy(work, 'agg-1')])
    b = write_jsonl(os.path.join(work, 'agg-b.jsonl'),
                    [healthy(work, 'agg-2'), healthy(work, 'agg-1')])  # 与 a 跨文件重复
    rep = fresh_path(os.path.join(work, 'agg.json'))
    rc, out, err = run_verifier([a, b], ['--json', rep])
    check_eq('agg.cross-file-dup-exit', rc, 1)
    check('agg.cross-file-dup-code', 'duplicate-id' in out, 'out=%r' % out[:300])
    with open(rep, encoding='utf-8') as fh:
        report = json.load(fh)
    check_eq('agg.files-count', report['totals']['files'], 2)
    check_eq('agg.lines-accumulate', report['totals']['lines'], 3)
    check_eq('agg.pass-accumulate', report['totals']['pass'], 2)
    check_eq('agg.fail-accumulate', report['totals']['fail'], 1)
    check('agg.per-file-report',
          len(report['files']) == 2
          and all(f.get('bytes') and f.get('sha256') for f in report['files'])
          and report['files'][0]['path'] != report['files'][1]['path'],
          'files=%r' % [f.get('path') for f in report['files']])
    # 定位要落在两个不同文件上（路径含盘符冒号，只能从**右侧**切行号）
    located = set()
    for dup in report.get('duplicateIds') or []:
        for where in dup.get('where') or []:
            located.add(where.rsplit(':', 1)[0])
    check_eq('agg.dup-locates-both-files', len(located), 2)

    # 同一份台账传两次 → 用法错误；拒绝时**不写**报告
    rep2 = os.path.join(work, 'agg-same.json')
    rc2, _out2, err2 = run_verifier([a, a], ['--json', rep2])
    check_eq('agg.same-file-twice-exit', rc2, 2)
    check('agg.same-file-twice-text', '同一份台账传了多次' in err2, 'stderr=%r' % err2[:200])
    check_eq('agg.same-file-twice-no-report', os.path.exists(rep2), False)

    # 换一种写法指向同一份文件（./x.jsonl）也必须识别为同一份
    alt = os.path.join(work, '.', 'agg-a.jsonl')
    rc3, _out3, err3 = run_verifier([a, alt])
    check_eq('agg.same-file-different-spelling-exit', rc3, 2)
    check('agg.same-file-different-spelling-text', '同一份台账传了多次' in err3,
          'stderr=%r' % err3[:200])


def test_direct_api_and_bool_fields(work):
    """direct API 与稳定布尔字段（R9-C 两项 review 修完后的回归）。

    重点：CLI 那一层的拒绝**不能顶替**库这一层的判据。
    """
    ve = load_verifier_module()

    # 1) direct API：同一份台账传两次。两份报告的 path 与 line **完全相同**，
    #    只按 (path, line) 认"第一条"会让两条都命中 skip → 一条都不翻红。
    a = write_jsonl(os.path.join(work, 'direct-a.jsonl'), [healthy(work, 'dir-1')])
    rep = ve.build_report([a, a], check_disk=True)
    check_eq('direct.verdict-fail', rep['verdict'], 'FAIL')
    check_eq('direct.exit-code', rep['exitCode'], 1)
    check_eq('direct.dup-found', len(rep['duplicateIds']), 1)
    # 不能用 `duplicateIds[0]['where']`：查不出重复时这里是空列表，会抛 IndexError，
    # 而**异常不是判据**——它会中断整个用例、让后面的断言（含 dup.exit）根本没跑到，
    # 看起来像"没翻红"。缺失就断言缺失，不要崩。
    dup0 = rep['duplicateIds'][0] if rep['duplicateIds'] else {}
    check_eq('direct.dup-where-count', len(dup0.get('where') or []), 2)
    check_eq('direct.totals-lines', rep['totals']['lines'], 2)
    check_eq('direct.totals-pass', rep['totals']['pass'], 1)
    check_eq('direct.totals-fail', rep['totals']['fail'], 1)
    # 不是"只在 totals 里加个数"：第二份报告那一行必须真的带上 duplicate-id 且翻红
    check_eq('direct.first-report-pass', rep['files'][0]['counts']['pass'], 1)
    check_eq('direct.second-report-fail', rep['files'][1]['counts']['fail'], 1)
    second = rep['files'][1]['lines'][0] if len(rep['files']) > 1 and rep['files'][1]['lines'] else None
    check('direct.second-line-present', second is not None, 'files=%r' % len(rep['files']))
    if second is not None:
        check_eq('direct.second-line-status', second['status'], schema.STATUS_FAIL)
        check('direct.second-line-code',
              any(i.get('code') == 'duplicate-id' for i in second['issues']),
              'issues=%r' % [i.get('code') for i in second['issues']])

    # 2) 对照：两份**不同**的健康台账不得误报重复（否则 1) 的"红"就是恒真空判据）
    b = write_jsonl(os.path.join(work, 'direct-b.jsonl'), [healthy(work, 'dir-2')])
    rep2 = ve.build_report([a, b], check_disk=True)
    check_eq('direct.distinct-verdict', rep2['verdict'], 'PASS')
    check_eq('direct.distinct-dups', rep2['duplicateIds'], [])

    # 3) lastLineUnterminated 必须是**稳定 bool**：四种情形逐项断言真实取值与真实类型，
    #    不允许用 `.get(key, False)` 把"没报告过"与"报告为假"混为一谈。
    eol_dir = os.path.join(work, 'eol')
    os.makedirs(eol_dir, exist_ok=True)
    cases = [
        ('normal', line_of(healthy(work, 'eol-normal')) + b'\n', False),
        ('empty', b'', False),
        ('no-trailing-newline', line_of(healthy(work, 'eol-notrail')), True),
        ('half-line-bad-json', b'{"schema":"r8c-evidence-1","id":"half"', True),
    ]
    for name, payload, expected in cases:
        p = write_file(os.path.join(eol_dir, '%s.jsonl' % name), payload)
        r = ve.scan_file(p, True)
        check_eq('eol.%s-key-present' % name, 'lastLineUnterminated' in r, True)
        check_eq('eol.%s-type-bool' % name, type(r.get('lastLineUnterminated')) is bool, True)
        check_eq('eol.%s-value' % name, r.get('lastLineUnterminated'), expected)


def test_file_level(work):
    # 重复 id：第二条判红
    a = healthy(work, 'dup-1')
    b = healthy(work, 'dup-2')
    b['id'] = 'dup-1'
    p = write_jsonl(os.path.join(work, 'dup.jsonl'), [a, b])
    rc, out, err = run_verifier([p])
    check_eq('dup.exit', rc, 1)
    check('dup.code', 'duplicate-id' in out, 'out=%r' % out[:300])

    # 全 unsupported → exit 4
    p = write_jsonl(os.path.join(work, 'legacy.jsonl'),
                    [{'seq': 1, 'cmd': ['x']}, {'seq': 2, 'cmd': ['y']}])
    rc, out, err = run_verifier([p])
    check_eq('all-unsupported.exit', rc, 4)

    # 只有 incomplete → exit 3
    inc = healthy(work, 'inc-1')
    inc['timedOut'] = True
    inc['rc'] = 8
    inc['rcSource'] = schema.RC_SOURCE_FORCED_TIMEOUT
    inc['infraOk'] = False
    inc['business']['pass'] = False
    inc['business']['observedRc'] = 8
    inc['business']['observedTimedOut'] = True
    write_helper_result(inc)
    p = write_jsonl(os.path.join(work, 'inc.jsonl'), [inc])
    rc, out, err = run_verifier([p])
    check_eq('only-incomplete.exit', rc, 3)

    # 空文件：不是"通过"，是没有证据
    p = write_jsonl(os.path.join(work, 'empty.jsonl'), [])
    rc, out, err = run_verifier([p])
    check('empty-file.not-pass', rc != 0, 'rc=%d out=%r' % (rc, out[:200]))

    # 混在一起：fail 优先
    p = write_jsonl(os.path.join(work, 'mixed.jsonl'), [healthy(work, 'mix-1'), inc,
                                                        {'seq': 3, 'cmd': ['z']}])
    rc, out, err = run_verifier([p])
    check_eq('mixed.incomplete-wins-over-unsupported', rc, 3)

    # --json 输出：路径已存在必须拒绝（不覆盖用户报告）
    p = write_jsonl(os.path.join(work, 'jsonout.jsonl'), [healthy(work, 'jo-1')])
    out_json = os.path.join(work, 'jo.json')
    write_file(out_json, b'PRE-EXISTING')
    rc, out, err = run_verifier([p, '--json', out_json])
    check_eq('json-out.refuses-overwrite', rc, 2)
    check_eq('json-out.bytes-intact', open(out_json, 'rb').read(), b'PRE-EXISTING')

    # 校验器**只读**：被校验的台账字节不因校验而改变
    before = hashlib.sha256(open(p, 'rb').read()).hexdigest()
    run_verifier([p])
    after = hashlib.sha256(open(p, 'rb').read()).hexdigest()
    check_eq('read-only.ledger-bytes', after, before)


def test_r7_original_if_present(work, explicit=None):
    """对 R7 作者坏台账**只复核分类，不改原件**（缺失就跳过，不伪造）。"""
    path = os.path.abspath(explicit or r'D:/Codes/Goxml-agent-rounds/2026-11-02/'
                                       r'night-product-r7/workbuddy/commands.jsonl')
    if not os.path.isfile(path):
        ROWS.append({'check': 'r7-original', 'ok': None,
                     'skipped': '原件不在本机：%s' % path})
        print('SKIP r7-original（原件不在本机）')
        return
    before = hashlib.sha256(open(path, 'rb').read()).hexdigest()
    rc, out, err = run_verifier([path])
    after = hashlib.sha256(open(path, 'rb').read()).hexdigest()
    check_eq('r7-original.exit', rc, 1)
    check_eq('r7-original.bytes-unchanged', after, before)
    # 6 行坏 JSON 必须逐行被点名
    check('r7-original.bad-lines', out.count('bad-json') >= 6,
          'bad-json 出现 %d 次' % out.count('bad-json'))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--work', required=True)
    ap.add_argument('--r7-ledger', default=None,
                    help='R7 作者台账原件路径（只读复核；不给就用默认位置，缺失则跳过）')
    args = ap.parse_args()
    work_root = os.path.abspath(args.work)
    os.makedirs(work_root, exist_ok=True)
    work = tempfile.mkdtemp(prefix='verify-evidence-', dir=work_root)

    # 逐个用例包一层：一个用例抛异常**不得**让后面的判据凭空消失。
    # 变异取证正是靠"哪些断言翻红"来判断的——被异常吃掉的那一段会伪装成"没翻红"。
    for _name, _fn in [
            ('test_healthy_control', lambda: test_healthy_control(work)),
            ('test_poisons', lambda: test_poisons(work)),
            ('test_disk_poisons', lambda: test_disk_poisons(work)),
            ('test_unknown_phase', lambda: test_unknown_phase(work)),
            ('test_legacy_and_bytes', lambda: test_legacy_and_bytes(work)),
            ('test_strict_input', lambda: test_strict_input(work)),
            ('test_clock_granularity', lambda: test_clock_granularity(work)),
            ('test_aggregate', lambda: test_aggregate(work)),
            ('test_direct_api_and_bool_fields', lambda: test_direct_api_and_bool_fields(work)),
            ('test_file_level', lambda: test_file_level(work)),
            ('test_r7_original_if_present', lambda: test_r7_original_if_present(work, args.r7_ledger))]:
        try:
            _fn()
        except Exception as exc:  # noqa: BLE001 —— 必须记下来，不能吞
            check('crash.%s' % _name, False,
                  '%s: %s | %s' % (type(exc).__name__, exc,
                                   traceback.format_exc().strip().splitlines()[-1]))

    failed = [r for r in ROWS if r['ok'] is False]
    skipped = [r for r in ROWS if r['ok'] is None]
    print(json.dumps({'status': 'PASS' if not failed else 'FAIL', 'checks': len(ROWS),
                      'failed': len(failed), 'skipped': len(skipped),
                      'workDir': work}, ensure_ascii=False))
    return 0 if not failed else 1


if __name__ == '__main__':
    sys.exit(main())

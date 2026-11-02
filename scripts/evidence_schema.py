# -*- coding: utf-8 -*-
r"""R8-C 证据台账 schema 与校验的**唯一**定义。

写入方（`record-command.py`）与校验方（`verify-evidence.py`）共用本文件，
避免"写的时候一套字段、验的时候另一套字段"的漂移——R7 台账 6 行坏 JSON
能在复核时才发现，根因之一就是没有机器可判的自有 schema。

设计取向（逐条对应 R7 复盘第 3 条"证据质量欠账"）：

  * `phase` 把"打算跑 / 已跑完 / 事后恢复 / 结果未知"**分开**，
    并且 `unknown` 形态**禁止**携带 started/rc 等事实（不把未知记成 0）；
  * `rcSource` 与 `timedOut` 必须自洽：超时那个 rc 是我们强制给的，
    不允许写成 `observed` 冒充进程的自然退出码；
  * `raw` 的 bytes/SHA 由**校验方按落盘字节重算**，不信任台账自述；
  * 历史别的 schema（R6 case result / R6 选择器 row / R7 手写台账）
    一律标 `unsupported` 并停止深校验，**不**因为不匹配就叫数据造假。

本文件只依赖标准库，不做任何写操作。
"""
import datetime
import hashlib
import os
import json
import math

SCHEMA_VERSION = 'r8c-evidence-1'

# 当前版本：本文件产出的台账行。
CURRENT_SCHEMAS = (SCHEMA_VERSION,)

# 历史/邻近形态：只用于**分类**，不做字段级判定。
# 这些形态不是本 schema 的旧版本，而是别的工具的产物；把它们判成"造假"是错的。
HISTORICAL_SCHEMAS = {
    'r6c-2': 'verify_examples_proc.py 的 case result.json（不是台账）',
    'r6c-selcase-1': 'verify_examples_selcase.py 的 case row（不是台账）',
    'r7c-ledger-1': 'R7 作者手写台账形态（无 schema 字段，按历史分类）',
}

PHASE_PREPARED = 'prepared'
PHASE_COMPLETED = 'completed'
PHASE_RECOVERED = 'recovered'
PHASE_UNKNOWN = 'unknown'
PHASES = (PHASE_PREPARED, PHASE_COMPLETED, PHASE_RECOVERED, PHASE_UNKNOWN)

# rc 来源：与 verify_examples_proc 的取值一致；新增取值必须同步这里。
RC_SOURCE_OBSERVED = 'observed'
RC_SOURCE_FORCED_TIMEOUT = 'forced-on-timeout'
RC_SOURCE_AFTER_FAILED_TERMINATE = 'observed-after-failed-terminate'
RC_SOURCE_EXIT_QUERY_FAILED = 'exit-query-failed'
RC_SOURCE_NOT_EXITED = 'not-exited'
RC_SOURCES = (RC_SOURCE_OBSERVED, RC_SOURCE_FORCED_TIMEOUT,
              RC_SOURCE_AFTER_FAILED_TERMINATE, RC_SOURCE_EXIT_QUERY_FAILED,
              RC_SOURCE_NOT_EXITED)

# 历史形态识别：只在**没有 schema 字段**时启用，且必须靠排他标记命中，
# 不做"看起来像就认"的推测（否则一条被篡改的记录可以靠长得像历史形态躲过判定）。
LEGACY_FORMAT_FIELD = 'format'          # R6 两个随仓工具的版本标记字段
LEGACY_SHAPE_HINTS = (                  # (历史标签, 必须**同时**存在的键)
    ('r7c-ledger-1', ('seq', 'cmd')),   # R7 作者手写台账：{"seq":..,"slice":..,"cmd":..}
)

# rc 允许写成 None 的来源（"没拿到退出码"）——不得与"已观测"混用。
RC_SOURCES_ALLOWING_NULL_RC = (RC_SOURCE_NOT_EXITED, RC_SOURCE_EXIT_QUERY_FAILED)
# rc 必须为整数的来源。
RC_SOURCES_REQUIRING_INT_RC = (RC_SOURCE_OBSERVED, RC_SOURCE_FORCED_TIMEOUT,
                               RC_SOURCE_AFTER_FAILED_TERMINATE)

FORCED_TIMEOUT_RC = 8

_NONE = type(None)

# 字段表：(名字, 允许类型, 需要的 phase 集合)
# `None` 出现在类型里表示"允许为空（未知）"；判空与判类型分开，不把 None 当 0。
FIELDS = (
    ('schema', (str,), PHASES),
    ('id', (str,), PHASES),
    ('phase', (str,), PHASES),
    ('recordOk', (bool,), PHASES),
    ('recordReasons', (list,), PHASES),
    ('argv', (list,), PHASES),
    ('cwd', (str,), PHASES),
    ('plannedPath', (str,), PHASES),
    ('caseDir', (str,), PHASES),
    ('resultPath', (str, _NONE), PHASES),
    ('business', (dict,), PHASES),
    ('env', (dict,), PHASES),
    ('lock', (dict,), PHASES),
    ('tool', (dict,), PHASES),
    ('pid', (int, _NONE), PHASES),
    ('pidCreationTimeUtc', (str, _NONE), PHASES),
    ('startedUtc', (str, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('endedUtc', (str, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('elapsedSec', ((int, float), _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('started', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('rc', (int, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('rcSource', (str, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('timedOut', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('stillRunning', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('containment', (str,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('contained', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('activeAfter', (int, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('activeKnown', (bool, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('closeErrors', (list,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('cleanupError', (str, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('failClosed', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('infraOk', (bool,), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('infraReasons', (list, _NONE), (PHASE_COMPLETED, PHASE_RECOVERED)),
    ('raw', (dict,), (PHASE_COMPLETED, PHASE_RECOVERED)),
)

# `unknown` 形态**必须**写成"没有事实"：这三个字段只能是 None。
UNKNOWN_MUST_BE_NONE = ('started', 'rc', 'rcSource')

# 行内判定结果的取值。
STATUS_PASS = 'pass'
STATUS_FAIL = 'fail'
STATUS_INCOMPLETE = 'incomplete'
STATUS_UNSUPPORTED = 'unsupported'

# 严重度：fail 直接判 FAIL；incomplete 判 INCOMPLETE；warn 只报告。
SEVERITY_FAIL = 'fail'
SEVERITY_INCOMPLETE = 'incomplete'
SEVERITY_WARN = 'warn'


def classify_legacy(obj):
    """认历史形态：命中返回历史标签，认不出返回 None。**不做模糊推测**。"""
    fmt = obj.get(LEGACY_FORMAT_FIELD)
    if isinstance(fmt, str) and fmt in HISTORICAL_SCHEMAS:
        return fmt
    for label, keys in LEGACY_SHAPE_HINTS:
        if all(k in obj for k in keys):
            return label
    return None


def type_ok(value, types):
    """类型判定。**bool 不是 int**：`rc=True` 之类必须判红。

    `isinstance(True, int)` 恒真，曾经让 `activeAfter=False` 被当成 0 放绿，
    这里对 bool 单独处理。
    """
    if type(value) is bool and bool not in types:
        return False
    if value is None:
        return _NONE in types
    return isinstance(value, types)


def parse_iso(text):
    """解析 ISO-8601；带 `Z` 的写法统一成 +00:00（本机 Python 版本兼容）。"""
    if not isinstance(text, str) or not text:
        return None
    s = text.strip()
    if s.endswith('Z') or s.endswith('z'):
        s = s[:-1] + '+00:00'
    try:
        return datetime.datetime.fromisoformat(s)
    except ValueError:
        return None


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def strict_json_loads(text):
    """Reject non-JSON numbers and ambiguous duplicate object members."""
    def constant(value):
        raise ValueError('non-finite JSON number: %s' % value)
    def pairs(items):
        obj = {}
        for key, value in items:
            if key in obj:
                raise ValueError('duplicate JSON key: %s' % key)
            obj[key] = value
        return obj
    return json.loads(text, parse_constant=constant, object_pairs_hook=pairs)


def _issue(code, detail, severity=SEVERITY_FAIL):
    return {'code': code, 'detail': detail, 'severity': severity}


def validate_record(obj):
    """对**一条**台账记录做纯内存校验（不碰磁盘）。

    返回 `(status, issues)`：
      status ∈ {pass, fail, incomplete, unsupported}
      issues = [{'code','detail','severity'}]

    判定顺序：先形态（能不能认），再字段（有没有/对不对），再自洽（会不会自相矛盾），
    最后完整性（有没有该有而没有的事实）。**任何一步不通过都不默认成功**。
    """
    issues = []

    if not isinstance(obj, dict):
        return STATUS_FAIL, [_issue('not-an-object', 'type=%s' % type(obj).__name__)]

    schema = obj.get('schema')
    if schema is None:
        # 没有 schema 不等于造假：先认历史形态（别的工具的版本标记 / 已知台账形状），
        # 认出来就标 unsupported 并停止深校验；认不出来才判红。
        legacy = classify_legacy(obj)
        if legacy is not None:
            return STATUS_UNSUPPORTED, [_issue('unsupported-schema',
                                               '%s：%s' % (legacy, HISTORICAL_SCHEMAS[legacy]),
                                               SEVERITY_WARN)]
        return STATUS_FAIL, [_issue('missing-schema', '没有 schema 字段，无法判定版本')]
    if not isinstance(schema, str):
        return STATUS_FAIL, [_issue('bad-schema-type', 'schema=%r' % (schema,))]
    if schema in HISTORICAL_SCHEMAS:
        return STATUS_UNSUPPORTED, [_issue('unsupported-schema',
                                           '%s：%s' % (schema, HISTORICAL_SCHEMAS[schema]),
                                           SEVERITY_WARN)]
    if schema not in CURRENT_SCHEMAS:
        return STATUS_FAIL, [_issue('unknown-schema', 'schema=%r 不是本校验器认识的当前版本 %r'
                                    % (schema, list(CURRENT_SCHEMAS)))]

    phase = obj.get('phase')
    if phase not in PHASES:
        issues.append(_issue('bad-phase', 'phase=%r not in %r' % (phase, list(PHASES))))
        # phase 都不认识，后续字段级判定无从谈起，但已足以判红。
        return STATUS_FAIL, issues

    # ---- 字段：存在性 + 类型（一次收集全部，不"遇到第一个就返回"）
    for name, types, phases in FIELDS:
        if phase not in phases:
            continue
        if name not in obj:
            issues.append(_issue('missing-field', name))
            continue
        if not type_ok(obj[name], types):
            issues.append(_issue('bad-type', '%s=%r type=%s want=%r'
                                 % (name, obj[name], type(obj[name]).__name__, types)))

    # Semantic checks must never coerce malformed values or crash while comparing them.
    if issues:
        return STATUS_FAIL, issues
    if not obj.get('id') or not obj.get('argv') or any(
            not isinstance(arg, str) or '\x00' in arg for arg in obj.get('argv', [])):
        issues.append(_issue('bad-identity-or-argv', 'nonempty id and argv strings required'))

    if phase == PHASE_PREPARED:
        # 台账里不应该出现 prepared（那是 planned.json 的形态）；
        # 出现即说明写入方没写完就落了一行。
        issues.append(_issue('phase-prepared', '台账行停留在 prepared，不是可复核证据',
                             SEVERITY_INCOMPLETE))

    if phase in (PHASE_COMPLETED, PHASE_RECOVERED):
        issues.extend(_check_execution(obj))
        issues.extend(_check_times(obj))
        issues.extend(_check_business(obj))
        if phase == PHASE_RECOVERED:
            src = obj.get('recoveredFrom')
            if not isinstance(src, str) or not src:
                issues.append(_issue('missing-recovery-source',
                                     'recovered 行必须写明 recoveredFrom'))

    if phase == PHASE_UNKNOWN:
        for name in UNKNOWN_MUST_BE_NONE:
            if obj.get(name, None) is not None:
                issues.append(_issue('unknown-phase-claims-facts',
                                     '%s=%r：结果未知的形态不得携带事实' % (name, obj.get(name))))
        if obj.get('recordOk') is not False:
            issues.append(_issue('unknown-phase-claims-ok', 'recordOk=%r' % (obj.get('recordOk'),)))
        if obj.get('launchOutcome') != 'unknown':
            issues.append(_issue('missing-launch-outcome',
                                 'launchOutcome=%r，应为 "unknown"' % (obj.get('launchOutcome'),)))

    if obj.get('recordOk') is True and obj.get('recordReasons'):
        issues.append(_issue('record-ok-with-reasons',
                             'recordOk=True 但 recordReasons=%r' % (obj.get('recordReasons'),)))
    if obj.get('recordOk') is not True:
        issues.append(_issue('record-incomplete', 'recordOk=%r reasons=%r'
                             % (obj.get('recordOk'), obj.get('recordReasons')),
                             SEVERITY_INCOMPLETE))

    return _status_of(issues), issues


def _check_execution(obj):
    """启动/退出/收口的自洽性。"""
    issues = []
    started = obj.get('started')
    rc = obj.get('rc')
    src = obj.get('rcSource')
    timed_out = obj.get('timedOut')
    pid = obj.get('pid')

    if src is not None and src not in RC_SOURCES:
        issues.append(_issue('unknown-rc-source', 'rcSource=%r not in %r' % (src, list(RC_SOURCES))))

    if started is True:
        if not (type(pid) is int and pid > 0):
            issues.append(_issue('started-without-pid', 'started=True 但 pid=%r' % (pid,)))
        identity = parse_iso(obj.get('pidCreationTimeUtc'))
        if identity is None or identity.utcoffset() is None:
            issues.append(_issue('process-identity-unknown', 'PID creation timestamp unproved',
                                 SEVERITY_INCOMPLETE))
        if rc is None:
            issues.append(_issue('exit-code-unknown', 'started process exit code unknown',
                                 SEVERITY_INCOMPLETE))
    else:
        # 没启动就不该有退出码 / pid / 超时。
        if rc is not None:
            issues.append(_issue('not-started-but-claims-rc', 'started=%r rc=%r' % (started, rc)))
        if pid is not None:
            issues.append(_issue('not-started-but-claims-pid', 'started=%r pid=%r' % (started, pid)))
        if timed_out is True:
            issues.append(_issue('not-started-but-timed-out', 'started=%r timedOut=True' % (started,)))
        issues.append(_issue('not-started', '本条没有真正启动，不能当作"跑过"',
                             SEVERITY_INCOMPLETE))

    if rc is None:
        if src is not None and src not in RC_SOURCES_ALLOWING_NULL_RC:
            issues.append(_issue('rc-null-source-mismatch',
                                 'rc=None 但 rcSource=%r（允许 %r）'
                                 % (src, list(RC_SOURCES_ALLOWING_NULL_RC))))
    else:
        if src not in RC_SOURCES_REQUIRING_INT_RC:
            issues.append(_issue('rc-int-source-mismatch',
                                 'rc=%r 但 rcSource=%r（应为 %r）'
                                 % (rc, src, list(RC_SOURCES_REQUIRING_INT_RC))))
        # Windows 上被 Ctrl+Break / 异常终止的进程退出码是 32 位（如 0xC000013A），
        # 不能用 POSIX 的 0..255 去判；只要求非负且不超 32 位。
        if not (0 <= int(rc) <= 0xFFFFFFFF):
            issues.append(_issue('rc-out-of-range', 'rc=%r' % (rc,)))

    if src == RC_SOURCE_FORCED_TIMEOUT and timed_out is not True:
        issues.append(_issue('forced-rc-without-timeout',
                             'rcSource=forced-on-timeout 但 timedOut=%r' % (timed_out,)))
    if timed_out is True and src not in (RC_SOURCE_FORCED_TIMEOUT, RC_SOURCE_NOT_EXITED, None):
        issues.append(_issue('timeout-rc-source-mismatch', 'timedOut=True rcSource=%r' % (src,)))
    if timed_out is True:
        issues.append(_issue('timed-out', '超时被终止：rc 是我们强制给的，不是自然退出码',
                             SEVERITY_INCOMPLETE))

    # 收口：未知一律 INCOMPLETE，不默认成功。
    # 只在**真的启动过**时才谈收口：没启动就没有进程树可收，此时 `not-started`
    # 已经把"证据不完整"说清楚了，再报 contained/activeAfter 只会误导排查方向。
    if obj.get('refused') is True:
        issues.append(_issue('refused-before-launch', 'refusalReason=%r' % (obj.get('refusalReason'),),
                             SEVERITY_INCOMPLETE))
    if started is True:
        for key, expected in (('stillRunning', False), ('failClosed', False), ('infraOk', True)):
            if obj.get(key) is not expected:
                issues.append(_issue('execution-not-complete', '%s=%r' % (key, obj.get(key)),
                                     SEVERITY_INCOMPLETE))
        if obj.get('infraReasons'):
            issues.append(_issue('infra-reasons', repr(obj.get('infraReasons')),
                                 SEVERITY_INCOMPLETE))
        if obj.get('contained') is not True:
            issues.append(_issue('not-contained', 'contained=%r（进程树归属未证）'
                                 % (obj.get('contained'),), SEVERITY_INCOMPLETE))
        if obj.get('activeKnown') is not True:
            issues.append(_issue('active-count-unknown', 'activeKnown=%r（树内活动进程数未知）'
                                 % (obj.get('activeKnown'),), SEVERITY_INCOMPLETE))
        if obj.get('activeAfter') is None:
            issues.append(_issue('active-after-unknown', 'activeAfter=None', SEVERITY_INCOMPLETE))
        elif type(obj.get('activeAfter')) is int and obj['activeAfter'] != 0:
            issues.append(_issue('active-after-nonzero', 'activeAfter=%r' % (obj.get('activeAfter'),),
                                 SEVERITY_INCOMPLETE))
    if obj.get('cleanupError') is not None:
        issues.append(_issue('cleanup-error', 'cleanupError=%r' % (obj.get('cleanupError'),),
                             SEVERITY_INCOMPLETE))
    if obj.get('closeErrors'):
        issues.append(_issue('close-errors', 'closeErrors=%r' % (obj.get('closeErrors'),),
                             SEVERITY_INCOMPLETE))
    return issues


def _check_times(obj):
    issues = []
    s = obj.get('startedUtc')
    e = obj.get('endedUtc')
    if s is None:
        issues.append(_issue('missing-start-time', 'startedUtc=None', SEVERITY_INCOMPLETE))
    elif parse_iso(s) is None:
        issues.append(_issue('bad-start-time', 'startedUtc=%r' % (s,)))
    if e is None:
        issues.append(_issue('missing-end-time', 'endedUtc=None', SEVERITY_INCOMPLETE))
    elif parse_iso(e) is None:
        issues.append(_issue('bad-end-time', 'endedUtc=%r' % (e,)))
    ts, te = parse_iso(s), parse_iso(e)
    if ((ts is not None and ts.utcoffset() is None) or
            (te is not None and te.utcoffset() is None)):
        issues.append(_issue('timezone-missing', 'timestamps require explicit UTC offset'))
    elif ts is not None and te is not None and te < ts:
        issues.append(_issue('time-order', 'endedUtc %s < startedUtc %s' % (e, s)))

    el = obj.get('elapsedSec')
    if el is None:
        issues.append(_issue('elapsed-unknown', 'elapsedSec=None（不补造时间、不写 0）',
                             SEVERITY_INCOMPLETE))
    elif not type_ok(el, (int, float)):
        issues.append(_issue('bad-elapsed', 'elapsedSec=%r' % (el,)))
    elif isinstance(el, float) and not math.isfinite(el):
        issues.append(_issue('nonfinite-elapsed', 'elapsedSec=%r' % (el,)))
    elif el < 0:
        issues.append(_issue('negative-elapsed', 'elapsedSec=%r' % (el,)))
    return issues


def _check_business(obj):
    """业务判据与观测值必须自洽：`pass=True` 时 rc 必须等于期望值。"""
    issues = []
    b = obj.get('business')
    if not isinstance(b, dict):
        return [_issue('bad-business', 'business=%r' % (b,))]
    if 'pass' not in b or type(b.get('pass')) is not bool:
        issues.append(_issue('bad-business-pass', 'business.pass=%r' % (b.get('pass'),)))
    if type(b.get('expectRc')) is not int:
        issues.append(_issue('bad-business-expect-rc', 'business.expectRc=%r' % (b.get('expectRc'),)))
    obs = b.get('observedRc')
    if not (obs is None or type(obs) is int):
        issues.append(_issue('bad-business-observed-rc', 'business.observedRc=%r' % (obs,)))
    if obs != obj.get('rc') or type(obs) is not type(obj.get('rc')):
        issues.append(_issue('business-observation-mismatch', 'observedRc differs from actual rc'))
    for observed, actual in (('started', 'started'), ('observedTimedOut', 'timedOut')):
        if observed in b and b[observed] is not obj.get(actual):
            issues.append(_issue('business-observation-mismatch', observed))
    if b.get('pass') is True:
        if type(b.get('expectRc')) is int and obs != b['expectRc']:
            issues.append(_issue('business-pass-contradiction',
                                 'business.pass=True 但 observedRc=%r expectRc=%r'
                                 % (obs, b['expectRc'])))
        rc = obj.get('rc')
        if type(b.get('expectRc')) is int and rc != b['expectRc']:
            issues.append(_issue('business-pass-rc-mismatch',
                                 'business.pass=True 但 rc=%r' % (rc,)))
        if obj.get('started') is not True:
            issues.append(_issue('business-pass-not-started',
                                 'business.pass=True 但 started=%r' % (obj.get('started'),)))
    return issues


def validate_raw(obj, require_files=None):
    """按**落盘字节**复核 raw 自述（bytes / sha256）；不信任记录里的数字。

    `require_files` 为 False 时只做形态检查（给离线场景用）。
    """
    issues = []
    raw = obj.get('raw')
    if not isinstance(raw, dict):
        return [_issue('raw-missing', 'raw=%r' % (raw,))]
    started = obj.get('started')
    for stream in ('stdout', 'stderr'):
        spec = raw.get(stream)
        if not isinstance(spec, dict):
            issues.append(_issue('raw-stream-missing', stream))
            continue
        path = spec.get('path')
        if not isinstance(path, str) or not path:
            issues.append(_issue('raw-path-missing', stream))
            continue
        if require_files is False:
            continue
        if not os.path.isfile(path):
            issues.append(_issue('raw-file-missing',
                                 '%s: %s' % (stream, path),
                                 SEVERITY_FAIL if started is True else SEVERITY_INCOMPLETE))
            continue
        actual_bytes = os.path.getsize(path)
        if type(spec.get('bytes')) is not int or spec.get('bytes') != actual_bytes:
            issues.append(_issue('raw-bytes-mismatch',
                                 '%s: 记录 %r 实际 %d' % (stream, spec.get('bytes'), actual_bytes)))
        sha = spec.get('sha256')
        if sha is None:
            if started is True:
                issues.append(_issue('raw-sha-missing', stream, SEVERITY_INCOMPLETE))
            continue
        if not isinstance(sha, str):
            issues.append(_issue('raw-sha-bad-type', '%s: %r' % (stream, sha)))
            continue
        real = sha256_file(path)
        if sha != real:
            issues.append(_issue('raw-sha-mismatch', '%s: 记录 %s 实际 %s'
                                 % (stream, sha[:12], real[:12])))
    return issues


def validate_result_file(obj):
    """Cross-check durable helper facts rather than accepting an arbitrary existing file."""
    path = obj.get('resultPath')
    if path is None:
        return [_issue('missing-result-path', 'resultPath=None', SEVERITY_INCOMPLETE)]
    if not isinstance(path, str) or not os.path.isfile(path):
        return [_issue('missing-result-file', str(path), SEVERITY_INCOMPLETE)]
    try:
        with open(path, 'r', encoding='utf-8') as fh:
            result = strict_json_loads(fh.read())
    except (OSError, ValueError, UnicodeError) as exc:
        return [_issue('invalid-result-file', '%s: %s' % (type(exc).__name__, exc))]
    if not isinstance(result, dict) or result.get('format') != 'r6c-2' or result.get('phase') is not None:
        return [_issue('result-not-final', 'expected a final r6c-2 helper result')]
    issues = []
    keys = ('argv', 'cwd', 'pid', 'pidCreationTimeUtc', 'startedUtc', 'endedUtc', 'elapsedSec',
            'started', 'rc', 'rcSource', 'timedOut', 'stillRunning', 'containment', 'contained',
            'activeAfter', 'activeKnown', 'closeErrors', 'cleanupError', 'failClosed')
    for key in keys:
        if key not in result or type(result[key]) is not type(obj.get(key)) or result[key] != obj.get(key):
            issues.append(_issue('result-fact-mismatch', key))
    for stream in ('stdout', 'stderr'):
        spec = result.get(stream)
        recorded = obj.get('raw', {}).get(stream)
        if not isinstance(spec, dict) or not isinstance(recorded, dict):
            issues.append(_issue('result-fact-mismatch', stream))
        else:
            for key in ('path', 'bytes', 'sha256'):
                value = recorded.get(key)
                if type(spec.get(key)) is not type(value) or spec.get(key) != value:
                    issues.append(_issue('result-fact-mismatch', '%s.%s' % (stream, key)))
    # run_capture's ok means infrastructure integrity; it is separate from the child business rc.
    if result.get('ok') is not True or result.get('okReasons'):
        issues.append(_issue('result-infra-incomplete', 'helper infrastructure not proved',
                             SEVERITY_INCOMPLETE))
    return issues


def _status_of(issues):
    if any(i['severity'] == SEVERITY_FAIL for i in issues):
        return STATUS_FAIL
    if any(i['severity'] == SEVERITY_INCOMPLETE for i in issues):
        return STATUS_INCOMPLETE
    return STATUS_PASS


def validate_full(obj, check_disk=True):
    """内存校验 + 可选的磁盘复核；返回**合并后**的 (status, issues)。

    磁盘复核只针对"跑过"的形态：`unknown` 没有 raw 可谈，
    `prepared` 也不该去磁盘找结果文件。
    """
    status, issues = validate_record(obj)
    if status == STATUS_UNSUPPORTED:
        return status, issues  # 历史形态：不做字段级/磁盘级判定，也不升级成通过
    if check_disk and status in (STATUS_PASS, STATUS_INCOMPLETE) \
            and obj.get('phase') in (PHASE_COMPLETED, PHASE_RECOVERED):
        try:
            issues = list(issues) + validate_raw(obj) + validate_result_file(obj)
        except OSError as exc:
            issues = list(issues) + [_issue('disk-read-failed', str(exc))]
    elif not check_disk and status == STATUS_PASS:
        issues = list(issues) + [_issue('disk-not-checked', 'raw and helper facts unverified',
                                      SEVERITY_INCOMPLETE)]
    return _status_of(issues), issues


def combine(statuses):
    """整份文件的判定：fail 优先，其次 incomplete， unsupported 只在不掺别的时成立。"""
    if STATUS_FAIL in statuses:
        return STATUS_FAIL
    if STATUS_INCOMPLETE in statuses:
        return STATUS_INCOMPLETE
    if statuses and all(s == STATUS_UNSUPPORTED for s in statuses):
        return STATUS_UNSUPPORTED
    if STATUS_UNSUPPORTED in statuses:
        return STATUS_INCOMPLETE
    if not statuses:
        return STATUS_INCOMPLETE  # 一行都没有：不是"通过"，是没证据
    return STATUS_PASS

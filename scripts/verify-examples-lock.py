# -*- coding: utf-8 -*-
r"""verify-examples.ps1 的锁助手：**仓库自带**实现，只依赖标准库。

用法（输出均为单行 JSON，便于 PowerShell 用 ConvertFrom-Json 解析）：

  python verify-examples-lock.py acquire [--lock-path P] [--run-id ID] [--agent A] [--task T]
      # 成功 rc 0；锁被他人持有 rc 77
  python verify-examples-lock.py release <token> [--lock-path P]
      # 释放成功 rc 0；token 不匹配或锁不存在 rc 78
  python verify-examples-lock.py status [--lock-path P]
      # rc 0

协议（与历史采集器 `wcollector.proclock` 一致，见下"来源"）：
  1. 原子排他：os.open(O_CREAT|O_EXCL) 创建锁文件，先到先得；
  2. 只释放自己：release 比对文件里的 token，不匹配就拒绝删除；
  3. 不按 mtime 判锁可删：本实现从不因"够旧"而删除任何锁；
  4. hostIdentity 用真实 pid + 进程创建时间，用于事后核验归属，不用于自动删除。

来源说明：语义与协议沿用本项目夜间采集器的 `wcollector.proclock`（原子排他、
只释放自己 token、不以 mtime 删锁、pid+createFiletime 身份）。本文件是**为随仓
可迁移而重写的最小实现**：只取锁协议本身，不引入采集器的其它内容（不拷贝采集器、
缓存、产物），也**不再 import 任何仓库之外的路径**。

默认锁路径：**仓库内** `<仓库根>/tmp/verify-examples/heavy.lock`，由本文件自身位置
推导（本文件在 <仓库根>/scripts/ 下）。在另一台机器上 checkout 后即可直接使用，
不需要历史目录。需要共用夜间公共锁时请**显式**传 `--lock-path`。
"""
import json
import os
import sys
import time
import uuid

LOCK_IMPL = os.path.abspath(__file__)


def default_lock_path():
    """仓库内默认锁路径：<仓库根>/tmp/verify-examples/heavy.lock。"""
    repo = os.path.dirname(os.path.dirname(LOCK_IMPL))
    return os.path.join(repo, 'tmp', 'verify-examples', 'heavy.lock')


# --------------------------------------------------------------------------- 身份


def _creation_filetime_windows(pid=None):
    """Windows 进程创建时间（FILETIME，100ns ticks）。失败返回 None。"""
    try:
        import ctypes

        class FILETIME(ctypes.Structure):
            _fields_ = [('dwLowDateTime', ctypes.c_uint32),
                        ('dwHighDateTime', ctypes.c_uint32)]

        k32 = ctypes.WinDLL('kernel32', use_last_error=True)
        k32.GetCurrentProcess.restype = ctypes.c_void_p
        k32.OpenProcess.argtypes = [ctypes.c_uint32, ctypes.c_int32, ctypes.c_uint32]
        k32.OpenProcess.restype = ctypes.c_void_p
        k32.CloseHandle.argtypes = [ctypes.c_void_p]
        k32.CloseHandle.restype = ctypes.c_int32
        k32.GetProcessTimes.argtypes = [ctypes.c_void_p] + [ctypes.POINTER(FILETIME)] * 4
        k32.GetProcessTimes.restype = ctypes.c_int32
        created = FILETIME()
        exited = FILETIME()
        kernel = FILETIME()
        user = FILETIME()
        handle = k32.GetCurrentProcess() if pid is None else k32.OpenProcess(0x1000, False, pid)
        if not handle:
            return None
        try:
            ok = k32.GetProcessTimes(handle, ctypes.byref(created), ctypes.byref(exited),
                                     ctypes.byref(kernel), ctypes.byref(user))
        finally:
            if pid is not None:
                k32.CloseHandle(handle)
        if not ok:
            return None
        return (created.dwHighDateTime << 32) | created.dwLowDateTime
    except Exception:
        return None


def _creation_filetime_posix():
    try:
        with open('/proc/self/stat', 'r') as fh:
            data = fh.read()
        # 字段 22（starttime）在 comm 字段的最后一个 ')' 之后的第 20 个字段。
        rest = data.rsplit(')', 1)[1].split()
        return int(rest[19])
    except Exception:
        return None


def host_identity():
    """真实 pid + 进程创建时间；取不到创建时间时降级为 pid-only。

    `identityStrength` 会写进锁文件：pid+createFiletime 表示身份足够强，
    pid-only 表示本机 API 不可用（仅提示，不影响排他语义）。
    """
    pid = os.getpid()
    created = _creation_filetime_windows()
    if created is None:
        created = _creation_filetime_posix()
    return {
        'pid': pid,
        'createFiletime': created,
        'identityStrength': 'pid+createFiletime' if created is not None else 'pid-only',
    }


def pid_alive(pid):
    """仅用于报告"持有者进程是否还在"（staleCandidate 提示），**不用于删锁**。"""
    try:
        if not isinstance(pid, int) or pid <= 0:
            return False
        if os.name == 'nt':
            import ctypes
            k32 = ctypes.WinDLL('kernel32', use_last_error=True)
            k32.OpenProcess.argtypes = [ctypes.c_uint32, ctypes.c_int32, ctypes.c_uint32]
            k32.OpenProcess.restype = ctypes.c_void_p
            k32.GetExitCodeProcess.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_uint32)]
            k32.GetExitCodeProcess.restype = ctypes.c_int32
            k32.CloseHandle.argtypes = [ctypes.c_void_p]
            k32.CloseHandle.restype = ctypes.c_int32
            PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
            handle = k32.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
            if not handle:
                return False
            try:
                code = ctypes.c_uint32()
                ok = k32.GetExitCodeProcess(handle, ctypes.byref(code))
                return bool(ok) and code.value == 259  # STILL_ACTIVE
            finally:
                k32.CloseHandle(handle)
        os.kill(pid, 0)
        return True
    except Exception:
        return False


# --------------------------------------------------------------------------- 锁


class LockHeldError(RuntimeError):
    def __init__(self, path, holder, stale_candidate):
        RuntimeError.__init__(self, 'lock held: %s' % path)
        self.path = path
        self.holder = holder
        self.stale_candidate = stale_candidate


class Lock(object):
    """原子排他锁；只释放 token 属于自己的锁。"""

    def __init__(self, path):
        self.path = path
        self.token = None
        self.holder = None
        self.acquired = False
        self.release_error = ''

    def _payload(self, agent, task, run_id):
        payload = {
            'token': self.token,
            'agent': agent,
            'task': task,
            'runId': run_id,
            'hostPid': os.getpid(),
            'hostIdentity': host_identity(),
            'acquiredAt': time.strftime('%Y-%m-%dT%H:%M:%S%z'),
            'acquiredAtEpoch': time.time(),
        }
        return payload

    def acquire(self, agent, task, run_id):
        if self.acquired:
            return self.holder
        self.token = uuid.uuid4().hex
        payload = self._payload(agent, task, run_id)
        blob = json.dumps(payload, ensure_ascii=False, indent=2)
        parent = os.path.dirname(self.path)
        if parent:
            os.makedirs(parent, exist_ok=True)
        try:
            fd = os.open(self.path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o644)
        except FileExistsError:
            holder = self.read()
            stale = False
            if holder:
                pid = holder.get('hostPid')
                stale = not pid_alive(pid)
                recorded = (holder.get('hostIdentity') or {}).get('createFiletime')
                actual = _creation_filetime_windows(pid) if os.name == 'nt' and isinstance(pid, int) and pid > 0 else None
                if recorded is not None and actual is not None and recorded != actual:
                    stale = True
            raise LockHeldError(self.path, holder, stale)
        try:
            os.write(fd, blob.encode('utf-8'))
            os.fsync(fd)
        finally:
            os.close(fd)
        self.holder = payload
        self.acquired = True
        return payload

    def release(self):
        """只释放 token 匹配的锁，返回**实测**是否真的释放。"""
        if not self.acquired:
            return False
        holder = self.read()
        if holder is None:
            self.acquired = False
            return False
        if holder.get('token') != self.token:
            self.acquired = False
            self.release_error = '锁的 token 不是本持有者的，拒绝删除'
            return False
        try:
            os.unlink(self.path)
        except FileNotFoundError:
            pass
        except OSError as exc:
            self.release_error = 'unlink failed: %s' % exc
            return False
        released = not os.path.exists(self.path)
        self.acquired = False
        return released

    def read(self):
        try:
            with open(self.path, 'r', encoding='utf-8') as fh:
                return json.load(fh)
        except (FileNotFoundError, ValueError, OSError):
            return None


# --------------------------------------------------------------------------- CLI


def emit(obj, rc):
    print(json.dumps(obj, ensure_ascii=True))
    return rc


def parse_args(argv):
    """剥离 --lock-path / --run-id / --agent / --task，其余按位置参数返回。"""
    opts = {'lock_path': None, 'run_id': None, 'agent': 'workbuddy',
            'task': 'verify-examples'}
    rest = []
    i = 0
    while i < len(argv):
        key = argv[i]
        if key in ('--lock-path', '--run-id', '--agent', '--task') and i + 1 < len(argv):
            name = key[2:].replace('-', '_')
            opts[name] = argv[i + 1]
            i += 2
            continue
        rest.append(argv[i])
        i += 1
    if opts['lock_path'] is None:
        opts['lock_path'] = default_lock_path()
    return opts, rest


def main(argv):
    if not argv:
        return emit({'ok': False, 'error': 'usage: acquire|release|status'}, 2)
    opts, rest = parse_args(argv)
    if not rest:
        return emit({'ok': False, 'error': 'missing action'}, 2)

    action = rest[0]
    lock = Lock(opts['lock_path'])
    base = {'path': lock.path, 'lockImpl': LOCK_IMPL}

    if action == 'status':
        holder = lock.read()
        out = dict(base)
        out.update({'ok': True, 'exists': holder is not None, 'holder': holder})
        return emit(out, 0)

    if action == 'acquire':
        run_id = opts['run_id'] or (len(rest) > 1 and rest[1]) or \
            ('verify-examples-' + uuid.uuid4().hex[:8])
        try:
            holder = lock.acquire(agent=opts['agent'], task=opts['task'], run_id=run_id)
        except LockHeldError as exc:
            out = dict(base)
            out.update({'ok': False, 'busy': True, 'holder': exc.holder,
                        'staleCandidate': exc.stale_candidate})
            return emit(out, 77)
        out = dict(base)
        out.update({'ok': True, 'acquired': True, 'token': holder['token'],
                    'runId': holder['runId'], 'hostPid': holder['hostPid'],
                    'identityStrength': holder['hostIdentity']['identityStrength']})
        return emit(out, 0)

    if action == 'release':
        if len(rest) < 2:
            return emit({'ok': False, 'error': 'release requires token'}, 2)
        token = rest[1]
        lock.token = token
        lock.acquired = True
        released = lock.release()
        out = dict(base)
        out.update({'ok': released, 'released': released, 'token': token,
                    'exists': os.path.exists(lock.path), 'error': lock.release_error})
        return emit(out, 0 if released else 78)

    return emit({'ok': False, 'error': 'unknown action %s' % action}, 2)


if __name__ == '__main__':
    try:
        rc = main(sys.argv[1:])
    except OSError as exc:
        rc = emit({'ok': False, 'error': str(exc)}, 2)
    raise SystemExit(rc)

"""Independent regression: repeated selfchecks preserve all prior evidence bytes."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('ownership_selfcheck', HERE / 'test-verify-evidence.py')
selfcheck = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selfcheck)


def snapshot(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob('*') if p.is_file()}


class EvidenceOutputOwnership(unittest.TestCase):
    def test_fresh_report_preserves_foreign_and_first_failure(self):
        with tempfile.TemporaryDirectory(prefix='evidence-ownership-') as td:
            root = Path(td)
            foreign = root / 'agg.json'
            foreign.write_bytes(b'USER-REPORT-UNOWNED')
            first = Path(selfcheck.fresh_path(str(foreign)))
            self.assertFalse(first.exists())
            first.write_bytes(b'{"verdict":"FAIL","firstFailure":true}')
            before = snapshot(root)
            second = Path(selfcheck.fresh_path(str(foreign)))
            self.assertNotEqual(first, second)
            self.assertFalse(second.exists())
            second.write_bytes(b'{"verdict":"PASS"}')
            after = snapshot(root)
            self.assertTrue(all(after.get(n) == h for n, h in before.items()))
            self.assertEqual(foreign.read_bytes(), b'USER-REPORT-UNOWNED')

    def test_same_work_root_two_real_runs_preserve_old_bytes(self):
        with tempfile.TemporaryDirectory(prefix='evidence-repeat-') as td:
            root = Path(td)
            (root / 'half-report.json').write_bytes(b'USER-HALF-REPORT')
            (root / 'agg.json').write_bytes(b'USER-AGG-REPORT')
            env = dict(os.environ, PYTHONDONTWRITEBYTECODE='1', PYTHONIOENCODING='utf-8')
            dirs = []
            before = snapshot(root)
            for _ in range(2):
                p = subprocess.run([sys.executable, str(HERE / 'test-verify-evidence.py'),
                                    '--work', str(root)], capture_output=True, env=env,
                                   cwd=str(HERE), timeout=60)
                self.assertEqual(p.returncode, 0, (p.stdout, p.stderr))
                result = json.loads(p.stdout.decode('utf-8').splitlines()[-1])
                self.assertEqual(result['status'], 'PASS')
                self.assertEqual(result['failed'], 0)
                self.assertGreaterEqual(result['checks'], 152)
                dirs.append(result['workDir'])
                after = snapshot(root)
                self.assertTrue(all(after.get(n) == h for n, h in before.items()))
                before = after
            self.assertNotEqual(dirs[0], dirs[1])
            self.assertEqual((root / 'agg.json').read_bytes(), b'USER-AGG-REPORT')
            self.assertEqual((root / 'half-report.json').read_bytes(), b'USER-HALF-REPORT')


if __name__ == '__main__':
    unittest.main()

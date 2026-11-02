"""Focused regression checks for R6 independent review findings; no real child launch."""
import importlib.util
import contextlib
import io
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPTS))
import verify_examples_proc as proc
import verify_examples_proctree as tree

def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

consumer = load('review_consumer_fixture', 'test-verify-examples-proc-consumer.py')

class ReviewRegressions(unittest.TestCase):
    def test_timeout_finite_dword_boundaries(self):
        for val in (False, True, 0, -1, float('inf'), float('nan'), 1e308,
                    10 ** 400, 4294967.295, 4294967.296):
            with self.subTest(value=str(val)):
                self.assertFalse(tree.validate_timeout(val)[0])
        self.assertEqual(tree.validate_timeout(4294967.294)[:2], (True, 0xFFFFFFFE))
        self.assertEqual(tree.validate_timeout(1800)[:2], (True, 1800000))

    def test_invalid_timeout_cannot_create_job_or_process(self):
        fake = consumer.FakeK32()
        with patch.object(tree, '_k32', fake):
            for val in (True, 4294967.295, 4294967.296, 1e308):
                result = {'failClosed': False}
                tree.run(result, ['never.exe'], '.', {}, 'unused.out', 'unused.err', val)
                self.assertTrue(result['failClosed'])
        self.assertEqual(fake.calls['CreateJobObjectW'], 0)
        self.assertEqual(fake.calls['CreateProcessW'], 0)

    def test_identity_unknown_refuses_resume_and_green(self):
        with tempfile.TemporaryDirectory() as directory:
            consumer.WORK = directory
            fake = consumer.FakeK32(identity_ok=False)
            with patch.object(tree, '_k32', fake), patch.object(fake, 'ResumeThread', wraps=fake.ResumeThread) as resume:
                result = consumer.run_tree(tree, fake)
                self.assertFalse(result['ok'])
                self.assertTrue(result['failClosed'])
                self.assertIsNone(result.get('pidCreationTimeUtc'))
                self.assertEqual(resume.call_count, 0)
                self.assertEqual(fake.calls['TerminateJobObject'], 1)

    def test_identity_known_control(self):
        with tempfile.TemporaryDirectory() as directory:
            consumer.WORK = directory
            fake = consumer.FakeK32()
            with patch.object(tree, '_k32', fake):
                result = consumer.run_tree(tree, fake)
                self.assertTrue(result['ok'], result)
                self.assertIsNotNone(result['pidCreationTimeUtc'])

    def test_existing_report_refused_before_selector_load_or_lock(self):
        for filename in ('test-verify-examples-workbench.py', 'test-verify-examples-forms.py'):
            with self.subTest(selector=filename), tempfile.TemporaryDirectory() as directory:
                path = pathlib.Path(directory) / 'report.json'
                original = b'user-owned\r\n\x00\xff'
                path.write_bytes(original)
                selector = load('selector_review_' + filename, filename)
                args = [filename, '--repo', str(SCRIPTS.parent), '--out', str(path),
                        '--lock-path', str(pathlib.Path(directory) / 'never.lock')]
                with patch.object(sys, 'argv', args), patch.object(selector, 'load_lock_helper',
                        side_effect=AssertionError('must refuse before lock')), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(selector.main(), 2)
                self.assertEqual(path.read_bytes(), original)
                self.assertEqual(sorted(p.name for p in pathlib.Path(directory).iterdir()), ['report.json'])

if __name__ == '__main__':
    unittest.main()

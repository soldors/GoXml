"""Independent adversarial checks for evidence completeness (no child processes)."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

import evidence_schema as schema

spec = importlib.util.spec_from_file_location('evidence_verifier', Path(__file__).with_name('verify-evidence.py'))
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class EvidenceReview(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='evidence-review-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.record = {
            'schema': schema.SCHEMA_VERSION, 'id': 'review', 'phase': 'completed',
            'recordOk': True, 'recordReasons': [], 'argv': ['sentinel', ''],
            'cwd': str(self.root), 'plannedPath': str(self.root / 'planned.json'),
            'caseDir': str(self.root), 'resultPath': str(self.root / 'result.json'),
            'pid': 1234, 'pidCreationTimeUtc': '2026-11-02T00:00:00+00:00',
            'startedUtc': '2026-11-02T00:00:01+00:00',
            'endedUtc': '2026-11-02T00:00:02+00:00', 'elapsedSec': 1.0,
            'started': True, 'rc': 0, 'rcSource': 'observed', 'timedOut': False,
            'stillRunning': False, 'containment': 'win-job', 'contained': True,
            'activeAfter': 0, 'activeKnown': True, 'closeErrors': [],
            'cleanupError': None, 'failClosed': False, 'infraOk': True, 'infraReasons': [],
            'business': {'pass': True, 'expectRc': 0, 'observedRc': 0},
            'env': {}, 'lock': {}, 'tool': {}, 'raw': {},
        }
        for stream, data in (('stdout', b'hello\x00\xff\r\n'), ('stderr', b'')):
            path = self.root / (stream + '.raw')
            path.write_bytes(data)
            self.record['raw'][stream] = {'path': str(path), 'bytes': len(data),
                                         'sha256': hashlib.sha256(data).hexdigest()}
        self.result = copy.deepcopy(self.record)
        self.result.update(format='r6c-2', ok=True, okReasons=[])
        self.result.pop('phase')
        self.result.update(copy.deepcopy(self.record['raw']))
        self.write_result()

    def write_result(self):
        Path(self.record['resultPath']).write_text(json.dumps(self.result), encoding='utf-8')

    def verify(self, record=None, disk=True, extra=b'', transform=None):
        data = json.dumps(self.record if record is None else record).encode('utf-8')
        if transform:
            data = transform(data)
        path = self.root / 'ledger.jsonl'
        path.write_bytes(data + b'\n' + extra)
        before = {p: p.read_bytes() for p in self.root.iterdir() if p.is_file()}
        report = verifier.build_report([str(path)], check_disk=disk)
        self.assertEqual(before, {p: p.read_bytes() for p in before})
        return report['verdict']

    def test_healthy_and_business_failure_are_valid_evidence(self):
        self.assertEqual(self.verify(), 'PASS')
        self.record['business'].update(expectRc=7, **{'pass': False})
        self.assertEqual(self.verify(), 'PASS')

    def test_infrastructure_and_identity_cannot_claim_complete(self):
        variants = [dict(stillRunning=True), dict(failClosed=True), dict(infraOk=False),
                    dict(pidCreationTimeUtc=None), dict(infraReasons=['unknown'])]
        for change in variants:
            with self.subTest(change=change):
                rec = copy.deepcopy(self.record); rec.update(change)
                self.assertNotEqual(self.verify(rec), 'PASS')
        rec = copy.deepcopy(self.record)
        rec.update(rc=None, rcSource='not-exited', business={'pass': False, 'expectRc': 0, 'observedRc': None})
        self.assertNotEqual(self.verify(rec), 'PASS')

    def test_malformed_values_are_structured_not_exceptions(self):
        for change in (dict(rc='wrong'), dict(rc=True), dict(id=[]), dict(elapsedSec=float('nan')),
                       dict(endedUtc='2026-11-02T00:00:02'), dict(activeAfter=False)):
            with self.subTest(change=change):
                rec = copy.deepcopy(self.record); rec.update(change)
                self.assertEqual(self.verify(rec), 'FAIL')

    def test_strict_json(self):
        self.assertEqual(self.verify(transform=lambda b: b[:-1] + b',"rc":0}'), 'FAIL')
        self.assertEqual(self.verify(transform=lambda b: b[:-1] + b',"anything":NaN}'), 'FAIL')

    def test_raw_and_result_are_verified_independently(self):
        for malformed in (None, [], 'not-a-stream', 1):
            with self.subTest(stream=malformed):
                rec = copy.deepcopy(self.record)
                rec['raw']['stdout'] = malformed
                self.assertEqual(self.verify(rec), 'FAIL')
        rec = copy.deepcopy(self.record); rec['raw']['stderr']['bytes'] = False
        self.assertEqual(self.verify(rec), 'FAIL')
        self.result['rc'] = 7; self.write_result()
        self.assertEqual(self.verify(), 'FAIL')
        self.result = {'phase': 'prepared'}; self.write_result()
        self.assertEqual(self.verify(), 'FAIL')

    def test_partial_verification_is_not_pass(self):
        self.assertEqual(self.verify(disk=False), 'INCOMPLETE')
        self.assertEqual(self.verify(extra=b'{"schema":"r7c-ledger-1"}\n'), 'INCOMPLETE')

    def test_business_observation_must_match_even_when_failure(self):
        self.record['business'].update(observedRc=7, **{'pass': False})
        self.assertEqual(self.verify(), 'FAIL')


if __name__ == '__main__':
    unittest.main()

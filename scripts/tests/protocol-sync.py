#!/usr/bin/env python3
"""Exercise source-contract drift detection without modifying the checkout."""
import contextlib
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('protocol_sync', ROOT / 'scripts/sync-agent-protocol.py')
sync = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sync)


class ProtocolSyncTests(unittest.TestCase):
    def setUp(self):
        runtime = ROOT.parent / 'hypercdr-runtime/validation/protocol-phase2'
        runtime.mkdir(parents=True, exist_ok=True)
        self.directory = tempfile.TemporaryDirectory(prefix='contract-sync-', dir=runtime)
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.source = self.root / 'backend/internal/protocol'
        self.target = self.root / 'agent/comm-agent/pkg/protocol'
        self.source.mkdir(parents=True)
        self.target.mkdir(parents=True)
        for name in sync.FILES:
            (self.source / name).write_text('package protocol\n')

    def run_sync(self, check):
        output = io.StringIO()
        with patch.object(sync, 'ROOT', self.root), patch('sys.argv', ['sync'] + (['--check'] if check else [])), contextlib.redirect_stderr(output):
            result = sync.main()
        return result, output.getvalue()

    def test_missing_copies_fail_without_writing(self):
        result, output = self.run_sync(True)
        self.assertEqual(result, 1)
        self.assertIn('Protocol drift', output)
        self.assertEqual(list(self.target.iterdir()), [])

    def test_sync_is_exact_and_idempotent(self):
        self.assertEqual(self.run_sync(False)[0], 0)
        self.assertEqual(self.run_sync(True)[0], 0)
        self.assertEqual(self.run_sync(False)[0], 0)
        for name in sync.FILES:
            self.assertEqual((self.source / name).read_bytes(), (self.target / name).read_bytes())

    def test_each_independent_contract_drift_is_detected(self):
        self.run_sync(False)
        for name in sync.FILES:
            with self.subTest(name=name):
                target = self.target / name
                original = target.read_bytes()
                target.write_bytes(original + b'var Drift = true\n')
                result, output = self.run_sync(True)
                self.assertEqual(result, 1)
                self.assertIn(name, output)
                self.assertNotEqual(target.read_bytes(), original)
                self.run_sync(False)
                self.assertEqual(self.run_sync(True)[0], 0)


if __name__ == '__main__':
    unittest.main()

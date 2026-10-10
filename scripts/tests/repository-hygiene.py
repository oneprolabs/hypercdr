#!/usr/bin/env python3
"""Regression tests using isolated real Git indexes, never the live repository."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
RUNTIME = ROOT.parent / 'hypercdr-runtime' / 'validation'
RUNTIME.mkdir(parents=True, exist_ok=True)
CHECKER = ROOT / 'scripts/repository-hygiene.py'


class HygieneTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='hygiene-test-', dir=RUNTIME)
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git('init', '-q')
        self.write('.gitignore', (ROOT / '.gitignore').read_bytes())

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.STDOUT)

    def write(self, name, content, stage=True):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        if stage:
            self.git('add', '-f', '--', name)
        return path

    def check(self, succeeds, message=''):
        result = subprocess.run(['python3', '-B', str(CHECKER), '--root', str(self.root)],
                                text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, succeeds, result.stdout + result.stderr)
        if message:
            self.assertIn(message, result.stdout)

    def test_source_fonts_images_and_testdata_pass(self):
        for name, content in [('app/main.go', b'package main\n'),
                              ('fonts/real.woff2', b'wOF2font'),
                              ('images/icon.png', b'\x89PNG\r\n\x1a\n'),
                              ('third_party/velero/pkg/testdata/sample.json', b'{"items":[]}'),
                              ('docs/node_modules-guide.md', b'Dependency documentation')]:
            self.write(name, content)
        self.check(True)

    def test_all_dependency_backup_variants_rejected_and_ignored(self):
        for variant in ['node_modules', 'node_modules.before-dev-20260729',
                        'node_modules_backup', 'node_modules.old']:
            with self.subTest(variant=variant):
                name = f'nested/frontend/{variant}/package.json'
                path = self.write(name, b'{}')
                self.assertEqual(self.git('check-ignore', '--no-index', '--', name).decode().strip(), name)
                self.check(False, 'Generated/dependency path')
                self.git('rm', '-f', '--', name)
                if path.parent.exists():
                    path.parent.rmdir()

    def test_untracked_dependency_backup_rejected(self):
        self.write('frontend/node_modules.backup/readme', b'cache', stage=False)
        self.check(False, 'Generated directory')

    def test_oversized_index_cannot_be_hidden_by_worktree_replacement(self):
        path = self.write('large.txt', b'x' * (1024 * 1024 + 1))
        path.write_bytes(b'small')
        self.check(False, 'Indexed file exceeds')

    def test_oversized_worktree_rejected_before_staging(self):
        path = self.write('large.txt', b'small')
        path.write_bytes(b'x' * (1024 * 1024 + 1))
        self.check(False, 'Worktree file exceeds')

    def test_compiled_extensions_rejected(self):
        for suffix in ['node', 'so', 'wasm', 'exe', 'a', 'o', 'class', 'pyc']:
            with self.subTest(suffix=suffix):
                name = 'lib/test.' + suffix
                self.write(name, b'compiled')
                self.check(False, 'Compiled output')
                self.git('rm', '-f', '--', name)

    def test_staged_binary_magic_without_extension_rejected(self):
        path = self.write('innocent-name', b'\x7fELFcompiled')
        path.write_bytes(b'harmless text')
        self.check(False, 'Compiled binary in index')

    def test_missing_worktree_does_not_hide_tracked_output(self):
        self.write('artifact.node', b'binary').unlink()
        self.check(False, 'Compiled output in index')

    def test_exact_asset_exception_and_stale_hash(self):
        content = b'\x89PNG' + b'x' * (1024 * 1024)
        asset = self.write('images/required.png', content)
        policy = {'version': 1, 'exceptions': [{'path': 'images/required.png',
            'sha256': hashlib.sha256(content).hexdigest(), 'maxBytes': len(content),
            'reason': 'Required product illustration, explicitly reviewed.'}]}
        self.write('config/repository-assets.json', json.dumps(policy).encode())
        self.check(True)
        asset.write_bytes(content[:-1] + b'y')
        self.check(False, 'digest mismatch')
        self.git('add', 'images/required.png')
        self.check(False, 'Indexed asset exception digest mismatch')

    def test_no_exception_for_compiled_or_dependency_files(self):
        for name in ['lib/example.node', 'frontend/node_modules.old/example.txt']:
            with self.subTest(name=name):
                policy = {'version': 1, 'exceptions': [{'path': name, 'sha256': '0' * 64,
                    'maxBytes': 2 * 1024 * 1024, 'reason': 'Should not bypass policy.'}]}
                self.write('config/repository-assets.json', json.dumps(policy).encode())
                self.check(False, 'cannot be exempted')

    def test_symlink_is_not_followed(self):
        target = self.root / 'external.node'
        target.write_bytes(b'\x7fELFnot repository data')
        (self.root / 'source-link').symlink_to(target)
        self.git('add', 'source-link')
        self.check(True)


if __name__ == '__main__':
    unittest.main()

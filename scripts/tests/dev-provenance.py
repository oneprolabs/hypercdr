#!/usr/bin/env python3
"""Provenance must identify actual builds, not merely the latest checkout."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
from unittest.mock import patch

root = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('provenance', root / 'scripts/dev/provenance.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
with tempfile.TemporaryDirectory() as directory:
    base = Path(directory)
    source = base / 'worktree A'
    source.mkdir()
    def git(*args):
        return subprocess.check_output(['git', '-C', str(source), *args], stderr=subprocess.DEVNULL)
    git('init')
    git('config', 'user.email', 'test@example.com')
    git('config', 'user.name', 'Test')
    (source / 'backend').mkdir()
    code = source / 'backend/main.go'
    code.write_text('package main\n')
    git('add', '.')
    git('commit', '-m', 'baseline')
    artifact = base / 'platform-api'
    artifact.write_bytes(b'built binary')
    capture = base / 'capture.json'
    capture.write_text(json.dumps(p.snapshot(source, 'api')))
    metadata = base / 'platform-api.provenance.json'
    p.record(capture, artifact, metadata)
    text = p.describe('api', 'active', artifact, source)
    assert str(source) in text and 'dirty: false' in text and 'WARNING' not in text, text
    sibling = base / 'worktree B'
    git('worktree', 'add', '-b', 'other', str(sibling))
    assert 'different worktree' in p.describe('api', 'active', artifact, sibling)
    code.write_text('package main\n// local change\n')
    assert 'source changed since build' in p.describe('api', 'active', artifact, source)
    try:
        p.record(capture, artifact, metadata)
        raise AssertionError('accepted source change during build')
    except RuntimeError:
        pass
    capture.write_text(json.dumps(p.snapshot(source, 'api')))
    p.record(capture, artifact, metadata)
    assert 'dirty: true' in p.describe('api', 'active', artifact, source)
    artifact.write_bytes(b'unrecorded replacement')
    assert 'artifact changed' in p.describe('api', 'active', artifact, source)
    assert 'no running build' in p.describe('api', 'inactive', artifact, source)
    metadata.unlink()
    assert 'source unknown' in p.describe('api', 'active', artifact, source)
    metadata.write_text('not json')
    assert 'source unknown' in p.describe('api', 'active', artifact, source)
    frontend = base / 'dist'
    frontend.mkdir()
    (frontend / 'index.html').write_text('old')
    (source / 'frontend').mkdir()
    (source / 'frontend/app.ts').write_text('source')
    capture.write_text(json.dumps(p.snapshot(source, 'frontend')))
    p.record(capture, frontend, base / 'dist.provenance.json')
    assert 'WARNING' not in p.describe('frontend', 'active', frontend, source)
    (frontend / 'index.html').write_text('new')
    assert 'artifact changed' in p.describe('frontend', 'active', frontend, source)
with patch.object(p.subprocess, 'check_output', return_value=b'{ path=/runtime with spaces/run-api.sh ; argv[]=hidden ; }'):
    assert p.running_artifact('api') == Path('/runtime with spaces/bin/platform-api')
with patch.object(p.subprocess, 'check_output', return_value=b'{ path=/unexpected/start.sh ; argv[]=hidden ; }'):
    try:
        p.running_artifact('api')
        raise AssertionError('accepted an unknown launcher')
    except ValueError:
        pass
assert p.frontend_artifact(['node', 'vite', 'preview', '--outDir', '/actual/dist'], '/other') == Path('/actual/dist')
assert p.frontend_artifact(['node', 'vite', 'preview', '--outDir=dist.next'], '/runtime') == Path('/runtime/dist.next')
assert p.frontend_artifact(['node', 'vite', 'preview'], '/runtime') == Path('/runtime/dist')
print('Development provenance: worktrees, source changes, artifact changes, dirty builds, unknown/stopped services passed')

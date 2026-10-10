#!/usr/bin/env python3
"""Keep dependency caches, compiled outputs and oversized blobs out of Git."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess

LIMIT = 1024 * 1024
COMPILED = {'.node', '.so', '.dylib', '.exe', '.a', '.o', '.obj', '.class', '.pyc', '.wasm'}
MAGIC = {b'\x7fELF', b'\x00asm', b'\xfe\xed\xfa\xce', b'\xfe\xed\xfa\xcf',
         b'\xce\xfa\xed\xfe', b'\xcf\xfa\xed\xfe', b'\xca\xfe\xba\xbe'}
GENERATED = ('frontend/dist', 'frontend/coverage', 'frontend/playwright-report',
             'frontend/test-results', 'frontend/visual-diffs', '.playwright-cli',
             'output', 'dist', '.cache')


def git(root, *args, data=None):
    return subprocess.check_output(['git', '-C', str(root), *args], input=data)


def forbidden_path(path, directory=False):
    parts = PurePosixPath(path).parts
    parts = parts if directory else parts[:-1]
    return any(p.startswith('node_modules') or p in {'.playwright-cli', '__pycache__'} for p in parts) or any(
        path == p or path.startswith(p + '/') for p in GENERATED)


def compiled(path, prefix):
    return Path(path).suffix.lower() in COMPILED or prefix[:4] in MAGIC or prefix[:2] == b'MZ'


def exceptions(root):
    path = root / 'config/repository-assets.json'
    if not path.exists():
        return {}
    doc = json.loads(path.read_text())
    if doc.get('version') != 1 or set(doc) != {'version', 'exceptions'}:
        raise ValueError('Invalid repository asset policy')
    result = {}
    for item in doc['exceptions']:
        if set(item) != {'path', 'sha256', 'maxBytes', 'reason'}:
            raise ValueError('Each exception needs path, sha256, maxBytes and reason')
        name = item['path']
        if not isinstance(name, str) or not name or name.startswith('/') or '..' in PurePosixPath(name).parts or name in result:
            raise ValueError('Exception paths must be unique repository-relative paths')
        if not re.fullmatch(r'[a-f0-9]{64}', item['sha256']) or not isinstance(item['maxBytes'], int) or item['maxBytes'] <= LIMIT or not item['reason'].strip():
            raise ValueError('Invalid asset exception digest, size or reason')
        if forbidden_path(name) or Path(name).suffix.lower() in COMPILED:
            raise ValueError('Generated files and compiled outputs cannot be exempted')
        result[name] = item
    return result


def inspect(root):
    policy = exceptions(root)
    errors = []
    # Index inspection also catches force-added/ignored files and staged binaries
    # that have subsequently been replaced with harmless worktree contents.
    entries = []
    for row in git(root, 'ls-files', '--stage', '-z').split(b'\0'):
        if not row:
            continue
        metadata, name = row.split(b'\t', 1)
        mode, oid, stage = metadata.decode().split()
        name = os.fsdecode(name)
        if stage != '0':
            errors.append(f'Unmerged index entry: {name!r}')
        if mode != '160000':
            entries.append((name, mode, oid))
    oids = sorted({oid for _, _, oid in entries})
    sizes = {}
    if oids:
        output = git(root, 'cat-file', '--batch-check=%(objectname) %(objecttype) %(objectsize)',
                     data=('\n'.join(oids) + '\n').encode())
        for row in output.decode().splitlines():
            oid, kind, size = row.split()
            if kind != 'blob':
                raise ValueError('Expected Git blob')
            sizes[oid] = int(size)
    permitted = set()
    for name, mode, oid in entries:
        size = sizes[oid]
        if forbidden_path(name, directory=mode == '120000'):
            errors.append(f'Generated/dependency path in index: {name!r}')
        if size > LIMIT and (name not in policy or size > policy[name]['maxBytes']):
            errors.append(f'Indexed file exceeds 1 MiB: {name!r} ({size} bytes)')
        else:
            permitted.add(oid)
        if Path(name).suffix.lower() in COMPILED:
            errors.append(f'Compiled output in index: {name!r}')
    # Read only size-approved blobs, never print their content or follow symlinks.
    blobs = {}
    if permitted:
        output = io.BytesIO(git(root, 'cat-file', '--batch', data=('\n'.join(sorted(permitted)) + '\n').encode()))
        while header := output.readline():
            oid, kind, size = header.decode().split()
            data = output.read(int(size))
            output.read(1)
            blobs[oid] = (data[:4], hashlib.sha256(data).hexdigest())
    indexed_names = {name for name, _, _ in entries}
    for name in policy.keys() - indexed_names:
        errors.append(f'Asset exception has no indexed file: {name!r}')
    for name, mode, oid in entries:
        if oid in blobs:
            prefix, digest = blobs[oid]
            if mode != '120000' and compiled(name, prefix):
                errors.append(f'Compiled binary in index: {name!r}')
            if name in policy and digest != policy[name]['sha256']:
                errors.append(f'Indexed asset exception digest mismatch: {name!r}')
        path = root / name
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if not stat.S_ISREG(info.st_mode):
            continue
        with path.open('rb') as stream:
            prefix = stream.read(4)
        if compiled(name, prefix):
            errors.append(f'Compiled output in worktree: {name!r}')
        exemption = policy.get(name)
        if info.st_size > LIMIT:
            if not exemption or info.st_size > exemption['maxBytes']:
                errors.append(f'Worktree file exceeds 1 MiB: {name!r} ({info.st_size} bytes)')
        if exemption and info.st_size <= exemption['maxBytes']:
            if hashlib.sha256(path.read_bytes()).hexdigest() != exemption['sha256']:
                errors.append(f'Worktree asset exception digest mismatch: {name!r}')
    # Reject dependency backup directories even when they were never staged.
    for parent, directories, _ in os.walk(root, followlinks=False):
        directories[:] = [d for d in directories if d != '.git']
        for directory in list(directories):
            relative = (Path(parent) / directory).relative_to(root).as_posix()
            if forbidden_path(relative, directory=True):
                errors.append(f'Generated directory must be outside source: {relative!r}')
                directories.remove(directory)
    for name in GENERATED:
        if (root / name).is_symlink():
            errors.append(f'Generated path must be outside source: {name!r}')
    return sorted(set(errors))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        errors = inspect(root)
    except (ValueError, KeyError, TypeError, OSError, subprocess.CalledProcessError) as exc:
        print(f'Repository hygiene check failed: {exc}')
        return 1
    for error in errors:
        print(error)
    if errors:
        return 1
    print('Repository source/runtime boundary and blob size checks passed')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

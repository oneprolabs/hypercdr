#!/usr/bin/env python3
"""Build provenance only: never read runtime configuration or execute state files."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
from datetime import datetime, timezone


def git(source, *args):
    return subprocess.check_output(['git', '-C', str(source), *args]).decode().strip()


def snapshot(source, component):
    source = Path(source).resolve()
    paths = [component if component == 'frontend' else 'backend', 'scripts', 'go.work', 'go.work.sum', 'config']
    names = subprocess.check_output(['git', '-C', str(source), 'ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', *paths]).split(b'\0')
    digest = hashlib.sha256()
    for name in sorted(set(names) - {b''}):
        path = source / name.decode()
        digest.update(name + b'\0')
        digest.update(path.read_bytes() if path.is_file() else b'<missing>')
        digest.update(str(path.stat().st_mode & 0o777).encode() if path.exists() else b'')
    return {'source': str(source), 'component': component, 'branch': git(source, 'rev-parse', '--abbrev-ref', 'HEAD'),
            'commit': git(source, 'rev-parse', 'HEAD'), 'dirty': bool(git(source, 'status', '--porcelain', '--untracked-files=normal')),
            'sourceFingerprint': digest.hexdigest()}


def artifact_digest(artifact):
    artifact = Path(artifact)
    digest = hashlib.sha256()
    paths = sorted(artifact.rglob('*')) if artifact.is_dir() else [artifact]
    for path in paths:
        if path.is_file():
            digest.update((str(path.relative_to(artifact)) if artifact.is_dir() else '').encode() + b'\0')
            digest.update(path.read_bytes())
    return digest.hexdigest()


def record(capture, artifact, destination):
    state = json.loads(Path(capture).read_text())
    if snapshot(state['source'], state['component']) != state:
        raise RuntimeError('Source changed during build; rebuild before recording provenance')
    state.update(builtAt=datetime.now(timezone.utc).isoformat(), artifactFingerprint=artifact_digest(artifact))
    destination = Path(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(destination.name + '.tmp')
    temporary.write_text(json.dumps(state, indent=2) + '\n')
    temporary.replace(destination)


def describe(service, status, artifact, source):
    prefix = f'{service}: {status}'
    if status not in ('active', 'activating'):
        return prefix + ' (no running build)'
    artifact = Path(artifact)
    metadata = artifact.with_name(artifact.name + '.provenance.json')
    try:
        state = json.loads(metadata.read_text())
        required = ('source', 'component', 'branch', 'commit', 'dirty', 'builtAt', 'sourceFingerprint', 'artifactFingerprint')
        if any(key not in state for key in required) or state['component'] != service:
            raise ValueError('invalid metadata')
        warnings = []
        if artifact_digest(artifact) != state['artifactFingerprint']:
            warnings.append('artifact changed; recorded build is unverified')
        if Path(state['source']).resolve() != Path(source).resolve():
            warnings.append('running build is from a different worktree')
        try:
            current = snapshot(state['source'], service)
            if current['sourceFingerprint'] != state['sourceFingerprint']:
                warnings.append('source changed since build; rebuild required')
        except (OSError, subprocess.CalledProcessError):
            warnings.append('build source is unavailable')
        details = f"source: {state['source']}, branch: {state['branch']}, commit: {state['commit'][:12]}, dirty: {str(state['dirty']).lower()}, built: {state['builtAt']}"
        return prefix + ' (' + details + ')' + ''.join('\n  WARNING: ' + w for w in warnings)
    except (OSError, ValueError, KeyError, TypeError):
        return prefix + ' (build source unknown; rebuild through scripts/dev/update-dev.sh)'


def frontend_artifact(argv, cwd):
    if 'preview' not in argv:
        raise ValueError('frontend is not a preview process')
    output = 'dist'
    for index, arg in enumerate(argv):
        if arg == '--outDir':
            output = argv[index + 1]
        elif arg.startswith('--outDir='):
            output = arg.split('=', 1)[1]
    path = Path(output)
    return path if path.is_absolute() else Path(cwd) / path


def running_artifact(service):
    raw = subprocess.check_output(['systemctl', 'show', f'hypercdr-dev-{service}.service', '-p', 'ExecStart', '--value'], stderr=subprocess.DEVNULL).decode()
    # Fixed launcher basename, parsed as data; never source a launcher (it contains secrets).
    match = re.search(r'path=(.*?/run-' + service + r'\.sh)(?:\s*;|\s+argv\[\]=)', raw)
    if not match:
        raise ValueError('unknown service launcher')
    runtime = Path(match.group(1)).parent
    if service == 'api':
        return runtime / 'bin/platform-api'
    pid = subprocess.check_output(['systemctl', 'show', 'hypercdr-dev-frontend.service', '-p', 'MainPID', '--value']).decode().strip()
    process = Path('/proc') / pid
    argv = [arg.decode() for arg in (process / 'cmdline').read_bytes().split(b'\0') if arg]
    return frontend_artifact(argv, (process / 'cwd').resolve())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['capture', 'record', 'status'])
    parser.add_argument('--source')
    parser.add_argument('--component', choices=['api', 'frontend'])
    parser.add_argument('--capture')
    parser.add_argument('--artifact')
    parser.add_argument('--output')
    args = parser.parse_args()
    if args.action == 'capture':
        Path(args.output).write_text(json.dumps(snapshot(args.source, args.component)))
    elif args.action == 'record':
        record(args.capture, args.artifact, args.output)
    else:
        for service in ('api', 'frontend'):
            result = subprocess.run(['systemctl', 'is-active', f'hypercdr-dev-{service}.service'], capture_output=True, text=True)
            status = result.stdout.strip() or 'unknown'
            try:
                artifact = running_artifact(service) if status in ('active', 'activating') else Path('/nonexistent')
                description = describe(service, status, artifact, args.source)
                if service == 'api' and status == 'active':
                    pid = subprocess.check_output(['systemctl', 'show', 'hypercdr-dev-api.service', '-p', 'MainPID', '--value']).decode().strip()
                    try:
                        if artifact_digest(Path('/proc') / pid / 'exe') != artifact_digest(artifact):
                            description += '\n  WARNING: running API differs from installed binary; restart required'
                    except OSError:
                        description += '\n  WARNING: running API identity could not be verified'
                print(description)
            except (OSError, ValueError, IndexError, subprocess.CalledProcessError):
                print(f'{service}: {status} (build source unknown; service launcher not recognized)')


if __name__ == '__main__':
    main()

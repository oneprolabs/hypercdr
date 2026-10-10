#!/usr/bin/env python3
"""Check documented Compose inputs and independently render source development."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / 'docker-compose.yml').read_text()
variables = set(re.findall(r'\$\{([A-Z][A-Z0-9_]*)', source))
example = (root / 'env.example').read_text()
documented = set(re.findall(r'^([A-Z][A-Z0-9_]*)=', example, re.M))
assert not variables - documented, f'Undocumented production inputs: {variables - documented}'
assert 'harbor.example.com' not in example
subprocess.run(['docker', 'compose', '--env-file', str(root / 'env.example'),
    '-f', str(root / 'docker-compose.yml'), '--profile', 'blue', '--profile', 'green',
    '--profile', 'website', 'config', '--quiet'], check=True)
runtime_root = root.parent / 'hypercdr-runtime' / 'validation'
runtime_root.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='hypercdr-compose-', dir=runtime_root) as runtime:
    env = dict(os.environ, HCDR_SOURCE_RUNTIME=runtime,
               HCDR_SOURCE_DB_PASSWORD='test', HCDR_SOURCE_SECRET_KEY='test',
               HCDR_SOURCE_RELEASE_TOKEN='test', HCDR_SOURCE_EXECUTOR_TOKEN='test')
    # Never inherit a developer .env or print rendered secrets.
    result = subprocess.check_output(['docker', 'compose', '--env-file', '/dev/null',
        '-f', str(root / 'docker-compose.source.yml'), 'config', '--format', 'json'], env=env)
    config = json.loads(result)
    services = config['services']
    assert all('build' in services[k] for k in ['hypercdr-platform-api', 'frontend', 'executor'])
    assert all('healthcheck' in s for s in services.values())
    assert not any('container_name' in s for s in services.values())
    assert services['frontend']['ports'][0]['host_ip'] == '127.0.0.1'
    assert not any('ports' in services[k] for k in ['postgres', 'executor', 'hypercdr-platform-api'])
    assert config['networks']['data']['internal']
    assert 'ingress' in services['frontend']['networks']
    assert not config['networks']['ingress'].get('internal', False)
    for service in ['hypercdr-platform-api', 'executor']:
        assert 'egress' in services[service]['networks']
    assert services['executor']['read_only']
    assert services['executor']['tmpfs'] == ['/tmp:size=32m,noexec,nosuid,nodev']
    # Missing manifests and an in-repository runtime must fail before starting containers.
    wrapper = root / 'scripts/dev/source-compose.sh'
    failure = subprocess.run([str(wrapper), 'up'], env=env, capture_output=True, text=True)
    assert failure.returncode != 0 and 'Provide a GitHub Release' in failure.stderr
    assert (Path(runtime) / 'compose.env').stat().st_mode & 0o777 == 0o600
    invalid = Path(runtime) / 'invalid-manifest.json'
    invalid.write_text(json.dumps({'version': 'test', 'componentManifest': {}}))
    failure = subprocess.run([str(wrapper), 'up', '--manifest', str(invalid)], env=env,
                             capture_output=True, text=True)
    assert failure.returncode != 0 and 'Missing cluster installer components' in failure.stderr
    unsafe_env = dict(env, HCDR_SOURCE_RUNTIME=str(root))
    failure = subprocess.run([str(wrapper), 'config'], env=unsafe_env, capture_output=True, text=True)
    assert failure.returncode != 0 and 'outside the repository' in failure.stderr
print('Deployment input and independent source Compose contracts passed')

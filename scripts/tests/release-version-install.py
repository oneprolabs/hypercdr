#!/usr/bin/env python3
"""Exercise public installer tag/asset resolution without network or Docker writes."""
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
RUNTIME = ROOT.parent / 'hypercdr-runtime' / 'validation' / 'version-scheme'

class InstallerVersions(unittest.TestCase):
    def run_installer(self, requested, version, tag):
        with tempfile.TemporaryDirectory(dir=RUNTIME) as temp:
            base = Path(temp)
            binary = base / 'bin'
            binary.mkdir()
            package = base / 'installer.tar.gz'
            marker = base / 'installed'
            with tarfile.open(package, 'w:gz') as archive:
                body = f'#!/bin/bash\nprintf "%s" "$1" > "{marker}"\n'.encode()
                info = tarfile.TarInfo(f'hypercdr-installer-{version}/install-blue-green.sh')
                info.size, info.mode = len(body), 0o755
                archive.addfile(info, io.BytesIO(body))
            fake = binary / 'curl'
            fake.write_text('''#!/usr/bin/env python3
import json, os, shutil, sys
args=sys.argv[1:]
url=args[-1]
if '-o' in args:
    shutil.copyfile(os.environ['TEST_PACKAGE'], args[args.index('-o')+1])
elif url.endswith('/tags/'+os.environ['TEST_TAG']):
    print(json.dumps({'assets':[{'name':'hypercdr-installer-'+os.environ['TEST_VERSION']+'.tar.gz','url':'https://fixture/package'}]}))
else:
    sys.exit(22)
''')
            fake.chmod(0o755)
            docker = binary / 'docker'
            docker.write_text('#!/bin/sh\nexit 0\n')
            docker.chmod(0o755)
            env = dict(os.environ, PATH=f'{binary}:'+os.environ['PATH'], TEST_PACKAGE=str(package), TEST_VERSION=version, TEST_TAG=tag)
            result = subprocess.run(['bash', str(ROOT/'deploy/online/install.sh'), '--version', requested, '--base-url', 'https://example.test', '--yes'], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
            self.assertEqual(marker.read_text(), version)

    def test_new_community_tag(self):
        self.run_installer('ce-v0.0.0', 'ce-v0.0.0', 'ce-v0.0.0')

    def test_historical_tag(self):
        self.run_installer('v1.0.117.20261014', '1.0.117.20261014', 'v1.0.117.20261014')

    def test_historical_numeric(self):
        self.run_installer('1.0.117.20261014', '1.0.117.20261014', 'v1.0.117.20261014')

if __name__ == '__main__':
    RUNTIME.mkdir(parents=True, exist_ok=True)
    unittest.main()

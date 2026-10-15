#!/usr/bin/env python3
"""Exercise release selection, identity and integrity gates without installing Docker."""
import hashlib, io, json, os, pathlib, subprocess, tarfile, tempfile
ROOT = pathlib.Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'deploy/enterprise/install.sh'
VERSION = 'v1.0.117.20261014-ent.2'
with tempfile.TemporaryDirectory(prefix='enterprise-installer-', dir=os.environ.get('HCDR_RUNTIME_ROOT')) as d:
    work = pathlib.Path(d)
    (work/'bin').mkdir()
    (work/'bin/docker').write_text('#!/bin/sh\nexit 0\n')
    (work/'bin/curl').write_text('''#!/usr/bin/env python3
import os,sys,pathlib
args=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE'])
url=next(x for x in args if x.startswith('https://'))
if 'api.github.com/' in url: sys.stdout.write((root/'releases.json').read_text())
else: pathlib.Path(args[args.index('-o')+1]).write_bytes((root/url.rsplit('/',1)[1]).read_bytes())
''')
    for p in (work/'bin').iterdir(): p.chmod(0o755)
    env = dict(os.environ, PATH=str(work/'bin')+':'+os.environ['PATH'], FIXTURE=str(work), MARKER=str(work/'installed'))
    archive = work/f'hypercdr-enterprise-installer-{VERSION}.tar.gz'
    checksum = work/f'hypercdr-enterprise-installer-{VERSION}.sha256'
    names = [archive.name, checksum.name]
    (work/'releases.json').write_text(json.dumps([
        dict(tag_name='v9.0.0.20990101',assets=[]),
        dict(tag_name='v1.0.117.20261014-ent.9',prerelease=True,assets=[{'name':n} for n in names]),
        dict(tag_name='v1.0.117.20261014-ent.8',assets=[]),
        dict(tag_name=VERSION,assets=[{'name':n} for n in names])]))
    def package(edition='enterprise', traversal=False):
        with tarfile.open(archive,'w:gz') as t:
            for name,data in [('release-manifest.json',json.dumps(dict(edition=edition,version=VERSION)).encode()),
                              ('../escape' if traversal else 'install.sh', b'#!/bin/sh\ntouch "$MARKER"\n')]:
                m=tarfile.TarInfo(name); m.size=len(data); t.addfile(m,io.BytesIO(data))
        checksum.write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
    def run(ok):
        (work/'installed').unlink(missing_ok=True)
        p=subprocess.run(['bash',str(SCRIPT),'--base-url','https://localhost:12448','--install-dir',str(work/'installation'),'--yes'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        assert (p.returncode==0)==ok, (p.stdout,p.stderr)
        assert (work/'installed').exists()==ok
    package(); run(True)
    checksum.write_text('0'*64+'  '+archive.name+'\n'); run(False)
    package(edition='community'); run(False)
    package(traversal=True); run(False)
    assert not (work/'escape').exists()
print('PASS: mixed release selection, checksum, edition identity, traversal rejection')

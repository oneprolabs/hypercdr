#!/usr/bin/env python3
import copy
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("manifest", ROOT / "scripts/release/dependency-manifest.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class DependencyManifestTests(unittest.TestCase):
    def setUp(self):
        runtime = Path(os.environ.get("HCDR_RUNTIME_ROOT", ROOT.parent / "hypercdr-runtime")) / "validation"
        runtime.mkdir(parents=True, exist_ok=True)
        self.directory = tempfile.TemporaryDirectory(dir=runtime)
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        lock = self.root / "packaging/oadp/image-lock.json"
        lock.parent.mkdir(parents=True)
        lock.write_text(json.dumps({"platform": "linux/amd64", "images": [{"component": "oadp-operator"}]}))
        self.registry = "registry.example/hypercdr"
        names = MODULE.RUNTIME_COMPONENTS | {"oadp-operator", "oadp-bundle", "oadp-catalog"}
        self.manifest = {
            "schemaVersion": 1,
            "platform": "linux/amd64",
            "registry": self.registry,
            "fingerprints": MODULE.fingerprints(self.root, self.registry),
            "images": {name: {"image": f"{self.registry}/{name}:fixed", "imageDigest": "sha256:" + "a" * 64, "version": "fixed"} for name in names},
        }

    def test_valid_and_stable(self):
        MODULE.validate(self.manifest, self.root, self.registry)
        self.assertEqual(self.manifest["fingerprints"], MODULE.fingerprints(self.root, self.registry))

    def test_invalid_manifests(self):
        for field in ["missing", "digest", "registry", "pinned", "version", "fingerprint"]:
            with self.subTest(field=field):
                candidate = copy.deepcopy(self.manifest)
                if field == "missing":
                    del candidate["images"]["velero"]
                elif field == "fingerprint":
                    candidate["fingerprints"]["velero"] = "changed"
                elif field == "digest":
                    candidate["images"]["velero"]["imageDigest"] = "invalid"
                elif field == "registry":
                    candidate["images"]["velero"]["image"] = "outside.invalid/image:fixed"
                elif field == "pinned":
                    candidate["images"]["velero"]["image"] += "@sha256:" + "b" * 64
                else:
                    candidate["images"]["velero"]["version"] = ""
                with self.assertRaises(ValueError):
                    MODULE.validate(candidate, self.root, self.registry)

    def test_change_propagation(self):
        before = MODULE.fingerprints(self.root, self.registry)
        source = self.root / "third_party/velero/main.go"
        source.parent.mkdir(parents=True)
        source.write_text("new input")
        after = MODULE.fingerprints(self.root, self.registry)
        self.assertNotEqual(before["velero"], after["velero"])
        for name in ["runtime", "oadp", "bundle", "catalog"]:
            self.assertEqual(before[name], after[name])
        source = self.root / "packaging/oadp/image-lock.json"
        source.write_text(source.read_text() + "\n")
        updated = MODULE.fingerprints(self.root, self.registry)
        for name in ["oadp", "bundle", "catalog"]:
            self.assertNotEqual(after[name], updated[name])


if __name__ == "__main__":
    unittest.main()

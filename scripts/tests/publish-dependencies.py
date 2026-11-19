#!/usr/bin/env python3
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("publisher", ROOT / "scripts/release/publish-dependencies.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class PublisherTests(unittest.TestCase):
    def setUp(self):
        runtime = Path(os.environ.get("HCDR_RUNTIME_ROOT", ROOT.parent / "hypercdr-runtime")) / "validation"
        runtime.mkdir(parents=True, exist_ok=True)
        self.directory = tempfile.TemporaryDirectory(dir=runtime)
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.registry = "registry.example/hypercdr"
        oadp = self.root / "packaging/oadp/image-lock.json"
        oadp.parent.mkdir(parents=True)
        oadp.write_text(json.dumps({"release": "1.3.10", "platform": "linux/amd64", "images": [{"component": "oadp-operator", "source": "source.example/operator:fixed", "sourceDigest": "sha256:" + "a" * 64}]}))
        runtime_lock = self.root / "packaging/dependencies/runtime-lock.json"
        runtime_lock.parent.mkdir(parents=True)
        runtime_lock.write_text(json.dumps({"velero": {"version": "1.18.2"}, "images": [
            {"component": name, "source": "source.example/" + name, "sourceDigest": "sha256:" + "a" * 64, "version": "fixed"}
            for name in sorted(MODULE.MANIFEST.RUNTIME_COMPONENTS - {"velero"})
        ]}))
        self.remote = {}
        self.operations = []
        self.runtime = self.root / "runtime"
        self.output = self.runtime / "dependency-manifest.json"

    def execute(self, arguments, env=None):
        arguments = list(map(str, arguments))
        self.operations.append(arguments)
        if "--tag" in arguments:
            target = arguments[arguments.index("--tag") + 1]
            if arguments[0] == "bash":
                target = MODULE.image_ref(self.registry, "velero", target)
        elif env and "HCDR_OADP_BUNDLE_IMAGE" in env:
            target = env["HCDR_OADP_BUNDLE_IMAGE"]
        elif env and "HCDR_OADP_CATALOG_IMAGE" in env:
            target = env["HCDR_OADP_CATALOG_IMAGE"]
        else:
            raise AssertionError(arguments)
        self.remote[target] = "sha256:" + "a" * 64

    def publish(self):
        with patch.object(MODULE, "ROOT", self.root), patch.object(MODULE, "remote_digest", side_effect=lambda image: self.remote.get(image)), patch.object(MODULE, "execute", side_effect=self.execute):
            MODULE.publish(self.registry, self.output, self.runtime)

    def test_cold_then_warm_and_component_change(self):
        self.publish()
        self.assertEqual(len(self.operations), 8)
        original = self.output.read_bytes()
        self.operations.clear()
        self.publish()
        self.assertEqual(self.operations, [])
        self.assertEqual(original, self.output.read_bytes())
        source = self.root / "third_party/velero/main.go"
        source.parent.mkdir(parents=True)
        source.write_text("changed")
        self.publish()
        self.assertEqual(len(self.operations), 1)
        self.assertIn("build-velero.sh", " ".join(self.operations[0]))

    def test_registry_failure_never_builds(self):
        with patch.object(MODULE, "ROOT", self.root), patch.object(MODULE, "remote_digest", side_effect=RuntimeError("unauthorized")), patch.object(MODULE, "execute") as operation:
            with self.assertRaises(RuntimeError):
                MODULE.publish(self.registry, self.output, self.runtime)
            operation.assert_not_called()
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()

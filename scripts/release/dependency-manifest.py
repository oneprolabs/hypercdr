#!/usr/bin/env python3
import argparse
import hashlib
import json
import re
from pathlib import Path


DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
RUNTIME_COMPONENTS = {
    "postgres", "velero", "velero-plugin-for-aws",
    "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp",
}


def fingerprints(root, registry):
    groups = {
        "velero": ["third_party/velero", "scripts/release/build-velero.sh"],
        "oadp": ["packaging/oadp/image-lock.json", "scripts/release/mirror-community-oadp-images.sh"],
        "bundle": ["packaging/oadp", "scripts/release/build-community-oadp-bundle.sh"],
        "catalog": ["scripts/release/build-community-oadp-catalog.sh"],
        "runtime": ["scripts/release/publish-runtime-images.sh", "scripts/release/sync-velero-plugins.sh"],
    }
    result = {}
    for group, inputs in groups.items():
        digest = hashlib.sha256()
        digest.update(registry.encode())
        for name in sorted(inputs + ["scripts/release/dependency-manifest.py", "scripts/release/dependency-registry.sh", "scripts/release/publish-dependencies.py", "scripts/release/common.sh", "scripts/lib/registry-config.sh"]):
            path = root / name
            files = sorted(path.rglob("*")) if path.is_dir() else [path]
            for source in files:
                if not source.is_file() or ".git" in source.relative_to(root).parts:
                    continue
                digest.update(str(source.relative_to(root)).encode() + b"\0")
                digest.update(source.read_bytes())
        result[group] = "sha256:" + digest.hexdigest()
    runtime_lock = root / "packaging/dependencies/runtime-lock.json"
    if runtime_lock.exists():
        runtime_inputs = json.loads(runtime_lock.read_text())
        result["runtime-lock"] = combined(json.dumps(runtime_inputs, sort_keys=True))
        result["velero"] = combined(result["velero"], json.dumps(runtime_inputs.get("velero", {}), sort_keys=True))
    result["bundle"] = combined(result["bundle"], result["oadp"])
    result["catalog"] = combined(result["catalog"], result["bundle"])
    return result


def combined(*values):
    return "sha256:" + hashlib.sha256("\n".join(values).encode()).hexdigest()


def validate(manifest, root, registry):
    if manifest.get("schemaVersion") != 1 or manifest.get("registry") != registry:
        raise ValueError("dependency schema or registry mismatch")
    if manifest.get("fingerprints") != fingerprints(root, registry):
        raise ValueError("dependency inputs changed; publish dependencies before platform release")
    lock = json.loads((root / "packaging/oadp/image-lock.json").read_text())
    if manifest.get("platform") != lock.get("platform"):
        raise ValueError("dependency platform mismatch")
    required = RUNTIME_COMPONENTS | {entry["component"] for entry in lock["images"]} | {"oadp-bundle", "oadp-catalog"}
    images = manifest.get("images", {})
    if not isinstance(images, dict) or set(images) != required:
        raise ValueError("dependency image set is incomplete or unexpected")
    for component, entry in images.items():
        if not isinstance(entry, dict):
            raise ValueError(f"invalid dependency entry: {component}")
        image = entry.get("image", "")
        digest = entry.get("imageDigest", "")
        if not isinstance(image, str) or not image.startswith(registry + (":" if ".aliyuncs.com/" in registry or registry.startswith("docker.io/") else "/")):
            raise ValueError(f"dependency outside selected registry: {component}")
        if not isinstance(digest, str) or not DIGEST.fullmatch(digest):
            raise ValueError(f"invalid dependency digest: {component}")
        if not isinstance(entry.get("version"), str) or not entry["version"]:
            raise ValueError(f"missing dependency version: {component}")
        if "@" in image and image.rsplit("@", 1)[1] != digest:
            raise ValueError(f"pinned reference mismatch: {component}")
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["fingerprint", "validate", "references"])
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--registry", required=True)
    parser.add_argument("--manifest", type=Path)
    arguments = parser.parse_args()
    if arguments.action == "fingerprint":
        print(json.dumps(fingerprints(arguments.root, arguments.registry), sort_keys=True))
        return
    if arguments.manifest is None:
        parser.error("--manifest is required")
    manifest = validate(json.loads(arguments.manifest.read_text()), arguments.root, arguments.registry)
    if arguments.action == "references":
        for component, entry in sorted(manifest["images"].items()):
            print("\t".join([component, entry["image"], entry["imageDigest"]]))


if __name__ == "__main__":
    main()

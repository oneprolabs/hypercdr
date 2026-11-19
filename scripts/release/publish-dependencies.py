#!/usr/bin/env python3
import argparse
import importlib.util
import json
import os
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("dependency_manifest", ROOT / "scripts/release/dependency-manifest.py")
MANIFEST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MANIFEST)


def execute(arguments, env=None):
    print("==> " + " ".join(map(str, arguments)), flush=True)
    subprocess.run(list(map(str, arguments)), env=env, check=True, timeout=1800)


def remote_digest(image):
    result = subprocess.run(
        ["bash", "-c", 'source "$1"; dependency_remote_digest "$2"', "_", str(ROOT / "scripts/release/dependency-registry.sh"), image],
        capture_output=True, text=True, timeout=90,
    )
    if result.returncode == 3:
        return None
    if result.returncode:
        raise RuntimeError(result.stderr)
    return result.stdout.strip()


def image_ref(registry, component, version):
    if ".aliyuncs.com/" in registry or registry.startswith("docker.io/"):
        return f"{registry}:{component}-{version}"
    return f"{registry}/{component}:{version}"


def publish(registry, output, runtime, force_suffix=""):
    fingerprints = MANIFEST.fingerprints(ROOT, registry)
    lock = json.loads((ROOT / "packaging/oadp/image-lock.json").read_text())
    runtime_lock = json.loads((ROOT / "packaging/dependencies/runtime-lock.json").read_text())
    runtime.mkdir(parents=True, exist_ok=True)
    images = {}
    resolved = dict(lock, images=[])
    resolved_path = runtime / "resolved-image-lock.json"
    env = dict(os.environ, HCDR_RUNTIME_ROOT=str(runtime), HCDR_IMAGE_REGISTRY=registry,
               HCDR_OADP_RESOLVED_LOCK=str(resolved_path), PYTHONDONTWRITEBYTECODE="1")

    def ensure(component, version, key, build, expected=None):
        tag = f"{version}-input-{key.removeprefix('sha256:')}{force_suffix}"
        target = image_ref(registry, component, tag)
        digest = remote_digest(target)
        if digest is None:
            build(target, tag)
            digest = remote_digest(target)
            if digest is None:
                raise RuntimeError(f"published dependency is missing: {component}")
        else:
            print(f"==> Reusing {component}: {target}@{digest}", flush=True)
        if expected is not None and digest != expected:
            raise RuntimeError(f"mirror digest mismatch: {component}")
        entry = {"image": target, "imageDigest": digest, "version": version}
        images[component] = entry
        return entry

    def mirror(source, target):
        execute(["timeout", "--kill-after=10", "900", "docker", "buildx", "imagetools", "create", "--prefer-index=false", "--tag", target, source])

    for entry in runtime_lock["images"]:
        source = entry["source"]
        digest = entry["sourceDigest"]
        if not MANIFEST.DIGEST.fullmatch(digest):
            raise ValueError("runtime dependency source must be digest pinned")
        key = MANIFEST.combined(fingerprints["runtime"], source, digest, entry["version"])
        ensure(entry["component"], entry["version"], key,
               lambda target, tag, source=source, digest=digest: mirror(f"{source}@{digest}", target), digest)

    def build_velero(target, tag):
        build_env = dict(env, HCDR_VELERO_BASE_LOCK=str(ROOT / "packaging/dependencies/runtime-lock.json"))
        execute(["bash", ROOT / "scripts/release/build-velero.sh", "--registry", registry, "--tag", tag, "--push"], build_env)

    ensure("velero", runtime_lock["velero"]["version"], fingerprints["velero"], build_velero)
    for entry in lock["images"]:
        source = entry["source"].split("@", 1)[0]
        digest = entry["sourceDigest"]
        key = MANIFEST.combined(source, digest, lock["platform"])
        target_entry = ensure(entry["component"], lock["release"], key,
                              lambda target, tag, source=source, digest=digest: mirror(f"{source}@{digest}", target), digest)
        resolved["images"].append(dict(entry, target=target_entry["image"], targetDigest=target_entry["imageDigest"]))
    resolved_path.write_text(json.dumps(resolved, indent=2) + "\n")
    for component in ["bundle", "catalog"]:
        def build(target, tag, component=component):
            build_env = dict(env, **{f"HCDR_OADP_{component.upper()}_IMAGE": target})
            execute(["bash", ROOT / f"scripts/release/build-community-oadp-{component}.sh", "--registry", registry], build_env)
        entry = ensure(f"oadp-{component}", lock["release"], fingerprints[component], build)
        resolved = json.loads(resolved_path.read_text())
        resolved[component] = entry
        resolved_path.write_text(json.dumps(resolved, indent=2) + "\n")
    manifest = {"schemaVersion": 1, "registry": registry, "platform": lock["platform"], "fingerprints": fingerprints, "images": images}
    MANIFEST.validate(manifest, ROOT, registry)
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".next")
    temporary.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    temporary.replace(output)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--registry", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--runtime", type=Path, required=True)
    parser.add_argument("--rebuild-id", default="")
    arguments = parser.parse_args()
    if arguments.rebuild_id and not __import__("re").fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9.-]{0,40}", arguments.rebuild_id):
        parser.error("invalid rebuild id")
    publish(arguments.registry.rstrip("/"), arguments.output, arguments.runtime,
            f"-rebuild-{arguments.rebuild_id}" if arguments.rebuild_id else "")


if __name__ == "__main__":
    main()

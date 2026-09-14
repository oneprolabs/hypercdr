#!/usr/bin/env python3
"""Small file-backed Release Center service."""
import json, os, pathlib, re, io, tarfile, hashlib, threading, hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

ROOT = pathlib.Path(os.environ.get("RELEASE_CENTER_DATA", "/var/lib/release-center"))
TOKEN = os.environ.get("RELEASE_CENTER_TOKEN", "")
VERSION = re.compile(r"(?:[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}|v[0-9]{8}\.[0-9]+)\Z")
LOCK = threading.RLock()

class Handler(BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.connection.settimeout(60)
    def _json(self, code, value):
        body = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def _auth(self):
        return bool(TOKEN) and hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + TOKEN)
    def do_GET(self):
        p = urlparse(self.path).path
        if p == "/healthz": return self._json(200, {"status":"ok"})
        if not self._auth(): return self._json(401, {"error":"unauthorized"})
        if p == "/api/v1/catalog":
            items=[]
            for f in sorted((ROOT/"releases").glob("*/release-manifest.json")):
                receipt = f.parent / "installer.json"
                if receipt.is_file() and (f.parent / "installer.tar.gz").is_file():
                    item = json.loads(f.read_text())
                    item["installer"] = json.loads(receipt.read_text())
                    items.append(item)
            return self._json(200, {"items":items})
        parts=p.split("/")
        if len(parts) >= 5 and not VERSION.fullmatch(parts[4]):
            return self._json(400, {"error":"invalid_version"})
        if len(parts)==6 and parts[:4]==["","api","v1","releases"] and parts[5]=="installer":
            f=ROOT/"releases"/parts[4]/"installer.tar.gz"
            if not f.is_file() or not (f.parent/"installer.json").is_file(): return self._json(404,{"error":"installer_not_found"})
            data=f.read_bytes(); self.send_response(200); self.send_header("Content-Type","application/gzip"); self.send_header("Content-Length",str(len(data))); self.end_headers(); self.wfile.write(data); return
        if len(parts)==5 and parts[:4]==["","api","v1","releases"]:
            f=ROOT/"releases"/parts[4]/"release-manifest.json"
            if not f.is_file(): return self._json(404,{"error":"release_not_found"})
            return self._json(200,json.loads(f.read_text()))
        return self._json(404,{"error":"not_found"})
    def do_POST(self):
        with LOCK:
            try:
                self._post()
            except (ValueError, tarfile.TarError, UnicodeError, EOFError):
                self._json(400, {"error":"invalid_request_or_archive"})
    def _post(self):
        if not self._auth(): return self._json(401,{"error":"unauthorized"})
        path = urlparse(self.path).path
        parts = path.split("/")
        if len(parts) == 6 and parts[:4] == ["", "api", "v1", "releases"] and parts[5] == "installer":
            version = parts[4].strip()
            if not VERSION.fullmatch(version): return self._json(400, {"error":"invalid_version"})
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 64 * 1024 * 1024: return self._json(413, {"error":"invalid_upload_size"})
            target = ROOT / "releases" / version
            if not (target/"release-manifest.json").is_file(): return self._json(404, {"error":"release_not_found"})
            data = self.rfile.read(length)
            if len(data) != length: return self._json(400, {"error":"incomplete_upload"})
            if not data or not data.startswith(b"\x1f\x8b"): return self._json(400, {"error":"installer_must_be_gzip"})
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as archive:
                members = archive.getmembers()
                if any(pathlib.PurePosixPath(m.name).is_absolute() or ".." in pathlib.PurePosixPath(m.name).parts or not (m.isfile() or m.isdir()) for m in members):
                    return self._json(400, {"error":"unsafe_archive"})
                manifests = [m for m in members if pathlib.PurePosixPath(m.name).name == "release-manifest.json"]
                if len(manifests) != 1 or manifests[0].size > 1024 * 1024:
                    return self._json(400, {"error":"installer_manifest_required"})
                if json.load(archive.extractfile(manifests[0])) != json.loads((target/"release-manifest.json").read_text()):
                    return self._json(400, {"error":"installer_manifest_mismatch"})
            destination = target/"installer.tar.gz"
            if destination.exists() and destination.read_bytes() != data:
                return self._json(409, {"error":"installer_is_immutable"})
            temporary = target/".installer-upload"
            temporary.write_bytes(data)
            temporary.replace(destination)
            receipt = target/".installer-receipt"
            receipt.write_text(json.dumps({"sha256":hashlib.sha256(data).hexdigest(), "size":len(data), "url":f"/api/v1/releases/{version}/installer"}))
            receipt.replace(target/"installer.json")
            return self._json(201, {"version": version, "status":"installer_uploaded"})
        if path != "/api/v1/releases": return self._json(404,{"error":"not_found"})
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > 1024 * 1024: return self._json(413, {"error":"invalid_manifest_size"})
        try: body=json.loads(self.rfile.read(length))
        except Exception: return self._json(400,{"error":"invalid_json"})
        if not isinstance(body, dict): return self._json(400,{"error":"manifest_must_be_object"})
        version=str(body.get("version", "")).strip()
        if not VERSION.fullmatch(version): return self._json(400,{"error":"invalid_version"})
        components = body.get("componentManifest")
        if not isinstance(components, dict) or not components:
            return self._json(400, {"error":"component_manifest_required"})
        for component in components.values():
            if not isinstance(component, dict) or not isinstance(component.get("image"), str) or not component["image"]:
                return self._json(400, {"error":"component_image_required"})
            digest = component.get("imageDigest")
            if not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
                return self._json(400, {"error":"invalid_component_digest"})
        if "installerPath" in body or "installer" in body: return self._json(400,{"error":"use_installer_upload_endpoint"})
        target=ROOT/"releases"/version; target.mkdir(parents=True,exist_ok=True)
        manifest = dict(body); manifest.pop("installerPath", None)
        f=target/"release-manifest.json"
        encoded=json.dumps(manifest,indent=2)+"\n"
        if f.exists() and json.loads(f.read_text())!=manifest: return self._json(409,{"error":"release_is_immutable"})
        temporary = target/".manifest-upload"
        temporary.write_text(encoded)
        temporary.replace(f)
        return self._json(201,{"version":version,"status":"awaiting_installer"})
    def log_message(self,*args): pass

if __name__ == "__main__":
    if not TOKEN: raise SystemExit("RELEASE_CENTER_TOKEN is required")
    ROOT.mkdir(parents=True,exist_ok=True); (ROOT/"releases").mkdir(exist_ok=True)
    ThreadingHTTPServer((os.environ.get("RELEASE_CENTER_BIND","0.0.0.0"), int(os.environ.get("RELEASE_CENTER_PORT","8090"))), Handler).serve_forever()

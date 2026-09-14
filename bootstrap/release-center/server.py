#!/usr/bin/env python3
"""Small file-backed Release Center service."""
import json, os, pathlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

ROOT = pathlib.Path(os.environ.get("RELEASE_CENTER_DATA", "/var/lib/release-center"))
TOKEN = os.environ.get("RELEASE_CENTER_TOKEN", "")

class Handler(BaseHTTPRequestHandler):
    def _json(self, code, value):
        body = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def _auth(self):
        return not TOKEN or self.headers.get("Authorization", "") == "Bearer " + TOKEN
    def do_GET(self):
        p = urlparse(self.path).path
        if p == "/healthz": return self._json(200, {"status":"ok"})
        if not self._auth(): return self._json(401, {"error":"unauthorized"})
        if p == "/api/v1/catalog":
            items=[]
            for f in sorted((ROOT/"releases").glob("*/release-manifest.json")):
                items.append(json.loads(f.read_text()))
            return self._json(200, {"items":items})
        parts=p.split("/")
        if len(parts)==6 and parts[:4]==["","api","v1","releases"] and parts[5]=="installer":
            f=ROOT/"releases"/parts[4]/"installer.tar.gz"
            if not f.is_file(): return self._json(404,{"error":"installer_not_found"})
            data=f.read_bytes(); self.send_response(200); self.send_header("Content-Type","application/gzip"); self.send_header("Content-Length",str(len(data))); self.end_headers(); self.wfile.write(data); return
        if len(parts)==5 and parts[:4]==["","api","v1","releases"]:
            f=ROOT/"releases"/parts[4]/"release-manifest.json"
            if not f.is_file(): return self._json(404,{"error":"release_not_found"})
            return self._json(200,json.loads(f.read_text()))
        return self._json(404,{"error":"not_found"})
    def do_POST(self):
        if not self._auth(): return self._json(401,{"error":"unauthorized"})
        if urlparse(self.path).path != "/api/v1/releases": return self._json(404,{"error":"not_found"})
        try: body=json.loads(self.rfile.read(int(self.headers.get("Content-Length","0"))))
        except Exception: return self._json(400,{"error":"invalid_json"})
        version=str(body.get("version", "")).strip()
        if not version: return self._json(400,{"error":"version_required"})
        target=ROOT/"releases"/version; target.mkdir(parents=True,exist_ok=True)
        f=target/"release-manifest.json"
        if f.exists() and f.read_bytes()!=json.dumps(body,indent=2).encode()+b"\n": return self._json(409,{"error":"release_is_immutable"})
        f.write_text(json.dumps(body,indent=2)+"\n")
        archive = body.get("installerPath") or body.get("installer", {}).get("path")
        if archive and pathlib.Path(archive).is_file():
            (target/"installer.tar.gz").write_bytes(pathlib.Path(archive).read_bytes())
        return self._json(201,{"version":version,"status":"published"})
    def log_message(self,*args): pass

if __name__ == "__main__":
    ROOT.mkdir(parents=True,exist_ok=True); (ROOT/"releases").mkdir(exist_ok=True)
    ThreadingHTTPServer((os.environ.get("RELEASE_CENTER_BIND","0.0.0.0"), int(os.environ.get("RELEASE_CENTER_PORT","8090"))), Handler).serve_forever()

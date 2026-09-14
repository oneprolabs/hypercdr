"""Exercise release publication over real HTTP using isolated temporary storage."""
import hashlib
import http.client
import io
import json
import pathlib
import tarfile
import tempfile
import threading
import unittest
import server


class PublicationTest(unittest.TestCase):
    def setUp(self):
        self.storage = tempfile.TemporaryDirectory()
        server.ROOT = pathlib.Path(self.storage.name)
        server.TOKEN = "test-token"
        self.http = server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        self.thread = threading.Thread(target=self.http.serve_forever, daemon=True)
        self.thread.start()
        self.manifest = {"version": "1.0.27.20260914", "componentManifest": {
            "platform-api": {"image": "registry.test/platform-api:1.0.27.20260914", "imageDigest": "sha256:" + "a" * 64}}}
        self.endpoint = "/api/v1/releases/1.0.27.20260914/installer"

    def tearDown(self):
        self.http.shutdown()
        self.http.server_close()
        self.thread.join()
        self.storage.cleanup()

    def request(self, method, path, body=None, token="test-token"):
        connection = http.client.HTTPConnection(*self.http.server_address, timeout=5)
        if isinstance(body, dict):
            body = json.dumps(body).encode()
        connection.request(method, path, body, {"Authorization": "Bearer " + token})
        response = connection.getresponse()
        result = response.status, response.read()
        connection.close()
        return result

    def archive(self, manifest, filename="package/release-manifest.json"):
        output = io.BytesIO()
        body = json.dumps(manifest).encode()
        with tarfile.open(fileobj=output, mode="w:gz") as archive:
            member = tarfile.TarInfo(filename)
            member.size = len(body)
            archive.addfile(member, io.BytesIO(body))
        return output.getvalue()

    def test_publish_download_checksum_and_retry(self):
        self.assertEqual(self.request("POST", "/api/v1/releases", self.manifest)[0], 201)
        self.assertEqual(json.loads(self.request("GET", "/api/v1/catalog")[1])["items"], [])
        archive = self.archive(self.manifest)
        self.assertEqual(self.request("POST", self.endpoint, archive)[0], 201)
        self.assertEqual(self.request("POST", self.endpoint, archive)[0], 201)
        self.assertEqual(self.request("GET", self.endpoint), (200, archive))
        items = json.loads(self.request("GET", "/api/v1/catalog")[1])["items"]
        self.assertEqual(items[0]["installer"]["sha256"], hashlib.sha256(archive).hexdigest())
        reordered = dict(reversed(list(self.manifest.items())))
        self.assertEqual(self.request("POST", "/api/v1/releases", reordered)[0], 201)
        changed = self.archive(self.manifest, "other/release-manifest.json")
        self.assertEqual(self.request("POST", self.endpoint, changed)[0], 409)

    def test_reject_incomplete_invalid_and_unsafe_publication(self):
        self.assertEqual(self.request("GET", "/api/v1/catalog", token="wrong")[0], 401)
        self.assertEqual(self.request("POST", self.endpoint, b"invalid")[0], 404)
        self.assertEqual(self.request("POST", "/api/v1/releases", {"version": "../../escape"})[0], 400)
        self.assertEqual(self.request("POST", "/api/v1/releases", dict(self.manifest, installerPath="/etc/passwd"))[0], 400)
        self.request("POST", "/api/v1/releases", self.manifest)
        self.assertEqual(self.request("POST", self.endpoint, b"\x1f\x8bgarbage")[0], 400)
        wrong = self.archive(dict(self.manifest, version="1.0.28.20260914"))
        self.assertEqual(self.request("POST", self.endpoint, wrong)[0], 400)
        unsafe = self.archive(self.manifest, "../release-manifest.json")
        self.assertEqual(self.request("POST", self.endpoint, unsafe)[0], 400)
        self.assertEqual(json.loads(self.request("GET", "/api/v1/catalog")[1])["items"], [])


if __name__ == "__main__":
    unittest.main()

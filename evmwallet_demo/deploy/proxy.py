#!/usr/bin/env python3
# Demo front door: serves the static page, /network.json, and forwards a
# whitelist of node API paths to the avalanchego node with Host rewritten
# to "localhost" (the node rejects other Host headers).
#
# usage: proxy.py <www dir> <network.json> <listen port>
import http.client
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

WWW, NETWORK_JSON, PORT = sys.argv[1], sys.argv[2], int(sys.argv[3])
ALLOWED = ("/ext/bc/C/rpc", "/ext/bc/C/avax", "/ext/bc/P", "/ext/info")
MAX_BODY = 256 * 1024
STATIC = {"index.html": "text/html; charset=utf-8", "ethers.umd.min.js": "application/javascript",
          "export_from_c.png": "image/png", "import_to_c.png": "image/png"}


def node_addr():
    with open(NETWORK_JSON) as f:
        u = urlsplit(json.load(f)["uri"])
    return u.hostname, u.port


class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, status, body, ctype="application/json", extra=()):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "content-type")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        for k, v in extra:
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self._send(204, b"")

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/network.json":
            try:
                with open(NETWORK_JSON, "rb") as f:
                    return self._send(200, f.read())
            except OSError:
                return self._send(503, b'{"error":"network not up yet"}')
        name = "index.html" if path == "/" else path.lstrip("/")
        if name in STATIC:
            try:
                with open(os.path.join(WWW, name), "rb") as f:
                    return self._send(200, f.read(), STATIC[name], [("Cache-Control", "no-cache")])
            except OSError:
                return self._send(404, b"not found", "text/plain")
        if path in ALLOWED:
            return self._proxy()
        self._send(404, b"not found", "text/plain")

    def do_POST(self):
        path = self.path.split("?", 1)[0]
        if path not in ALLOWED:
            return self._send(404, b'{"error":"not exposed"}')
        self._proxy()

    def _proxy(self, target=None):
        length = int(self.headers.get("Content-Length", 0))
        if length > MAX_BODY:
            return self._send(413, b'{"error":"body too large"}')
        body = self.rfile.read(length) if length else None
        try:
            conn = http.client.HTTPConnection(*(target or node_addr()), timeout=60)
            conn.request(self.command, self.path, body=body,
                         headers={"Host": "localhost", "Content-Type": "application/json"})
            resp = conn.getresponse()
            data = resp.read()
            conn.close()
        except Exception as e:  # node down, restarting, etc
            return self._send(502, json.dumps({"error": str(e)}).encode())
        self._send(resp.status, data, resp.getheader("Content-Type", "application/json"))

    def log_message(self, *a):
        pass


ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()

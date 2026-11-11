#!/usr/bin/env python3
"""真实 socket + TLS + built native UI 的 E2E。仅写隔离数据目录与本次工件。"""
import argparse
import base64
import fcntl
import concurrent.futures
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import stat
import ssl
import subprocess
import tempfile
import threading
import time


def wait_for(fn, seconds=8):
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError, AssertionError) as err:
            last = err
        time.sleep(0.025)
    raise AssertionError(f"condition timed out: {last}")


class Fixture(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    release = threading.Event()

    def log_message(self, *_):
        pass

    def body(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().split(b";")[0], 16)
                if not size:
                    while self.rfile.readline() != b"\r\n":
                        pass
                    return b"".join(chunks)
                chunks.append(self.rfile.read(size))
                assert self.rfile.read(2) == b"\r\n"
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def do_GET(self):
        self.do_POST()

    def do_POST(self):
        body = self.body()
        if self.path == "/events":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(b"data: first\n\n")
            self.wfile.flush()
            self.release.wait(12)
            self.wfile.write(b"data: last\n\n")
            self.wfile.flush()
            self.close_connection = True
            return
        if self.path == "/large-response":
            data = b"large-response-" * 100000
            content_type = "application/octet-stream"
        elif self.path.startswith("/burst"):
            data = b"burst-payload-" * 160
            content_type = "text/plain"
        else:
            value = {"id": "resp_prototype", "ok": True, "path": self.path,
                     "input": body.decode("utf-8", errors="replace") if len(body) < 4096 else None,
                     "length": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                     "trace": self.headers.get_all("X-Trace", [])}
            data = json.dumps(value, ensure_ascii=False).encode()
            content_type = "application/json"
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("X-Fixture", "aicat-e2e")
        self.end_headers()
        self.wfile.write(data)

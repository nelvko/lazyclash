"""Local protocol checks for the opt-in E2E CONNECT observer."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import socket
import socketserver
import subprocess
import sys
import tempfile
import threading
import time
import unittest


SCRIPT = Path(__file__).with_name("download-audit.py")


def header(connection):
    result = bytearray()
    while not result.endswith(b"\r\n\r\n"):
        data = connection.recv(1)
        if not data:
            break
        result.extend(data)
    return bytes(result)


class AuditTests(unittest.TestCase):
    @contextmanager
    def observer(self, upstream):
        with tempfile.TemporaryDirectory() as directory:
            ready, report = Path(directory) / "port", Path(directory) / "report.json"
            env = os.environ.copy()
            env["HTTPS_PROXY"] = upstream
            process = subprocess.Popen([sys.executable, str(SCRIPT), "--ready", str(ready), "--report", str(report)],
                                       env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline and not ready.exists() and process.poll() is None:
                    time.sleep(0.02)
                self.assertTrue(ready.exists(), "observer did not start")
                yield int(ready.read_text()), report
            finally:
                if process.poll() is None:
                    process.terminate()
                out, error = process.communicate(timeout=5)
                self.assertEqual(process.returncode, 0)
                self.assertEqual(out + error, b"")
                if ready.exists():
                    # Match the E2E cleanup check after closing actual tunnels;
                    # TIME_WAIT must not look like a leaked observer listener.
                    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
                        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                        listener.bind(("127.0.0.1", int(ready.read_text())))

    @contextmanager
    def upstream(self, status=200):
        requests = []

        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                self.request.settimeout(3)
                requests.append(header(self.request))
                self.request.sendall(("HTTP/1.1 %d Fixture\r\n\r\n" % status).encode())
                if status == 200:
                    while True:
                        data = self.request.recv(4096)
                        if not data:
                            break
                        self.request.sendall(data)

        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True

        with Server(("127.0.0.1", 0), Handler) as server:
            thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)
            thread.start()
            try:
                yield server.server_address[1], requests
            finally:
                server.shutdown()
                thread.join(timeout=3)

    def test_authenticated_upstream_preserves_tunnel_bytes_and_safe_report(self):
        with self.upstream() as (upstream_port, requests), self.observer(
                "http://fixture-user:fixture-pass@127.0.0.1:%d" % upstream_port) as (port, report):
            with socket.create_connection(("127.0.0.1", port), timeout=3) as connection:
                connection.sendall(b"CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n")
                self.assertIn(b"200", header(connection))
                payload = b"\x16\x03\x01opaque-tls-fixture\x00\xff"
                connection.sendall(payload)
                self.assertEqual(connection.recv(4096), payload)
            self.assertEqual(len(requests), 1)
            self.assertTrue(requests[0].startswith(b"CONNECT github.com:443 HTTP/1.1\r\n"))
            self.assertIn(b"Proxy-Authorization: Basic ", requests[0])
            result = json.loads(report.read_text())
            self.assertEqual(result, {"github": 1, "release_cdn": 0, "other": 0,
                                     "rejected_api": 0, "connected": 1, "upstream_failures": 0})
            self.assertNotIn("fixture", report.read_text())
            self.assertNotIn("127.0.0.1", report.read_text())

    def test_api_is_rejected_before_contacting_upstream(self):
        with self.upstream() as (upstream_port, requests), self.observer(
                "http://127.0.0.1:%d" % upstream_port) as (port, report):
            with socket.create_connection(("127.0.0.1", port), timeout=3) as connection:
                connection.sendall(b"CONNECT api.github.com:443 HTTP/1.1\r\n\r\n")
                self.assertIn(b"403", header(connection))
            self.assertEqual(requests, [])
            self.assertEqual(json.loads(report.read_text())["rejected_api"], 1)

    def test_upstream_failure_is_reported_without_fallback(self):
        with self.upstream(status=503) as (upstream_port, requests), self.observer(
                "http://127.0.0.1:%d" % upstream_port) as (port, report):
            with socket.create_connection(("127.0.0.1", port), timeout=3) as connection:
                connection.sendall(b"CONNECT github.com:443 HTTP/1.1\r\n\r\n")
                self.assertEqual(header(connection), b"")
            self.assertEqual(len(requests), 1)
            result = json.loads(report.read_text())
            self.assertEqual(result["upstream_failures"], 1)
            self.assertEqual(result["connected"], 0)

    def test_unsupported_upstream_fails_with_redacted_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            env = os.environ.copy()
            env["HTTPS_PROXY"] = "socks5://fixture-user:fixture-secret@127.0.0.1:1"
            process = subprocess.run([sys.executable, str(SCRIPT), "--ready", directory + "/port", "--report", directory + "/report"],
                                     env=env, capture_output=True, timeout=5)
            self.assertEqual(process.returncode, 2)
            self.assertIn(b"requires an existing HTTP(S) HTTPS_PROXY", process.stderr)
            self.assertNotIn(b"fixture-secret", process.stderr)
            self.assertFalse((Path(directory) / "port").exists())


if __name__ == "__main__":
    unittest.main()

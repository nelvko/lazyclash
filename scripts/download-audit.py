#!/usr/bin/env python3
"""Audit HTTPS downloads through the VM's explicitly selected environment proxy.

TLS remains end to end. Only destination categories and counts are persisted;
the upstream URL, credentials, request paths and payload are never recorded.
There is no direct connection or alternative proxy fallback.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import select
import signal
import socket
import socketserver
import ssl
import threading
from urllib.parse import unquote, urlsplit


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ready", required=True)
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    upstream = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy", "")
    proxy = urlsplit(upstream if "://" in upstream else "http://" + upstream)
    if proxy.scheme not in ("http", "https") or not proxy.hostname:
        parser.error("environment audit requires an existing HTTP(S) HTTPS_PROXY")
    try:
        proxy_port = proxy.port or (443 if proxy.scheme == "https" else 80)
    except ValueError:
        parser.error("environment audit requires a valid upstream port")
    authorization = ""
    if proxy.username is not None:
        credentials = unquote(proxy.username) + ":" + unquote(proxy.password or "")
        authorization = "Proxy-Authorization: Basic " + base64.b64encode(credentials.encode()).decode() + "\r\n"
    report = {"github": 0, "release_cdn": 0, "other": 0, "rejected_api": 0,
              "connected": 0, "upstream_failures": 0}
    report_path = Path(args.report)
    lock = threading.Lock()

    def record(field):
        with lock:
            report[field] += 1
            temporary = report_path.with_suffix(".tmp")
            temporary.write_text(json.dumps(report) + "\n")
            temporary.replace(report_path)

    class Handler(socketserver.BaseRequestHandler):
        def handle(self):
            remote = None
            contacting_upstream = established = False
            try:
                self.request.settimeout(30)
                header = bytearray()
                while not header.endswith(b"\r\n\r\n"):
                    chunk = self.request.recv(1)
                    if not chunk or len(header) >= 32768:
                        return
                    header.extend(chunk)
                first = header.split(b"\r\n", 1)[0].decode("ascii", "strict").split()
                if len(first) != 3 or first[0] != "CONNECT":
                    self.request.sendall(b"HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
                    return
                target = urlsplit("//" + first[1])
                host = (target.hostname or "").lower()
                if not host or target.port != 443 or target.username is not None:
                    self.request.sendall(b"HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
                    return
                if host == "api.github.com":
                    record("rejected_api")
                    self.request.sendall(b"HTTP/1.1 403 API Prohibited\r\nContent-Length: 0\r\n\r\n")
                    return
                category = "github" if host == "github.com" else (
                    "release_cdn" if host in {"release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com"} else "other")
                record(category)
                contacting_upstream = True
                remote = socket.create_connection((proxy.hostname, proxy_port), timeout=30)
                if proxy.scheme == "https":
                    remote = ssl.create_default_context().wrap_socket(remote, server_hostname=proxy.hostname)
                remote.sendall(("CONNECT " + first[1] + " HTTP/1.1\r\nHost: " + first[1] + "\r\n" + authorization + "\r\n").encode("ascii"))
                answer = bytearray()
                while not answer.endswith(b"\r\n\r\n"):
                    chunk = remote.recv(1)
                    if not chunk or len(answer) >= 32768:
                        raise OSError("incomplete upstream CONNECT")
                    answer.extend(chunk)
                status = answer.split(b"\r\n", 1)[0].split()
                if len(status) < 2 or status[1] != b"200":
                    raise OSError("upstream CONNECT refused")
                record("connected")
                established = True
                self.request.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                self.request.settimeout(None)
                remote.settimeout(None)
                while True:
                    readable, _, _ = select.select([self.request, remote], [], [], 60)
                    if not readable:
                        return
                    for source in readable:
                        chunk = source.recv(65536)
                        if not chunk:
                            return
                        destination = remote if source is self.request else self.request
                        destination.sendall(chunk)
            except (OSError, ValueError, UnicodeError):
                # The client may cancel a healthy download. Only failures before
                # the tunnel is established represent an upstream failure.
                if contacting_upstream and not established:
                    record("upstream_failures")
            finally:
                if remote is not None:
                    remote.close()

    class Server(socketserver.ThreadingTCPServer):
        daemon_threads = True
        allow_reuse_address = True

        def handle_error(self, request, client_address):
            # Tracebacks could include upstream details; fail via safe counts.
            record("upstream_failures")

    with Server(("127.0.0.1", 0), Handler) as server:
        report_path.write_text(json.dumps(report) + "\n")
        Path(args.ready).write_text(str(server.server_address[1]) + "\n")

        def stop(signum, frame):
            threading.Thread(target=server.shutdown, daemon=True).start()

        signal.signal(signal.SIGTERM, stop)
        signal.signal(signal.SIGINT, stop)
        server.serve_forever(poll_interval=0.1)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Exercise Profiles through a real PTY in a disposable, uninstalled home.

Uses only a loopback HTTP fixture and the existing stdlib terminal emulator.
Does not install a core or modify system services, proxy settings or user data.
"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import threading
import time
import uuid


def wait_until(terminal, predicate, label, timeout=15):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        terminal.read(0.1)
        if not terminal.screen.synchronized and predicate():
            return
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def paste(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")


def header_row(terminal, label):
    needle = label if label.startswith("[") else "[ " + label + " ]"
    terminal.wait(needle)
    rows = [i for i, line in enumerate(terminal.screen.text().splitlines()) if needle in line]
    assert len(rows) == 1 and rows[0] < 3, "page action was duplicated or left in the scrolling body: " + label + "\n" + terminal.screen.text()
    assert "Mihomo ·" not in "\n".join(terminal.screen.text().splitlines()[:3]), "page retained the generic core status"
    return rows[0]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    binary, output = args.binary.resolve(), args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("pty_smoke", Path(__file__).with_name("pty-smoke.py"))
    smoke = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(smoke)
    payload = [b"rules: ['MATCH,DIRECT']\n"]
    provider_name = ["pty-airport"]
    requests = []

    class Fixture(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            self.send_response(200)
            self.send_header("Content-Type", "text/yaml")
            self.send_header("profile-title", provider_name[0])
            self.end_headers()
            self.wfile.write(payload[0])

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    terminals = []
    try:
        with tempfile.TemporaryDirectory(prefix="lazyclash-profiles-pty-") as workspace:
            base = Path(workspace)
            home = base / "home"
            command = [str(binary), "--home", str(home), "--service-name", "lazyclash-profiles-pty-" + uuid.uuid4().hex[:12]]
            library_path = home / "configuration" / "library.json"

            def library():
                return json.loads(library_path.read_text()) if library_path.exists() else {}

            def profile_id(label):
                return next((profile_id for profile_id, record in library().get("profiles", {}).items() if record.get("name") == label), None)

            primary_id = [None]

            def primary_profile():
                saved = primary_id[0] or profile_id("pty-airport")
                return library().get("profiles", {}).get(saved, {})

            def revision():
                return primary_profile().get("spec", {}).get("revision")

            terminal = smoke.Terminal(command)
            terminals.append(terminal)
            terminal.wait("Overview")
            terminal.wait_absent("Loading state")
            terminal.resize(160, 40)
            terminal.key(b"3")
            terminal.wait("Configurations")
            header_row(terminal, "Update all")
            assert "Sources (" not in terminal.screen.text(), "source management remained in the Profiles header"
            terminal.key(b"a")
            secret = "pty-private-token-q1234567"
            address = "http://127.0.0.1:%d/%s?token=%s" % (server.server_port, secret, secret)
            paste(terminal, address)
            terminal.wait(secret)
            assert not library_path.exists(), "editing a URL unexpectedly saved a profile"
            terminal.key(b"\r")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "automatic subscription was not persisted")
            terminal.wait_absent("Fetching and validating")
            terminal.wait("pty-airport")
            primary_id[0] = profile_id("pty-airport")
            assert primary_id[0] and library()["profiles"][primary_id[0]]["name_source"] == "provider"
            terminal.wait_absent(secret)
            update_row = header_row(terminal, "Update all")
            assert primary_profile()["address"] == address
            assert not (home / "installation").exists(), "Add created runtime installation state"
            (output / "profiles-added.txt").write_text(terminal.screen.text())

            # Toolbar Down enters the first card; Right follows the wide grid.
            terminal.key(b"\x1b[B")
            terminal.key(b"\x1b[C")
            terminal.key(b"\r")
            terminal.wait("pty-airport · Configuration")
            terminal.wait("Install core to apply")
            assert header_row(terminal, "Update all") == update_row
            assert not (home / "installation").exists(), "card Enter implicitly applied a profile"

            payload[0] = b"rules: ['MATCH,REJECT']\n"
            provider_name[0] = "pty-updated"
            terminal.click_text("[ Update ]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "detail Update did not persist a profile revision")
            terminal.wait_absent("Updating profile")
            assert not (home / "installation").exists(), "Update applied a runtime configuration"
            terminal.wait("pty-updated · Configuration")
            assert profile_id("pty-updated") == primary_id[0], "provider rename changed its stable ID"

            terminal.click_text("[ Rename ]")
            terminal.wait("Display name")
            terminal.key(b"\x15")
            paste(terminal, "My Airport")
            terminal.key(b"\r")
            terminal.wait("My Airport · Configuration")
            assert profile_id("My Airport") == primary_id[0]
            assert library()["profiles"][primary_id[0]]["name_source"] == "custom"
            provider_name[0] = "provider-renamed-again"
            terminal.click_text("[ Edit URL ]")
            terminal.wait("New subscription URL or local YAML path")
            replacement_address = "http://127.0.0.1:%d/replacement" % server.server_port
            paste(terminal, replacement_address)
            terminal.key(b"\r")
            terminal.wait_absent("Updating subscription address")
            wait_until(terminal, lambda: primary_profile()["address"] == replacement_address, "Edit URL was not committed")
            assert profile_id("My Airport") == primary_id[0], "URL change overwrote a custom name or ID"

            terminal.resize(60, 20)
            terminal.wait("My Airport · Configuration")
            header_row(terminal, "Update all")
            terminal.key(b"\x1b")
            terminal.wait_absent("My Airport · Configuration")
            terminal.resize(160, 40)
            terminal.key(b"a")
            terminal.key(b"\x1b")
            terminal.wait("Update all")
            payload[0] = b"rules: ['DOMAIN,example.invalid,REJECT', 'MATCH,DIRECT']\n"
            terminal.click_text("[ Update all ]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "Update all did not persist a profile revision")
            terminal.wait_absent("Updating profiles")

            local_dir = base / "配置 files" / "nested folder"
            local_dir.mkdir(parents=True)
            (base / "配置 other").mkdir()
            local = local_dir / "pty-local"
            local.write_text("rules: ['MATCH,DIRECT']\n")
            (local_dir / "pty-other.yaml").write_text("rules: ['MATCH,DIRECT']\n")
            terminal.click_text("[ Import ]")
            terminal.wait("Import configuration")
            terminal.wait("Subscription URL or local YAML path")
            paste(terminal, str(base) + "/配")
            terminal.wait("› 配置 files/")
            terminal.wait("配置 other/")
            terminal.key(b"\x1b[B")
            terminal.wait("› 配置 other/")
            terminal.key(b"\x1b[A")
            terminal.wait("› 配置 files/")
            terminal.key(b"\t")
            terminal.wait("› nested folder/")
            assert len(library()["profiles"]) == 1, "directory completion submitted an import"
            terminal.key(b"\t")
            terminal.wait("› pty-local")
            terminal.wait("pty-other.yaml")
            terminal.key(b"\x1b[B")
            terminal.wait("› pty-other.yaml")
            terminal.key(b"\x1b[A")
            terminal.wait("› pty-local")
            terminal.key(b"\t")
            assert len(library()["profiles"]) == 1, "file completion submitted an import"
            assert profile_id("pty-local") is None, "Tab imported the selected file before Enter"
            terminal.key(b"\r")
            wait_until(terminal, lambda: profile_id("pty-local") is not None, "local import was not persisted")
            terminal.wait_absent("Fetching and validating")
            terminal.wait_absent("nested folder")
            assert "address" not in library()["profiles"][profile_id("pty-local")], "one-time import remembered its private path"
            assert not (home / "installation").exists(), "one-time import applied runtime state"

            terminal.key(b"a")
            terminal.key(b"\x1b")
            # A short viewport scrolls the cards; Update all stays fixed above
            # it and mouse updates still commit drafts without Apply.
            terminal.resize(40, 16)
            update_row = header_row(terminal, "Update all")
            terminal.key(b"\x1b[F")
            assert header_row(terminal, "Update all") == update_row
            payload[0] = b"rules: ['DOMAIN,header.invalid,DIRECT', 'MATCH,REJECT']\n"
            terminal.click_text("[ Update all ]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "fixed header Update all did not refresh while scrolled")
            terminal.wait_absent("Updating profiles")
            assert not (home / "installation").exists(), "fixed header update applied runtime state"
            assert header_row(terminal, "Update all") == update_row
            terminal.resize(20, 12)
            header_row(terminal, "Update all")
            terminal.resize(160, 40)
            terminal.wait("2 saved")
            (output / "profiles-header.txt").write_text(terminal.screen.text())

            for key, action in [(b"2", "[ Refresh ]"), (b"4", "[ Refresh ]"), (b"5", "[ Refresh ]"), (b"6", "[ Reconnect ]"), (b"7", "[ Network settings ]")]:
                terminal.key(key)
                header_row(terminal, action)
            terminal.key(b"1")
            terminal.wait("Mihomo · Not installed")
            terminal.key(b"3")
            provider_name[0] = "My Airport"
            terminal.key(b"a")
            paste(terminal, replacement_address)
            terminal.key(b"\r")
            wait_until(terminal, lambda: profile_id("My Airport (1)") is not None, "duplicate automatic name was not disambiguated")
            terminal.wait_absent("Fetching and validating")
            duplicate_id = profile_id("My Airport (1)")
            assert duplicate_id != primary_id[0], "duplicate import reused the existing profile ID"
            for _ in range(2):
                before = len(requests)
                terminal.click_text("[ Update all ]")
                wait_until(terminal, lambda: len(requests) >= before + 2, "Update all did not refresh both remote profiles")
                terminal.wait_absent("Updating profiles")
                assert profile_id("My Airport (1)") == duplicate_id, "repeated updates accumulated a duplicate suffix or changed the ID"
                assert profile_id("My Airport") == primary_id[0], "duplicate refresh changed the custom profile name"
            terminal.exit()

            terminal = smoke.Terminal(command)
            terminals.append(terminal)
            terminal.wait("Overview")
            terminal.wait_absent("Loading state")
            terminal.resize(160, 40)
            terminal.key(b"3")
            terminal.wait("My Airport")
            terminal.wait("My Airport (1)")
            terminal.wait("pty-local")
            terminal.wait("3 saved")
            assert secret not in terminal.text()
            assert not (home / "installation").exists()
            (output / "profiles-reopened.txt").write_text(terminal.screen.text())
            terminal.exit()
            result = {
                "passed": True,
                "scenarios": ["visible URL bracketed paste", "automatic Add without naming modal or Apply", "two-column arrows", "Enter opens details only", "provider name update retains ID", "custom Rename survives provider updates", "Edit URL retains custom name and ID", "detail configuration Update", "Update all", "visible one-time local Import", "live directory and file matches", "arrow selection and Tab completion without importing", "Unicode and spaced directories with an extensionless configuration", "60x20 resize and Escape", "fixed header while scrolled", "header mouse update without Apply", "20x12 compact header", "page-specific controls", "duplicate imports retain distinct IDs", "repeated updates preserve duplicate suffix", "exit and reopen persistence"],
                "http_requests": len(requests),
                "limitations": "No installed core; actual Apply/systemd integration is covered separately, not by this UI smoke test.",
            }
            (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps(result, ensure_ascii=False))
    finally:
        for terminal in terminals:
            terminal.cleanup()
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


if __name__ == "__main__":
    main()

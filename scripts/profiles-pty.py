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
import subprocess
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


def mouse_text(terminal, label, button=0, occurrence=0):
    terminal.wait(label)
    terminal.settle()
    matches = []
    for y, line in enumerate(terminal.screen.text().splitlines()):
        start = 0
        while label in line[start:]:
            x = line.index(label, start)
            matches.append((x, y))
            start = x + len(label)
    assert len(matches) > occurrence, "missing mouse target: " + label
    x, y = matches[occurrence]
    terminal.key(("\x1b[<%d;%d;%dM\x1b[<%d;%d;%dm" % (button, x + 1, y + 1, button, x + 1, y + 1)).encode())


def header_row(terminal, label):
    needle = "[" + label.strip("[] ") + "]"
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
    remote_started = threading.Event()
    remote_release = threading.Event()

    class Fixture(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            if self.path == "/new-profile":
                remote_started.set()
                remote_release.wait(timeout=10)
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
            terminal.wait("↑↓←→ Select")  # Initial version checks may still own the global busy state.
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

            # Menu entrypoints use the same private Profile without applying it.
            before = len(requests)
            mouse_text(terminal, "pty-airport", button=2)
            terminal.wait("View details")
            terminal.wait("Install core first")
            (output / "profiles-menu-160x40.txt").write_text(terminal.screen.text())
            terminal.key(b"\x1b")
            terminal.wait_absent("View details")
            terminal.key(b"\x1b")  # dismiss the Add notification before using its covered card controls
            mouse_text(terminal, "[↻]", button=2)
            terminal.wait("View details")
            terminal.key(b"\x1b")
            terminal.wait_absent("View details")
            mouse_text(terminal, "[⋯]", occurrence=1)
            terminal.wait("View details")
            terminal.key(b"q")
            terminal.key(b"2")
            paste(terminal, "menu-owned-paste")
            terminal.wait("View details")
            assert "menu-owned-paste" not in terminal.screen.text(), "menu paste escaped to a page input"
            # A click on another card dismisses without opening or applying it.
            mouse_text(terminal, "default")
            terminal.wait_absent("View details")
            assert "default · Configuration" not in terminal.screen.text(), "outside menu click reached its underlying card"
            terminal.key(b"m")
            terminal.wait("View details")
            terminal.key(b"\x1b")
            terminal.wait_absent("View details")
            assert len(requests) == before, "opening or dismissing a menu downloaded a subscription"
            assert not (home / "installation").exists(), "opening a menu applied runtime state"
            terminal.key(b"a")
            terminal.key(b"\x1b")

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
            terminal.click_text("[Update]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "detail Update did not persist a profile revision")
            terminal.wait_absent("Updating profile")
            assert not (home / "installation").exists(), "Update applied a runtime configuration"
            terminal.wait("pty-updated · Configuration")
            assert profile_id("pty-updated") == primary_id[0], "provider rename changed its stable ID"

            mouse_text(terminal, "pty-updated", button=2)
            terminal.wait("Rename…")
            mouse_text(terminal, "Rename…")
            terminal.wait("Display name")
            terminal.key(b"\x15")
            paste(terminal, "My Airport")
            terminal.key(b"\r")
            terminal.wait("My Airport · Configuration")
            assert profile_id("My Airport") == primary_id[0]
            assert library()["profiles"][primary_id[0]]["name_source"] == "custom"
            provider_name[0] = "provider-renamed-again"
            terminal.click_text("[Edit URL]")
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
            terminal.key(b"m")
            terminal.wait("View details")
            for columns, rows in [(60, 20), (40, 16), (20, 12)]:
                terminal.resize(columns, rows)
                terminal.wait("View details")
                assert not (home / "installation").exists(), "resizing a menu applied a profile"
                (output / ("profiles-menu-%dx%d.txt" % (columns, rows))).write_text(terminal.screen.text())
            terminal.key(b"\x1b")
            terminal.wait_absent("View details")
            terminal.resize(160, 40)
            terminal.key(b"a")
            terminal.key(b"\x1b")
            terminal.wait("Update all")
            payload[0] = b"rules: ['DOMAIN,example.invalid,REJECT', 'MATCH,DIRECT']\n"
            terminal.click_text("[Update all]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "Update all did not persist a profile revision")
            terminal.wait_absent("Updating profiles")

            local_dir = base / "配置 files" / "nested folder"
            local_dir.mkdir(parents=True)
            (base / "配置 other").mkdir()
            local = local_dir / "pty-local"
            local.write_text("rules: ['MATCH,DIRECT']\n")
            (local_dir / "pty-other.yaml").write_text("rules: ['MATCH,DIRECT']\n")
            terminal.click_text("[New profile]")
            terminal.wait("Display name (optional)")
            terminal.wait("Description (optional)")
            terminal.wait("URL or absolute local path")
            terminal.key(b"\t")  # optional display name -> optional description
            terminal.key(b"\t")  # optional description -> source
            paste(terminal, str(base) + "/配")
            terminal.wait("Source type: Local file")
            terminal.wait("› 配置 files/")
            terminal.wait("配置 other/")
            terminal.key(b"\x1b[B")
            terminal.wait("› 配置 other/")
            terminal.key(b"\x1b[A")
            terminal.wait("› 配置 files/")
            terminal.key(b"\t")
            terminal.wait("› nested folder/")
            assert len(library()["profiles"]) == 1, "directory completion created a profile"
            terminal.key(b"\t")
            terminal.wait("› pty-local")
            terminal.wait("pty-other.yaml")
            terminal.key(b"\x1b[B")
            terminal.wait("› pty-other.yaml")
            terminal.key(b"\x1b[A")
            terminal.wait("› pty-local")
            terminal.key(b"\t")
            assert len(library()["profiles"]) == 1, "file completion created a profile"
            assert profile_id("pty-local") is None, "Tab created the selected file profile before Enter"
            terminal.wait("› pty-local")
            terminal.key(b"\t")
            terminal.wait("›[Create]")
            terminal.key(b"\r")
            wait_until(terminal, lambda: profile_id("pty-local") is not None, "new local profile was not persisted")
            terminal.wait_absent("Creating profile")
            terminal.wait_absent("nested folder")
            local_id = profile_id("pty-local")
            local_record = library()["profiles"][local_id]
            assert local_record["address"] == str(local), "new local profile did not retain its source for updates"
            assert local_record["name_source"] == "url", "empty optional name was treated as a custom name"
            assert not local_record.get("description"), "empty optional description was not left empty"
            assert not (home / "installation").exists(), "new local profile applied runtime state"

            before = len(requests)
            local.write_text("rules: ['DOMAIN,local.invalid,REJECT', 'MATCH,DIRECT']\n")
            mouse_text(terminal, "pty-local", button=2)
            terminal.wait("View details")
            assert "No saved address" not in terminal.screen.text(), "new local profile Update was disabled"
            terminal.key(b"\r")  # Update is the first enabled action without an installed core.
            wait_until(terminal, lambda: library()["profiles"][local_id]["spec"]["revision"] == hashlib.sha256(local.read_bytes()).hexdigest(), "local menu Update did not reread the retained path")
            terminal.wait_absent("Updating profile")
            terminal.wait_absent("View details")
            assert len(requests) == before, "local Update sent an HTTP request"
            assert profile_id("pty-local") == local_id, "local refresh changed its stable profile ID"
            assert not (home / "installation").exists(), "local menu Update applied runtime state"

            # The second toolbar action also creates a named remote profile.
            # Hold its response to exercise the busy form without racing the
            # immediate loopback fixture; input must not submit it twice.
            remote_name = "PTY named remote"
            remote_description = "Remote metadata from New profile"
            remote_address = "http://127.0.0.1:%d/new-profile" % server.server_port
            terminal.click_text("[New profile]")
            terminal.wait("Display name (optional)")
            paste(terminal, remote_name)
            terminal.key(b"\t")
            paste(terminal, remote_description)
            terminal.key(b"\t")
            paste(terminal, remote_address)
            terminal.wait("Source type: Remote URL")
            terminal.key(b"\t")  # no path matches: source -> Create
            terminal.wait("›[Create]")
            terminal.key(b"\t")
            terminal.wait("›[Cancel]")
            terminal.key(b"\x1b[Z")  # Cancel -> Create
            terminal.wait("›[Create]")
            before = len(requests)
            terminal.key(b"\r")
            wait_until(terminal, remote_started.is_set, "New profile did not start its remote download")
            terminal.wait("Creating profile")
            terminal.key(b"\r\t\r")
            paste(terminal, "busy-input-must-not-edit")
            assert len(library()["profiles"]) == 2, "busy creation published a partial profile"
            remote_release.set()
            wait_until(terminal, lambda: profile_id(remote_name) is not None, "named remote profile was not created")
            terminal.wait_absent("Creating profile")
            terminal.wait_absent("URL or absolute local path")
            remote_id = profile_id(remote_name)
            remote_record = library()["profiles"][remote_id]
            assert len(requests) == before + 1, "New profile downloaded the remote source more than once"
            assert remote_record["name_source"] == "custom" and remote_record["name_base"] == remote_name
            assert remote_record["description"] == remote_description, "New profile lost the description"
            assert remote_record["address"] == remote_address, "New profile lost its remote refresh source"
            assert remote_record["spec"]["revision"] == hashlib.sha256(payload[0]).hexdigest()
            assert "busy-input-must-not-edit" not in terminal.screen.text(), "busy form input leaked into the page"
            assert not (home / "installation").exists(), "new remote profile applied runtime state"
            (output / "profiles-created.txt").write_text(terminal.screen.text())

            terminal.key(b"a")
            terminal.key(b"\x1b")
            # A short viewport scrolls the cards; Update all stays fixed above
            # it and mouse updates still commit drafts without Apply.
            terminal.resize(40, 16)
            update_row = header_row(terminal, "Update all")
            terminal.key(b"\x1b[F")
            assert header_row(terminal, "Update all") == update_row
            payload[0] = b"rules: ['DOMAIN,header.invalid,DIRECT', 'MATCH,REJECT']\n"
            terminal.click_text("[Update all]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "fixed header Update all did not refresh while scrolled")
            terminal.wait_absent("Updating profiles")
            assert not (home / "installation").exists(), "fixed header update applied runtime state"
            assert header_row(terminal, "Update all") == update_row
            terminal.resize(20, 12)
            header_row(terminal, "Update all")
            terminal.resize(160, 40)
            terminal.wait("3 custom · 1 built-in")
            (output / "profiles-header.txt").write_text(terminal.screen.text())

            for key, action in [(b"2", "[Refresh]"), (b"4", "[Refresh]"), (b"5", "[Rule providers]"), (b"6", "[Reconnect]"), (b"7", "[Download method]")]:
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
                terminal.click_text("[Update all]")
                wait_until(terminal, lambda: len(requests) >= before + 3, "Update all did not refresh all three remote profiles")
                terminal.wait_absent("Updating profiles")
                assert profile_id("My Airport (1)") == duplicate_id, "repeated updates accumulated a duplicate suffix or changed the ID"
                assert profile_id("My Airport") == primary_id[0], "duplicate refresh changed the custom profile name"
                assert profile_id(remote_name) == remote_id, "provider refresh changed the New profile custom name or ID"
                assert library()["profiles"][remote_id]["description"] == remote_description, "provider refresh discarded the description"
            terminal.exit()

            # CLI copy import remains supported independently of the new form.
            # Retain disabled-Update coverage for profiles without a source.
            subprocess.run(command + ["profile", "add", str(local), "--name", "pty-snapshot"], check=True, capture_output=True, timeout=15)
            snapshot_id = profile_id("pty-snapshot")
            assert snapshot_id and "address" not in library()["profiles"][snapshot_id], "copy import unexpectedly retained its source"

            terminal = smoke.Terminal(command)
            terminals.append(terminal)
            terminal.wait("Overview")
            terminal.wait_absent("Loading state")
            terminal.resize(160, 40)
            terminal.key(b"3")
            terminal.wait("My Airport")
            terminal.wait("My Airport (1)")
            terminal.wait("pty-local")
            terminal.wait(remote_name)
            terminal.wait("pty-snapshot")
            terminal.wait("5 custom · 1 built-in")
            terminal.wait("↑↓←→ Select")
            assert profile_id(remote_name) == remote_id and profile_id("pty-local") == local_id
            reopened_remote = library()["profiles"][remote_id]
            assert reopened_remote["name_source"] == "custom" and reopened_remote["name_base"] == remote_name
            assert reopened_remote["description"] == remote_description and reopened_remote["address"] == remote_address
            assert library()["profiles"][local_id]["address"] == str(local)
            assert secret not in terminal.text()
            assert not (home / "installation").exists()
            (output / "profiles-reopened.txt").write_text(terminal.screen.text())
            before = len(requests)
            mouse_text(terminal, "pty-snapshot", button=2)
            terminal.wait("No saved address")
            mouse_text(terminal, "Update · No saved address")
            terminal.wait("View details")
            assert len(requests) == before, "disabled copy-import Update sent a download request"
            assert not (home / "installation").exists(), "disabled menu Update applied runtime state"
            terminal.key(b"\x1b")
            terminal.wait_absent("View details")
            mouse_text(terminal, "pty-snapshot", button=2)
            mouse_text(terminal, "Delete…")
            terminal.wait("Delete recipe")
            terminal.wait("›[Cancel]")
            terminal.key(b"\r")
            terminal.wait_absent("Delete recipe")
            assert profile_id("pty-snapshot") == snapshot_id, "default Enter deleted a profile instead of canceling"

            mouse_text(terminal, "pty-snapshot", button=2)
            mouse_text(terminal, "Delete…")
            terminal.wait("›[Cancel]")
            terminal.key(b"\x1b[Z")  # Cancel -> name input
            paste(terminal, "wrong-name")
            terminal.key(b"\r")  # Input Enter only focuses confirmation.
            terminal.wait("›[Delete]")
            assert profile_id("pty-snapshot") == snapshot_id, "input Enter bypassed explicit deletion confirmation"
            terminal.key(b"\r")
            terminal.wait("Enter the exact name")
            assert profile_id("pty-snapshot") == snapshot_id, "delete confirmation accepted a mismatched name"
            terminal.key(b"\x01\x0b")
            paste(terminal, "pty-snapshot")
            terminal.click_text("[Delete]")
            wait_until(terminal, lambda: profile_id("pty-snapshot") is None, "confirmed deletion did not remove the selected profile")
            terminal.wait_absent("Deleting profile")
            assert len(requests) == before, "deletion requested a subscription download"
            (output / "profiles-delete-confirmed.txt").write_text(terminal.screen.text())
            terminal.exit()
            result = {
                "passed": True,
                "scenarios": ["visible URL bracketed paste", "automatic Add without naming modal or Apply", "two-column arrows", "Enter opens details only", "provider name update retains ID", "custom Rename survives provider updates", "Edit URL retains custom name and ID", "detail configuration Update", "Update all", "New profile local file with optional metadata omitted", "live directory and file matches", "arrow selection and Tab completion without creating", "Unicode and spaced directories with an extensionless configuration", "New profile remote URL with custom name and description", "Tab and Shift+Tab reach Create and Cancel", "busy form ignores editing and duplicate submission", "one download per new remote profile", "retained local source refresh", "CLI one-time copy import", "60x20 resize and Escape", "fixed header while scrolled", "header mouse update without Apply", "20x12 compact header", "page-specific controls", "duplicate imports retain distinct IDs", "repeated updates preserve duplicate suffix", "exit and reopen metadata and source persistence"],
                "menu_scenarios": ["right-click card without Apply", "right-click update icon opens containing card menu", "left-click menu button", "menu owns q, page digits and pasted text", "outside click closes without reaching the underlying card", "keyboard m and Escape focus restoration", "menu Rename uses the existing custom-name flow", "menu edge avoidance through resize", "retained local file Update rereads the source without HTTP", "copy-import Update is disabled without a download request"],
                "menu_dimensions": [[160, 40], [60, 20], [40, 16], [20, 12]],
                "delete_scenarios": ["default Enter cancels", "input Enter only focuses confirmation", "mismatched name is rejected", "explicit mouse confirmation deletes once"],
                "http_requests": len(requests),
                "limitations": "No installed core; actual Apply/systemd integration is covered separately, not by this UI smoke test.",
            }
            (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
            print(json.dumps(result, ensure_ascii=False))
    finally:
        remote_release.set()
        for terminal in terminals:
            terminal.cleanup()
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


if __name__ == "__main__":
    main()

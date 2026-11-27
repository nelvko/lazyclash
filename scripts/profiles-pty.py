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

from e2e_instance import UserInstance


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


def assert_saved_cards(terminal, count):
    terminal.settle()
    screen = terminal.screen.text()
    assert screen.count("⋯") == count, "unexpected number of profile cards\n" + screen
    for label in ("Built-in default configuration", "1 built-in", "Configurations ·"):
        assert label not in screen, "unexpected default card or summary\n" + screen


def assert_settings_allowance(terminal, quota, expiry):
    terminal.wait("Remaining / total")
    rows = terminal.screen.text().splitlines()
    y = next(i for i, row in enumerate(rows) if "Remaining / total" in row)
    for label, value in [("Remaining / total", quota), ("Expiry date", expiry)]:
        column = rows[y].index(label)
        assert rows[y + 1].startswith(value, column), f"settings value misaligned below {label}"


def assert_stacked_profile_fields(terminal):
    terminal.wait("Description (optional)")
    rows = terminal.screen.text().splitlines()
    first_frame = None
    previous = -1
    for label in ("Subscription URL or local path", "User-Agent (optional)", "Display name", "Description (optional)"):
        y = next(i for i, row in enumerate(rows) if label in row)
        x = rows[y].index(label)
        frame = (x, rows[y + 1].index("╮", x))
        assert y > previous and rows[y + 1][x] == "╭", f"field did not stack below its label: {label}"
        if first_frame is None:
            first_frame = frame
        assert frame == first_frame, f"field edges differ from the source field: {label}"
        previous = y


def header_row(terminal, label):
    needle = "[" + label.strip("[] ") + "]"

    def header_visible():
        header = "\n".join(terminal.screen.text().splitlines()[:3])
        if needle in header:
            return True
        # The newest notification covers the page action. Another may replace it.
        if "×" in header:
            terminal.click_text("×")
        return False

    wait_until(terminal, header_visible, "page header not visible: " + label)
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
    gib = 1024**3
    year = time.localtime().tm_year
    expires = int(time.mktime((year + 1, 6, 15, 12, 0, 0, 0, 0, -1)))
    future_date = time.strftime("%Y-%m-%d", time.localtime(expires))
    quota = [f"upload={gib}; download={2*gib}; total={10*gib}; expire={expires}"]
    requests = []
    remote_agents = []
    remote_started = threading.Event()
    remote_release = threading.Event()

    class Fixture(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            if self.path == "/new-profile":
                remote_agents.append(self.headers.get("User-Agent"))
                remote_started.set()
                remote_release.wait(timeout=10)
            if self.path == "/invalid-configuration":
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"rules: [invalid\n")
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/yaml")
            self.send_header("profile-title", provider_name[0])
            if quota[0] is not None:
                self.send_header("Subscription-Userinfo", quota[0])
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
            home.mkdir(mode=0o700)
            (home / "preferences.json").write_text(json.dumps({
                "HasRun": True, "GuideDismissed": True, "StartPage": "profiles",
            }))
            instance = UserInstance(binary, home, "lazyclash-profiles-pty-" + uuid.uuid4().hex[:12])
            command = instance.product("--lang", "en")
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
            terminal.wait("No saved profiles")
            terminal.resize(160, 40)
            assert_saved_cards(terminal, 0)
            header_row(terminal, "Update all")
            (output / "profiles-empty.txt").write_text(terminal.screen.text())
            assert "Sources (" not in terminal.screen.text(), "source management remained in the Profiles header"
            terminal.click_text("Subscription URL or local file path")
            terminal.key(b"\r")
            assert not library_path.exists(), "empty import created a profile"
            secret = "pty-private-token-q1234567"
            address = "http://127.0.0.1:%d/%s?token=%s" % (server.server_port, secret, secret)
            paste(terminal, address)
            terminal.wait(secret)
            terminal.wait("Enter Import")
            assert not library_path.exists(), "editing a URL unexpectedly saved a profile"
            terminal.key(b"\r")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "automatic subscription was not persisted")
            terminal.wait_absent("Fetching and validating")
            terminal.wait("pty-airport")
            primary_id[0] = profile_id("pty-airport")
            assert primary_id[0] and library()["profiles"][primary_id[0]]["name_source"] == "provider"
            terminal.wait_absent(secret)
            update_row = header_row(terminal, "Update all")
            assert_saved_cards(terminal, 1)
            assert primary_profile()["address"] == address
            assert not (home / "installation").exists(), "Add created runtime installation state"
            for columns, rows in [(60, 20), (160, 40)]:
                terminal.resize(columns, rows)
                terminal.wait("7.0 GiB / 10.0 GiB")
                terminal.wait("━")
                terminal.settle()
                screen = terminal.screen.text()
                origin_row = next(line for line in screen.splitlines() if "127.0.0.1" in line)
                assert origin_row.rstrip().endswith("just now"), "missing source fallback or misplaced fetch age"
                allowance = next(line for line in screen.splitlines() if "7.0 GiB / 10.0 GiB" in line)
                assert allowance.split("7.0 GiB / 10.0 GiB", 1)[1].strip() == future_date, "card should show bare balance and expiry"
                (output / f"profiles-quota-{columns}x{rows}.txt").write_text(screen)
            (output / "profiles-added.txt").write_text(screen)

            # Menu entrypoints use the same private Profile without applying it.
            before = len(requests)
            mouse_text(terminal, "pty-airport", button=2)
            terminal.wait("Settings…")
            terminal.wait("Install core first")
            menu_rows = [line.split("│")[1].strip(" ›") for line in terminal.screen.text().splitlines() if line.count("│") == 2]
            first = menu_rows.index("Use · Install core first")
            assert menu_rows[first:first + 6] == [
                "Use · Install core first", "Edit configuration…", "Edit overrides…",
                "Update", "Settings…", "Delete…",
            ], "remote profile actions were missing, separated, or out of order"
            (output / "profiles-menu-160x40.txt").write_text(terminal.screen.text())
            terminal.key(b"\x1b")
            terminal.wait_absent("Settings…")
            mouse_text(terminal, "↻", button=2)
            terminal.wait("Settings…")
            terminal.key(b"\x1b")
            terminal.wait_absent("Settings…")
            mouse_text(terminal, "⋯")
            terminal.wait("Settings…")
            terminal.key(b"q")
            terminal.key(b"2")
            paste(terminal, "menu-owned-paste")
            terminal.wait("Settings…")
            assert "menu-owned-paste" not in terminal.screen.text(), "menu paste escaped to a page input"
            # Clicking outside dismisses the menu without opening the creation form.
            mouse_text(terminal, "[New]")
            terminal.wait_absent("Settings…")
            assert "New profile" not in terminal.screen.text(), "outside menu click reached its underlying control"
            terminal.key(b"m")
            terminal.wait("Settings…")
            terminal.key(b"\x1b")
            terminal.wait_absent("Settings…")
            assert len(requests) == before, "opening or dismissing a menu downloaded a subscription"
            assert not (home / "installation").exists(), "opening a menu applied runtime state"
            terminal.key(b"a")
            terminal.key(b"\x1b")

            # Enter on the card opens the same Settings panel as its menu.
            terminal.key(b"\x1b[B")
            terminal.key(b"\r")
            terminal.wait("Profile settings")
            assert_settings_allowance(terminal, "7.0 GiB / 10.0 GiB", future_date)
            terminal.wait(secret)  # address is populated only after the private settings read
            assert_stacked_profile_fields(terminal)
            assert "Deactivate" not in terminal.screen.text()
            assert not (home / "installation").exists(), "opening Settings applied a profile"
            memo = "Description only"
            terminal.key(b"\t")  # display name -> description
            paste(terminal, memo)
            terminal.key(b"\t")
            terminal.wait("› Import overrides…")
            terminal.key(b"\t")
            terminal.wait("›[Cancel]")
            terminal.key(b"\t")
            terminal.wait("›[Save]")
            terminal.click_text("[Save]")
            wait_until(terminal, lambda: primary_profile().get("description") == memo, "settings did not save description")
            terminal.wait_absent("Profile settings")
            assert primary_profile()["name_source"] == "provider" and len(requests) == before
            terminal.wait(memo)
            memo_row = next(line for line in terminal.screen.text().splitlines() if memo in line)
            assert memo_row.rstrip().endswith("just now") and "127.0.0.1" not in memo_row, "description did not replace source, or age moved"

            payload[0] = b"rules: ['MATCH,REJECT']\n"
            provider_name[0] = "pty-updated"
            cutoff = int(time.time()) + 10
            cutoff_date = time.strftime("%Y-%m-%d", time.localtime(cutoff))
            quota[0] = f"upload={gib}; download={5*gib}; total={10*gib}; expire={cutoff}"
            mouse_text(terminal, "pty-airport", button=2)
            mouse_text(terminal, "Update", occurrence=1)  # skip the fixed Update all header
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "menu Update did not persist a profile revision")
            terminal.wait_absent("Updating profile")
            assert not (home / "installation").exists(), "Update applied a runtime configuration"
            terminal.wait("pty-updated")
            terminal.wait("4.0 GiB / 10.0 GiB")
            terminal.wait_absent("7.0 GiB / 10.0 GiB")
            terminal.wait(cutoff_date)
            terminal.wait("Expired")  # no input or refresh across the provider's cutoff
            assert time.time() >= cutoff
            card = terminal.screen.text()
            allowance = next(line for line in card.splitlines() if "4.0 GiB / 10.0 GiB" in line)
            assert allowance.split("4.0 GiB / 10.0 GiB", 1)[1].strip() == "Expired" and cutoff_date not in card
            assert profile_id("pty-updated") == primary_id[0], "provider rename changed its stable ID"
            assert primary_profile()["description"] == memo

            mouse_text(terminal, "pty-updated", button=2)
            mouse_text(terminal, "Settings…")
            terminal.wait("Profile settings")
            assert_settings_allowance(terminal, "4.0 GiB / 10.0 GiB", cutoff_date)
            terminal.wait(memo)
            # A malformed replacement must preserve both the record and the draft.
            prior = primary_profile()
            new_memo = "Changed together"
            invalid_address = f"http://127.0.0.1:{server.server_port}/invalid-configuration"
            terminal.key(b"\x15")
            paste(terminal, "My Airport")
            terminal.key(b"\t\x15")
            paste(terminal, new_memo)
            terminal.key(b"\x1b[Z" * 3 + b"\x15")  # description -> name -> User-Agent -> source
            paste(terminal, invalid_address)
            terminal.key(b"\r")
            wait_until(terminal, lambda: "/invalid-configuration" in requests, "invalid source was not checked")
            terminal.wait("invalid Mihomo YAML")
            terminal.wait("Profile settings")
            terminal.wait(new_memo)
            terminal.wait("My Airport")
            terminal.wait("/invalid-configuration")
            assert primary_profile() == prior, "failed validation changed existing profile metadata"
            replacement_address = f"http://127.0.0.1:{server.server_port}/replacement"
            provider_name[0] = "provider-renamed-again"
            terminal.key(b"\x15")
            paste(terminal, replacement_address)
            terminal.key(b"\r")
            wait_until(terminal, lambda: primary_profile()["address"] == replacement_address, "fixed source was not committed")
            terminal.wait_absent("Profile settings")
            assert profile_id("My Airport") == primary_id[0] and primary_profile()["name_source"] == "custom"
            assert primary_profile()["description"] == new_memo
            terminal.wait_absent(secret)

            terminal.resize(60, 20)
            mouse_text(terminal, "My Airport", button=2)
            mouse_text(terminal, "Settings…")
            terminal.wait("Profile settings")
            terminal.settle()
            (output / "profiles-settings-60x20.txt").write_text(terminal.screen.text())
            terminal.key(b"\x1b")
            terminal.wait_absent("Profile settings")
            terminal.key(b"m")
            terminal.wait("Settings…")
            for columns, rows in [(60, 20), (40, 16), (20, 12)]:
                terminal.resize(columns, rows)
                terminal.wait("Settings…")
                assert not (home / "installation").exists(), "resizing a menu applied a profile"
                (output / ("profiles-menu-%dx%d.txt" % (columns, rows))).write_text(terminal.screen.text())
            terminal.key(b"\x1b")
            terminal.wait_absent("Settings…")
            terminal.resize(160, 40)
            terminal.key(b"a")
            terminal.key(b"\x1b")
            terminal.wait("Update all")
            payload[0] = b"rules: ['DOMAIN,example.invalid,REJECT', 'MATCH,DIRECT']\n"
            quota[0] = None
            terminal.click_text("[Update all]")
            wait_until(terminal, lambda: revision() == hashlib.sha256(payload[0]).hexdigest(), "Update all did not persist a profile revision")
            terminal.wait_absent("Updating profiles")
            terminal.wait_absent("4.0 GiB / 10.0 GiB")
            for columns, rows in [(60, 20), (160, 40)]:
                terminal.resize(columns, rows)
                terminal.wait("My Airport")
                terminal.settle()
                screen = terminal.screen.text()
                assert not any(item in screen for item in (future_date, cutoff_date, "Expired", "━")), "stale expiry or quota after the provider omitted its header"
                (output / f"profiles-no-quota-{columns}x{rows}.txt").write_text(screen)

            local_dir = base / "配置 files" / "nested folder"
            local_dir.mkdir(parents=True)
            (base / "配置 other").mkdir()
            local = local_dir / "pty-local"
            local.write_text("rules: ['MATCH,DIRECT']\n")
            (local_dir / "pty-other.yaml").write_text("rules: ['MATCH,DIRECT']\n")
            terminal.click_text("[New]")
            terminal.wait("New profile")
            paste(terminal, str(base) + "/配")
            terminal.wait_absent("User-Agent")
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
            terminal.key(b"\r")
            wait_until(terminal, lambda: profile_id("pty-local") is not None, "new local profile was not persisted")
            terminal.wait_absent("Creating profile")
            terminal.wait_absent("nested folder")
            header_row(terminal, "Update all")
            local_id = profile_id("pty-local")
            local_record = library()["profiles"][local_id]
            assert local_record["address"] == str(local), "new local profile did not retain its source for updates"
            assert local_record["name_source"] == "url", "empty optional name was treated as a custom name"
            assert not local_record.get("description"), "empty optional description was not left empty"
            assert not (home / "installation").exists(), "new local profile applied runtime state"

            # Use the two saved cards to check horizontal navigation. Their
            # order comes from the rendered grid, independent of generated IDs.
            terminal.wait("pty-local")
            card_row = next(line for line in terminal.screen.text().splitlines() if "pty-local" in line and "My Airport" in line)
            right_name = max(("My Airport", "pty-local"), key=card_row.index)
            terminal.key(b"a")
            terminal.key(b"\x1b")
            terminal.key(b"\x1b[B")
            terminal.key(b"\x1b[C")
            terminal.key(b"\r")
            terminal.wait("Profile settings")
            wait_until(terminal, lambda: any("│ " + right_name in line for line in terminal.screen.text().splitlines()),
                       "settings opened for a different card")
            terminal.key(b"\x1b")
            terminal.wait_absent("Profile settings")

            before = len(requests)
            local.write_text("rules: ['DOMAIN,local.invalid,REJECT', 'MATCH,DIRECT']\n")
            mouse_text(terminal, "pty-local", button=2)
            terminal.wait("Settings…")
            mouse_text(terminal, "Reload from file")
            wait_until(terminal, lambda: library()["profiles"][local_id]["spec"]["revision"] == hashlib.sha256(local.read_bytes()).hexdigest(), "local menu Update did not reread the retained path")
            terminal.wait_absent("Updating profile")
            terminal.wait_absent("Settings…")
            assert len(requests) == before, "local Update sent an HTTP request"
            assert profile_id("pty-local") == local_id, "local refresh changed its stable profile ID"
            assert not (home / "installation").exists(), "local menu Update applied runtime state"

            # The second toolbar action also creates a named remote profile.
            # Hold its response to exercise the busy form without racing the
            # immediate loopback fixture; input must not submit it twice.
            remote_name = "PTY named remote"
            remote_description = "Remote metadata from New profile"
            remote_agent = "PTY-Subscription/1.0"
            remote_address = "http://127.0.0.1:%d/new-profile" % server.server_port
            terminal.click_text("[New]")
            terminal.wait("New profile")
            paste(terminal, remote_address)
            terminal.wait("User-Agent")
            assert_stacked_profile_fields(terminal)
            terminal.key(b"\t")
            paste(terminal, remote_agent)
            terminal.key(b"\t")
            paste(terminal, remote_name)
            terminal.key(b"\t")
            paste(terminal, remote_description)
            terminal.key(b"\r")  # description -> primary action without submitting
            terminal.wait("›[Create]")
            terminal.key(b"\x1b[Z")
            terminal.wait("›[Cancel]")
            terminal.key(b"\t")
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
            terminal.wait_absent("User-Agent")
            remote_id = profile_id(remote_name)
            remote_record = library()["profiles"][remote_id]
            assert len(requests) == before + 1, "New profile downloaded the remote source more than once"
            assert remote_agents == [remote_agent], "new remote profile used the wrong User-Agent"
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
            terminal.wait(remote_name)
            assert_saved_cards(terminal, 3)
            (output / "profiles-header.txt").write_text(terminal.screen.text())

            terminal.key(b"2")
            terminal.wait("Core not installed")
            terminal.settle()
            proxy_header = "\n".join(terminal.screen.text().splitlines()[:3])
            assert "Proxies" in proxy_header, "proxy header lost its page title"
            assert "Refresh" not in proxy_header and "↻" not in proxy_header, "proxy header retained a refresh action"
            for key, action in [(b"4", "[Pause]"), (b"5", "[Rule providers]"), (b"6", "[Clear]")]:
                terminal.key(key)
                header_row(terminal, action)
            terminal.key(b"7")
            terminal.wait("Download settings")
            terminal.key(b"1")
            terminal.wait("Not installed")
            terminal.key(b"3")
            provider_name[0] = "My Airport"
            terminal.key(b"a")
            paste(terminal, replacement_address)
            terminal.key(b"\r")
            wait_until(terminal, lambda: profile_id("My Airport (1)") is not None, "duplicate automatic name was not disambiguated")
            terminal.wait_absent("Fetching and validating")
            duplicate_id = profile_id("My Airport (1)")
            assert duplicate_id != primary_id[0], "duplicate import reused the existing profile ID"
            # A saved record precedes the TUI receiving the action result.
            # Wait for the new card before clicking the header.
            terminal.wait("My Airport (1)")
            for _ in range(2):
                before = len(requests)
                before_remote = len(remote_agents)
                previous = {pid: record["updated_at"] for pid, record in library()["profiles"].items()}
                header_row(terminal, "Update all")
                terminal.click_text("[Update all]")
                wait_until(terminal, lambda: len(requests) >= before + 3, "Update all did not refresh all three remote profiles")
                wait_until(terminal, lambda: all(record["updated_at"] != previous[pid] for pid, record in library()["profiles"].items()), "Update all did not save every refreshed profile")
                terminal.wait("Enter Update profiles")
                assert profile_id("My Airport (1)") == duplicate_id, "repeated updates accumulated a duplicate suffix or changed the ID"
                assert profile_id("My Airport") == primary_id[0], "duplicate refresh changed the custom profile name"
                assert profile_id(remote_name) == remote_id, "provider refresh changed the New profile custom name or ID"
                assert library()["profiles"][remote_id]["description"] == remote_description, "provider refresh discarded the description"
                assert remote_agents[before_remote:] == [remote_agent], "refresh did not reuse the saved User-Agent"
            terminal.exit()

            # Seed a one-time copy through the fixture and inspect it in the TUI.
            # Copies without a retained source have no Update action.
            subprocess.run(instance.fixture("profile", "add", str(local), "--name", "pty-snapshot"), check=True, capture_output=True, timeout=15)
            snapshot_id = profile_id("pty-snapshot")
            assert snapshot_id and "address" not in library()["profiles"][snapshot_id], "copy import unexpectedly retained its source"

            terminal = smoke.Terminal(command)
            terminals.append(terminal)
            terminal.wait("pty-snapshot")
            terminal.resize(160, 40)
            terminal.wait("My Airport")
            terminal.wait("My Airport (1)")
            terminal.wait("pty-local")
            terminal.wait(remote_name)
            assert_saved_cards(terminal, 5)
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
            terminal.wait("Settings…")
            menu = terminal.screen.text().split("[Update all]", 1)[-1]
            assert "Update" not in menu and "Reload from file" not in menu, "copy import offered a source update"
            assert len(requests) == before, "opening the copy-import menu sent a download request"
            assert not (home / "installation").exists(), "menu applied runtime state"
            terminal.key(b"\x1b")
            terminal.wait_absent("Settings…")
            mouse_text(terminal, "pty-snapshot", button=2)
            mouse_text(terminal, "Delete…")
            terminal.wait("Delete profile")
            terminal.wait("›[Cancel]")
            terminal.key(b"\r")
            terminal.wait_absent("Delete profile")
            assert profile_id("pty-snapshot") == snapshot_id, "default Enter deleted a profile instead of canceling"

            mouse_text(terminal, "pty-snapshot", button=2)
            mouse_text(terminal, "Delete…")
            terminal.wait("›[Cancel]")
            # A confirmation has no name field. Typed or pasted text must not
            # activate deletion or leak into an invisible input.
            terminal.key(b"q3")
            paste(terminal, "ignored-name")
            assert profile_id("pty-snapshot") == snapshot_id, "typing activated deletion"
            terminal.wait_absent("ignored-name")
            for columns, rows in [(120, 32), (60, 20), (40, 16), (24, 7)]:
                terminal.resize(columns, rows)
                terminal.wait("[Confirm delete]")
                terminal.wait("[Cancel]")
                (output / ("profiles-delete-%dx%d.txt" % (columns, rows))).write_text(terminal.screen.text())
            terminal.resize(120, 32)
            terminal.wait("[Confirm delete]")
            terminal.key(b"\t")
            terminal.wait("›[Confirm delete]")
            terminal.click_text("[Confirm delete]")
            wait_until(terminal, lambda: profile_id("pty-snapshot") is None, "confirmed deletion did not remove the selected profile")
            terminal.wait_absent("Deleting profile")
            assert len(requests) == before, "deletion requested a subscription download"
            (output / "profiles-delete-confirmed.txt").write_text(terminal.screen.text())
            terminal.exit()
            result = {
                "passed": True,
                "usage_scenarios": ["bare remaining and total bytes with bare future date", "card becomes Expired at cutoff without input", "Settings labels the balance and expiry date", "missing header clears old usage and expiry"],
                "scenarios": ["empty library has no built-in card or count summary", "first card is the first saved profile", "visible URL bracketed paste", "automatic Import without naming modal or Apply", "two-column arrows", "Enter opens Settings", "provider name update retains ID", "Settings protects provider names on description-only edits", "failed source validation retains all settings and form input", "changing the source and manual name together retains the stable ID", "menu Update", "Update all", "New profile local file with optional metadata omitted", "live directory and file matches", "arrow selection and Tab completion without creating", "Unicode and spaced directories with an extensionless configuration", "New remote profile with a custom name, description and User-Agent reused on refresh", "Tab and Shift+Tab reach Create and Cancel", "busy form ignores editing and duplicate submission", "one download per new remote profile", "retained local source refresh", "saved one-time copy import", "60x20 resize and Escape", "fixed header while scrolled", "header mouse update without Apply", "20x12 compact header", "page-specific controls", "duplicate imports retain distinct IDs", "repeated updates preserve duplicate suffix", "exit and reopen metadata and source persistence"],
                "menu_scenarios": ["right-click card without Apply", "right-click update icon opens containing card menu", "left-click menu button", "menu owns q, page digits and pasted text", "outside click closes without opening the underlying creation form", "keyboard m and Escape focus restoration", "menu Settings edits metadata atomically", "menu edge avoidance through resize", "retained local file Update rereads the source without HTTP", "copy-import Update is hidden"],
                "menu_dimensions": [[160, 40], [60, 20], [40, 16], [20, 12]],
                "delete_scenarios": ["default Enter cancels", "no name input or validation", "confirmation owns typed and pasted text", "confirmation stays visible from 120x32 to 24x7", "explicit mouse confirmation deletes once"],
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

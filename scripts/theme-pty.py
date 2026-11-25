#!/usr/bin/env python3
"""Verify Dark theme rendering through an isolated, real controlling PTY.

Requires an already built --binary (absolute path). It does not build, create a
VM, install a core/service or change system network settings. First-run
guidance may perform its automatic read-only stable-version check.
--output stores per-profile raw PTY captures and current-screen text snapshots.
Pass --notifications-only to exercise feedback, compact layouts and cancellation.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import uuid


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("lazyclash_pty_smoke", HERE / "pty-smoke.py")
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)

# Terminal deliberately defaults to NO_COLOR for the lifecycle test suite. This
# child-only wrapper selects each color capability without changing that suite,
# the caller's environment, or shell startup files.
WRAPPER = """
import os, sys
profile = sys.argv[1]
for name in ('NO_COLOR', 'COLORTERM', 'CLICOLOR', 'CLICOLOR_FORCE', 'TERM_PROGRAM', 'TMUX', 'TMUX_PANE'):
    os.environ.pop(name, None)
os.environ['TERM'] = 'xterm-256color'
if profile == 'truecolor':
    os.environ['COLORTERM'] = 'truecolor'
elif profile == 'no-color':
    os.environ['NO_COLOR'] = '1'
os.execv(sys.argv[2], sys.argv[2:])
"""
SIZES = [(160, 45), (120, 32), (80, 24), (60, 20), (120, 18)]
PREFIX = "https://pty.example.invalid/"
SGR = re.compile(rb"\x1b\[([0-9;:]*)m")
RGB = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]2[;:]")
INDEXED = re.compile(rb"\x1b\[[0-9;:]*[34]8[;:]5[;:]")
OSC_SET = re.compile(rb"\x1b\](?:10|11);(?!\?)")


def save_screen(terminal, output, profile, name):
    settle(terminal)
    (output / (profile + "-" + name + ".txt")).write_text(terminal.screen.text() + "\n")


def settle(terminal):
    deadline, quiet = time.monotonic() + 4, 0
    while time.monotonic() < deadline:
        before = len(terminal.raw)
        terminal.read(0.03)
        complete = not terminal.screen.synchronized and terminal.screen.state == "text"
        quiet = quiet + 1 if len(terminal.raw) == before and complete else 0
        if quiet == 2:
            return
    raise AssertionError("PTY did not finish a frame\n" + terminal.screen.text())


def enter_network(terminal):
    terminal.key(b"n")
    terminal.wait("Use environment proxy")


def focused_prefix(terminal):
    settle(terminal)
    terminal.wait("Acceleration URL")
    # The input draws its native cursor immediately after the typed URL.
    screen = terminal.screen
    row = screen.cells[screen.y]
    cursor_shown = terminal.raw.rfind(b"\x1b[?25h") > terminal.raw.rfind(b"\x1b[?25l")
    return cursor_shown and "".join(row[:screen.x]).endswith(PREFIX)


def corner_boxes(screen):
    for top, row in enumerate(screen.cells):
        for right in range(max(0, screen.columns-5), screen.columns):
            if row[right] != "╮" or "╭" not in row[:right]:
                continue
            left = max(i for i in range(right) if row[i] == "╭")
            if screen.columns >= 100 and left < screen.columns // 2:
                continue
            for bottom in range(top+1, screen.rows-1):
                below = screen.cells[bottom]
                if below[left] == "╰" and below[right] == "╯":
                    yield left, top, right, bottom
                    break
                if below[left] != "│" or below[right] != "│":
                    break


def list_cards(terminal, underlying, minimum=1, title="Notifications", collapse="Esc Collapse",
               details="Details", close="× Close"):
    terminal.wait(title)
    terminal.wait(underlying)
    terminal.wait(collapse)
    settle(terminal)
    screen = terminal.screen
    lines = screen.text().splitlines()
    heading = next((y for y, line in enumerate(lines[:5]) if title in line), -1)
    footer = next((y for y, line in enumerate(lines) if collapse in line), -1)
    # Each complete card owns its controls. No enclosing frame swallows the
    # page or groups multiple operations into one undifferentiated surface.
    boxes = []
    for left, top, right, bottom in corner_boxes(screen):
        if top <= heading < bottom:
            raise AssertionError("Notification list has an enclosing frame\n" + screen.text())
        if 0 <= heading < top < bottom < footer:
            text = "\n".join("".join(row[left+1:right]) for row in screen.cells[top+1:bottom])
            if details in text and close in text:
                boxes.append((left, top, right, bottom))
    if len(boxes) < minimum:
        raise AssertionError("Too few independent notification cards with controls\n" + screen.text())
    return boxes


def detail_corner(terminal, underlying, title="Details"):
    terminal.wait(title)
    terminal.wait(underlying)
    settle(terminal)
    screen = terminal.screen
    for left, top, right, bottom in corner_boxes(screen):
        if top < 5 and title in "".join(screen.cells[top+1][left:right]):
            return left, top, right
    raise AssertionError("Notification detail did not expand in the upper right\n" + screen.text())


def click_card_button(terminal, box, label):
    left, top, right, bottom = box
    for y in range(top+1, bottom):
        text = "".join(terminal.screen.cells[y][left:right+1])
        if label in text:
            x = left + SMOKE.cell_width(text[:text.index(label)])
            terminal.key(f"\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m".encode())
            return
    raise AssertionError("Missing button in selected card: " + label)


def empty_shortcut(terminal, underlying):
    terminal.wait(underlying)
    terminal.key(b"\x0e")
    settle(terminal)
    screen = terminal.screen.text()
    if underlying not in screen or "No notifications" in screen or "Esc Collapse" in screen:
        raise AssertionError("Ctrl+N opened an empty surface or left the original page\n" + screen)


def exercise_notifications(terminal, output, profile):
    # Three independent commands share a cause, but retain their own context.
    # The uninstalled guards do not touch a service or query a release.
    # Settings accepts all three service shortcuts. Overview reserves r.
    terminal.key(b"7")
    terminal.wait("Interface preferences")
    empty_shortcut(terminal, "Interface preferences")
    error = "Mihomo is not installed"
    terminal.key(b"s")
    terminal.wait(error)
    terminal.wait("Start service")
    terminal.key(b"r")
    terminal.wait("Restart service")
    screen = terminal.screen.text().splitlines()
    start_y = next(i for i, line in enumerate(screen) if "Start service" in line)
    restart_y = next(i for i, line in enumerate(screen) if "Restart service" in line)
    if start_y >= restart_y or sum(error in line for line in screen[:restart_y + 3]) != 2:
        raise AssertionError("independent failures were merged or hidden")
    deadline = time.monotonic() + 5.2
    while time.monotonic() < deadline:
        terminal.read()
    terminal.wait("Start service")
    terminal.wait("Restart service")
    terminal.key(b"x")
    terminal.wait("more")
    terminal.wait("Expand")
    screen = terminal.screen.text().splitlines()
    restart_y = next(i for i, line in enumerate(screen) if "Restart service" in line)
    stop_y = next(i for i, line in enumerate(screen) if "Stop service" in line)
    if restart_y >= stop_y:
        raise AssertionError("visible notifications are not ordered oldest first")
    save_screen(terminal, output, profile, "notification-overflow")

    terminal.click_text("Expand")
    list_cards(terminal, "Interface preferences", minimum=2)
    terminal.click_text("Proxies")  # Outside click collapses without opening that page.
    terminal.wait_absent("Esc Collapse")
    terminal.wait("Interface preferences")
    terminal.key(b"\x0e")
    list_cards(terminal, "Interface preferences", minimum=2)
    terminal.key(b"\x0e")
    terminal.wait_absent("Esc Collapse")
    terminal.click_text("Expand")
    list_cards(terminal, "Interface preferences", minimum=2)
    for context in ("Start service", "Restart service", "Stop service"):
        terminal.wait(context)
    save_screen(terminal, output, profile, "notification-list")
    cards = list_cards(terminal, "Interface preferences", minimum=2)
    click_card_button(terminal, cards[1], "Details")
    terminal.wait("Esc Back")
    detail_corner(terminal, "Interface preferences")
    terminal.wait("Restart service")
    terminal.wait_absent("Start service")
    terminal.key(b"\x1b")
    cards = list_cards(terminal, "Interface preferences", minimum=2)
    click_card_button(terminal, cards[0], "× Close")
    terminal.wait_absent("Start service")
    terminal.wait("Restart service")
    terminal.wait("Stop service")
    terminal.key(b"\x1b")
    terminal.wait_absent("Esc Collapse")
    for columns, rows in SIZES:
        terminal.resize(columns, rows)
        # A resized emulator initially contains cropped cells from the old
        # frame. Wait for complete new geometry without assuming one-line text.
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            terminal.read()
            screen = terminal.screen.text().splitlines()
            if (not terminal.screen.synchronized
                    and any(error in line for line in screen)
                    and any("Stop service" in line for line in screen)
                    and screen[-1].find("q Quit") == columns - 8):
                break
        else:
            raise AssertionError("notification resize did not finish rendering")
        error_y = next(i for i, line in enumerate(screen) if error in line)
        if error_y > 5 or any(error in line for line in screen[-2:]):
            raise AssertionError("Toast is not in the upper-right corner")
        save_screen(terminal, output, profile, "notification-%sx%s" % (columns, rows))

    terminal.key(b"s")
    terminal.wait("Start service")
    terminal.resize(40, 24)
    terminal.key(b"3")
    terminal.wait("Error · 3 more · Expand")
    terminal.key(b"\x0e")  # Ctrl+N opens the bounded collection from any page.
    cards = list_cards(terminal, "Profiles")
    terminal.wait(error)
    terminal.wait("Error · Settings · Restart")
    if len(cards) != 2:
        raise AssertionError("Compact viewport should show two complete error cards")
    left, top, right, bottom = cards[0]
    terminal.key(f"\x1b[<65;{(left+right)//2+1};{(top+bottom)//2+1}M".encode())
    cards = list_cards(terminal, "Profiles")
    _, last_top, _, _ = cards[-1]
    if "Start service" not in terminal.screen.text().splitlines()[last_top+1]:
        raise AssertionError("Wheel did not reveal the last notification card")
    click_card_button(terminal, cards[-1], "× Close")
    terminal.wait_absent("Start service")
    terminal.key(b"\x1b[H")
    terminal.wait("Error · Settings · Restart")
    terminal.key(b"x")
    terminal.wait_absent("Error · Settings · Restart")
    terminal.key(b"\x1b")
    terminal.wait_absent("Esc Collapse")
    # The single folded error opens its own detail and returns directly to Profiles.
    terminal.wait("Error · " + error)
    save_screen(terminal, output, profile, "notification-single-summary-40x24")
    terminal.click_text(error)
    terminal.wait("Stop service")
    terminal.wait("Esc Collapse")
    terminal.key(b"\x1b")
    terminal.wait_absent("Esc Collapse")
    terminal.click_text(error)
    terminal.wait("Stop service")
    terminal.key(b"x")
    terminal.wait_absent("Stop service")
    empty_shortcut(terminal, "Profiles")

    terminal.resize(120, 32)
    terminal.key(b"a")
    terminal.key(b"1243\r")
    validation = "subscription source must be"
    terminal.wait(validation)
    terminal.wait("1243")
    if "Error ·" in terminal.screen.text() or "Expand" in terminal.screen.text():
        raise AssertionError("input validation generated a global notification")
    save_screen(terminal, output, profile, "notification-local-validation")
    terminal.key(b"\x01\x0bhttps://example.invalid/sub")
    terminal.wait_absent(validation)
    terminal.key(b"\t")  # End editing before using a page shortcut.
    terminal.key(b"1")
    terminal.wait("Proxy mode")
    terminal.wait_absent(validation)
    terminal.key(b"s")
    terminal.wait("Start service")  # The same failure can be shown again after closing.
    terminal.key(b"\x0e")
    terminal.wait("Notifications")
    terminal.key(b"x")
    terminal.wait_absent("Start service")
    empty_shortcut(terminal, "Proxy mode")

    terminal.key(b"a")
    terminal.wait("Import configuration")
    terminal.key(b"\r")
    terminal.wait("A value is required")
    terminal.key(b"\x1b")
    terminal.wait_absent("Import configuration")
    terminal.wait_absent("A value is required")
    empty_shortcut(terminal, "Proxy mode")
    save_screen(terminal, output, profile, "notification-canceled")
    terminal.resize(160, 45)
    terminal.wait("Proxy mode")


def exercise_long_detail(terminal, output, profile, command, directory):
    # Real profile imports followed by a missing source produce a multiline
    # diagnostic with distinct first/last markers, without installing a core.
    source = directory / "notice-source.yaml"
    source.write_text('rules: ["MATCH,DIRECT"]\n')
    for i in range(1, 19):
        subprocess.run([*command, "profile", "add", str(source), "--remember-source", "--name", f"Lab-{i:02d}"],
                       check=True, capture_output=True, timeout=10)
    source.unlink()
    terminal.key(b"g")
    terminal.key(b"3")
    # The header label is already painted while the initial snapshot is empty.
    terminal.wait("Lab-01")
    terminal.wait("Update all")
    terminal.click_text("Update all")
    terminal.wait("Profiles updated: 0. Failed: 18")
    terminal.key(b"7")
    terminal.wait("Interface preferences")
    terminal.click_text("Details")
    terminal.wait("Lab-01")
    detail_corner(terminal, "Interface preferences")
    save_screen(terminal, output, profile, "notification-detail-160x45")
    terminal.key(b"\x1b[F")  # End
    terminal.wait("Lab-18")
    terminal.key(b"\x1b")
    terminal.wait_absent("Esc Collapse")
    terminal.wait("Interface preferences")

    terminal.resize(40, 24)
    terminal.key(b"\x0e")
    list_cards(terminal, "Settings")
    save_screen(terminal, output, profile, "notification-list-40x24")
    terminal.key(b"\r")
    terminal.wait("Lab-01")
    left, top, right = detail_corner(terminal, "Settings")
    save_screen(terminal, output, profile, "notification-detail-40x24")
    if "Lab-18" in terminal.screen.text():
        raise AssertionError("Long detail did not require scrolling at 40×24")
    for _ in range(40):
        terminal.key(f"\x1b[<65;{(left + right)//2 + 1};{top + 4}M".encode())
        if "Lab-18" in terminal.screen.text():
            break
    terminal.wait("Lab-18")
    terminal.key(b"\x1b")
    terminal.wait_absent("Lab-18")
    list_cards(terminal, "Settings")
    terminal.key(b"x")
    terminal.wait_absent("Esc Collapse")
    empty_shortcut(terminal, "Settings")
    terminal.resize(160, 45)
    terminal.key(b"1")
    terminal.wait("Proxy mode")


def exercise_network(terminal, output, profile):
    # Network setup is a single method choice. Prefixes are visible
    # public URLs, and abandoning the modal discards the complete draft.
    enter_network(terminal)
    terminal.key(b"\r")  # Direct -> environment
    terminal.key(b"\r")  # Environment -> GitHub acceleration
    terminal.wait("Acceleration URL")
    terminal.key(b"\t")
    terminal.key(PREFIX.encode("ascii"))
    terminal.wait(PREFIX)
    for columns, rows in SIZES:
        mark = len(terminal.text())
        terminal.resize(columns, rows)
        terminal.wait(PREFIX, after=mark)
        if not focused_prefix(terminal):
            raise AssertionError("resize lost the draft input or its focus at %sx%s" % (columns, rows))
        terminal.wait("Esc")
        save_screen(terminal, output, profile, "network-%sx%s" % (columns, rows))
    terminal.resize(120, 32)
    settle(terminal)
    terminal.wait(PREFIX)
    terminal.key(b"\x1b")
    terminal.wait_absent("Acceleration URL")
    enter_network(terminal)
    terminal.wait("Direct")
    terminal.wait_absent("Acceleration URL")
    terminal.wait_absent(PREFIX)
    terminal.key(b"\x1b")
    terminal.wait_absent("Use environment proxy")
    terminal.wait("Proxy mode")


def exercise(binary, output, profile, notifications_only):
    terminal = None
    with tempfile.TemporaryDirectory(prefix="lazyclash-theme-pty-") as temporary:
        home = Path(temporary) / "home"
        service = "lazyclash-theme-" + uuid.uuid4().hex[:12]
        product = [str(binary), "--home", str(home), "--service-name", service]
        command = [sys.executable, "-c", WRAPPER, profile, *product]
        try:
            terminal = SMOKE.Terminal(command)
            terminal.wait("Overview")
            terminal.wait_absent("Loading state")
            terminal.wait("q Quit")
            mark = len(terminal.text())
            terminal.resize(160, 45)
            terminal.wait("Install the Mihomo core", after=mark)
            terminal.click_text("Set up later")
            terminal.wait("Proxy mode")
            save_screen(terminal, output, profile, "overview")
            exercise_notifications(terminal, output, profile)
            exercise_long_detail(terminal, output, profile, product, Path(temporary))
            if not notifications_only:
                exercise_network(terminal, output, profile)
            terminal.exit()

            raw = bytes(terminal.raw)
            if OSC_SET.search(raw):
                raise AssertionError("TUI set the terminal's global foreground/background")
            if profile == "truecolor" and not RGB.search(raw):
                raise AssertionError("True Color run emitted no RGB styling")
            if profile == "256" and (RGB.search(raw) or not INDEXED.search(raw)):
                raise AssertionError("256-color run did not exclusively use its supported color representation")
            if profile == "no-color":
                # Terminal renderers may reset SGR on entry/exit; NO_COLOR must
                # contain no actual foreground/background/decorative styles.
                styled = [m.group(1) for m in SGR.finditer(raw) if m.group(1) not in (b"", b"0")]
                if styled:
                    raise AssertionError("NO_COLOR run emitted SGR styling: " + repr(styled[:4]))
            if (home / "installation").exists() or (home / "preferences.json").exists():
                raise AssertionError("read-only theme exercise installed a core or saved preferences")
            return {"profile": profile, "status": "passed", "sizes": SIZES + [(40, 24)],
                    "notifications_only": notifications_only,
                    "rgb": bool(RGB.search(raw)), "indexed": bool(INDEXED.search(raw))}
        finally:
            if terminal is not None:
                (output / (profile + ".ansi")).write_bytes(terminal.raw)
                terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="Absolute path to an already built lazyclash")
    parser.add_argument("--output", type=Path, required=True, help="Directory for isolated PTY evidence")
    parser.add_argument("--profile", choices=("all", "truecolor", "256", "no-color"), default="all")
    parser.add_argument("--notifications-only", action="store_true", help="Run just the notification checks through the product TUI")
    args = parser.parse_args()
    if not args.binary.is_absolute() or not args.binary.is_file() or not os.access(args.binary, os.X_OK):
        parser.error("--binary must be an absolute path to an executable file")
    args.output.mkdir(parents=True, exist_ok=True)
    profiles = ("truecolor", "256", "no-color") if args.profile == "all" else (args.profile,)
    results = [exercise(args.binary, args.output, profile, args.notifications_only) for profile in profiles]
    (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    for result in results:
        print("Dark theme PTY %s: passed (%s sizes)" % (result["profile"], len(result["sizes"])))


if __name__ == "__main__":
    main()

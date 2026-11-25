#!/usr/bin/env python3
"""Verify Settings in a real controlling PTY with an opt-in memory backend.

Build: go test -c ./internal/tui -o bin/settings-tui.test
No core, network, managed files or service is used. Optional PNGs redraw actual
PTY cells and colors; they are not native terminal-app screenshots.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid

from tui_layout import sidebar_visible


SIZES = ((240, 45), (200, 40), (160, 45), (120, 32), (105, 32), (104, 32),
         (100, 28), (99, 28), (84, 24), (83, 24), (80, 24), (60, 20), (40, 16), (240, 45))


def labels(language):
    result = {}
    directory = Path(__file__).resolve().parents[1] / "internal" / "i18n" / "locales"
    for path in directory.glob(language + ".*.json"):
        result.update(json.loads(path.read_text()))
    return result


def position(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        for x, cell in enumerate(row):
            if cell and "".join(row[x:]).startswith(label):
                return x, y
    raise AssertionError("Visible text missing: " + label + "\n" + terminal.screen.text())


def click(terminal, point):
    x, y = point
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())


def focus(terminal, label, footer_hint=""):
    pattern = re.compile(r"›\s*(?:[<\[]\s*)?" + re.escape(label))
    for _ in range(36):
        terminal.settle()
        screen = terminal.screen.text()
        if pattern.search(screen) and footer_hint in screen.splitlines()[-1]:
            return
        terminal.key(b"\t")
    raise AssertionError("Tab did not reveal " + label + "\n" + terminal.screen.text())


def fixture_terminal(preview, binary, report, language, profile):
    environment = {"LAZYCLASH_SETTINGS_PTY_REPORT": str(report), "LAZYCLASH_SETTINGS_PTY_LANGUAGE": language}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestSettingsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def exercise(preview, binary, output, language, profile, args):
    output.mkdir(parents=True)
    words = labels(language)
    report = output / "calls.json"
    terminal = fixture_terminal(preview, binary, report, language, profile)
    try:
        terminal.wait("Settings preview")
        terminal.key(b"7")
        terminal.wait(words["settings.interface"])
        terminal.settle()
        for heading in ("settings.interface", "settings.serviceGroup", "settings.core", "settings.diagnostics"):
            terminal.wait(words[heading])
        terminal.wait(words["dashboard.network.mixedPort"] % "7897")
        terminal.wait("lazyclash doctor")
        preview.capture(terminal, output, "settings-wide", args)

        terminal.click_text("Dark ▾")
        terminal.wait("✓ Dark")
        terminal.wait("    Auto ")
        _, checked_y = position(terminal, "✓ Dark")
        assert position(terminal, "Light")[1] == checked_y - 1, "Light must precede Dark"
        assert position(terminal, "Auto ")[1] == checked_y + 1, "Auto must follow Dark"
        preview.capture(terminal, output, "theme-menu-wide", args)
        terminal.key(b"\x1b[A")
        terminal.key(b"\x1b")
        terminal.wait_absent("✓ Dark")
        terminal.wait("Dark ▾")

        terminal.key(b"\r")  # Canceling the dropdown returns focus to its trigger.
        terminal.wait("✓ Dark")
        terminal.key(b"\x1b[A")
        terminal.key(b"\r")
        terminal.wait("Light ▾")
        terminal.wait(words["settings.themeLightPlaceholder"])
        terminal.click_text("Light ▾")
        terminal.wait("✓ Light")
        mark = len(terminal.raw)
        terminal.click_text("    Auto ")
        terminal.wait("Auto ▾")
        terminal.wait(words["settings.themeAuto"] % "Dark")
        terminal.settle()
        assert b"\x1b]11;?" in terminal.raw[mark:], "Auto did not query the terminal background"
        terminal.send(b"\x1b]11;rgb:ffff/ffff/ffff\x07")
        terminal.wait(words["settings.themeAuto"] % "Light")
        terminal.click_text("Auto ▾")
        terminal.wait("✓ Auto")
        terminal.click_text("    Dark")
        terminal.wait("Dark ▾")

        other = "zh-Hans" if language == "en" else "en"
        native = {"en": "English", "zh-Hans": "简体中文"}
        current = native[language] + " ▾"
        switched = native[other] + " ▾"
        assert native[other] not in terminal.screen.text(), "closed language dropdown shows both languages"
        assert position(terminal, words["dashboard.interface.keyHints"])[1] == position(terminal, current)[1] + 2, "unexpected text below the language control"
        current_point = position(terminal, current)
        click(terminal, current_point)
        terminal.wait("✓ " + native[language])
        terminal.wait(native[other])
        preview.capture(terminal, output, "language-menu-wide", args)
        move = b"\x1b[B" if language == "en" else b"\x1b[A"
        terminal.key(move)
        terminal.key(b"\x1b")
        terminal.wait_absent(native[other])
        terminal.key(b"\r")
        terminal.wait("✓ " + native[language])
        terminal.key(move)
        terminal.key(b"\r")
        click(terminal, current_point)  # The pending save disables the trigger.
        terminal.wait(labels(other)["settings.interface"])
        terminal.wait(switched)
        terminal.settle()
        preview.capture(terminal, output, "language-switched", args)
        terminal.click_text(switched)
        terminal.wait("✓ " + native[other])
        terminal.click_text(native[language])
        terminal.wait(words["settings.interface"])
        terminal.settle()

        # Exercise the row's mouse target and the global recovery shortcut.
        _, y = position(terminal, words["dashboard.interface.keyHints"])
        on = words["dashboard.control.on"]
        for row_y in range(y, min(y + 3, terminal.screen.rows)):
            row = terminal.screen.cells[row_y]
            points = [(x, row_y) for x, cell in enumerate(row) if cell and "".join(row[x:]).startswith(on)]
            if points:
                click(terminal, points[0])
                break
        else:
            raise AssertionError("Shortcut hints row has no toggle")
        terminal.settle()
        assert words["dashboard.control.off"] in terminal.screen.text(), "mouse did not turn hints off"
        preview.capture(terminal, output, "hints-hidden", args)
        terminal.key(b"\x14")  # Ctrl+T restores the hidden footer.
        terminal.settle()

        # Completed preference Toasts can cover the right card's top border.
        # Close them through their real mouse targets before measuring layout.
        for _ in range(8):
            if "×" not in terminal.screen.text():
                break
            click(terminal, position(terminal, "×"))
            terminal.settle()
        assert "×" not in terminal.screen.text(), "preference Toasts still cover the Settings cards"

        for width, height in SIZES:
            terminal.resize(width, height)
            terminal.settle()
            terminal.key(b"\x1b[H")
            terminal.settle()
            terminal.wait(words["settings.interface"])
            sidebar = sidebar_visible(width, height)
            left, right = (22, width - 4) if sidebar else (2, width - 3)
            columns = 2 if right - left + 1 >= 80 else 1
            borders = [row for row in terminal.screen.cells if row[left] == "┌"]
            assert borders, "Settings cards have no visible top border"
            assert borders[0][right] == "┐", "Settings cards did not stretch to the available right edge"
            assert borders[0].count("┌") == columns, "Settings column count did not follow the available width"
            preview.capture(terminal, output, "settings-%dx%d" % (width, height), args)

            # Scroll down and up in the body. The short view must reveal lower
            # groups; each Tab pass then reveals the exact focused control.
            before = terminal.screen.text()
            for _ in range(8):
                terminal.key(("\x1b[<65;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.settle()
            if columns == 1:
                assert before != terminal.screen.text(), "wheel did not scroll the narrow Settings body"
            preview.capture(terminal, output, "scrolled-%dx%d" % (width, height), args)
            if width == 40:
                focus(terminal, current)
                terminal.key(b"\r")
                terminal.wait("✓ " + native[language])
                preview.capture(terminal, output, "language-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent(native[other])
                focus(terminal, "Dark ▾")
                terminal.key(b"\r")
                terminal.wait("✓ Dark")
                preview.capture(terminal, output, "theme-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓ Dark")
            focus(terminal, words["settings.openLogs"])
            preview.capture(terminal, output, "tab-logs-%dx%d" % (width, height), args)
            focus(terminal, words["settings.edit"], words["navigation.controls.editPorts"])
            terminal.key(b"\r")
            terminal.wait(words["ports.title"])
            terminal.wait("7897")
            preview.capture(terminal, output, "ports-%dx%d" % (width, height), args)
            terminal.key(b"\x1b")
            terminal.wait_absent(words["ports.title"])
            terminal.settle()
            assert re.search(r"›\s*(?:\[\s*)?" + re.escape(words["settings.edit"]), terminal.screen.text()), "port return lost its trigger focus"
            for _ in range(8):
                terminal.key(("\x1b[<64;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.key(b"\x1b[H")
            terminal.settle()

        # The diagnostics action only opens the read-only Logs page. The
        # Settings shortcut restores the existing page after its stream stops.
        focus(terminal, words["settings.openLogs"])
        terminal.key(b"\r")
        terminal.wait("settings-pty readonly log")
        preview.capture(terminal, output, "diagnostics-open-logs", args)
        terminal.key(b"7")
        terminal.wait(words["settings.interface"])
        terminal.settle()
        preview.capture(terminal, output, "logs-return-settings", args)

        terminal.exit()
        calls = json.loads(report.read_text())
        assert calls["language_saves"] == 2 and calls["language"] == language, calls
        assert calls["hint_saves"] == 2 and not calls["hints_hidden"], calls
        assert calls["theme_saves"] == 3 and calls["theme"] == "dark", calls
        assert calls["port_reads"] == len(SIZES), calls
        assert calls["log_streams"] == 1, calls
        assert calls["service_changes"] == calls["mode_changes"] == calls["port_applies"] == 0, calls
        raw = bytes(terminal.raw)
        assert not preview.OSC_SET.search(raw), "application changed the terminal palette"
        if profile == "256":
            assert not preview.RGB.search(raw), "ANSI256 emitted RGB colors"
        if profile == "no-color":
            assert not any(value not in (b"", b"0") for value in preview.SGR.findall(raw)), "NO_COLOR emitted styling"
        (output / "pty.raw").write_bytes(raw)
        print("PASS %s/%s: theme and language dropdowns, busy guard, hints, adaptive width/columns, wheel, Tab, ports return and diagnostics Logs; %s" % (language, profile, json.dumps(calls)), flush=True)
    except Exception:
        (output / "failed.txt").write_text(terminal.screen.text() + "\n")
        (output / "failed.ansi").write_bytes(terminal.raw)
        if args.png:
            preview.png(terminal.screen, output / "failed.png", args.font, args.cjk_font, args.font_size)
        raise
    finally:
        terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--lang", choices=["en", "zh-Hans"], action="append")
    parser.add_argument("--profile", choices=["truecolor", "256", "no-color"], action="append")
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("settings_pty_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    output = args.output.resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "256", "no-color"):
            exercise(preview, args.binary, output / language / profile, language, profile, args)
    print("Evidence:", output)


if __name__ == "__main__":
    main()

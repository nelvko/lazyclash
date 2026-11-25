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


def switch_track(enabled, style="slanted", profile="truecolor"):
    if style == "slider":
        if profile != "no-color":
            return " " * 6
        style = "text"
    return {"slanted": ("〇╱╱╱╱", "◢◤◢◤〇"), "line": ("●──", "━━●"),
            "text": ("OFF", " ON")}[style][enabled]


def assert_slider(terminal, x, y, enabled):
    cells = terminal.screen.cells[y][x:x + 6]
    colors = [cell[1] for cell in terminal.screen.styles[y][x:x + 6]]
    if enabled:
        colors.reverse()
    assert cells == [" "] * 6 and colors[0] != colors[2], (cells, colors)
    assert colors == [colors[0]] * 2 + [colors[2]] * 4, colors


def check_switch(terminal, words, key, enabled, profile, style="slanted"):
    # Observe the committed endpoint after any pending operation and slide.
    deadline = time.monotonic() + 3
    while True:
        label_x, y = position(terminal, words[key])
        row = terminal.screen.cells[y]
        right = row.index("│", label_x)
        x = right - 8
        face = switch_track(enabled, style, profile)
        if "".join(row[x:right - 2]).lstrip() == face.lstrip() and row[right - 2:right] == [" ", " "]:
            break
        if time.monotonic() >= deadline:
            raise AssertionError((key, enabled, style, "".join(row[x:right])))
        terminal.read(0.05)
    if style == "slider" and profile != "no-color":
        assert_slider(terminal, x, y, enabled)
    return x, y


def exercise_styles(preview, terminal, words, profile, output, args):
    current = "slanted"
    initial = {key: check_switch(terminal, words, key, enabled, profile)
               for key, enabled in (("settings.ipv6", True), ("settings.unifiedDelay", False))}
    for choice in ("slider", "text", "line", "slanted"):
        name = lambda style: words["settings.switchStyle." + style]
        terminal.click_text(name(current) + " ▾")
        terminal.wait("✓ " + name(current))
        terminal.wait("●── → ━━●")
        if choice == "slider":
            _, top = position(terminal, "✓ " + name("slanted"))
            for index, style in enumerate(("slanted", "slider", "text", "line")):
                y = top + index
                row = terminal.screen.cells[y]
                arrow = row.index("→")
                for enabled in (False, True):
                    face = switch_track(enabled, style, profile)
                    face_width = preview.SMOKE.cell_width(face)
                    x = arrow + 2 if enabled else arrow - 1 - face_width
                    assert "".join(row[x:x + face_width]) == face
                    if style == "slider" and profile != "no-color":
                        assert_slider(terminal, x, y, enabled)
            preview.capture(terminal, output, "switch-style-menu", args)
        terminal.click_text("  " + name(choice) + " ")
        terminal.wait(name(choice) + " ▾")
        for key, enabled in (("settings.ipv6", True), ("settings.unifiedDelay", False)):
            assert check_switch(terminal, words, key, enabled, profile, choice) == initial[key]
        preview.capture(terminal, output, "switch-style-" + choice, args)
        current = choice


def focus(terminal, label, footer_hint=""):
    pattern = re.compile(r"›\s*(?:[<\[]\s*)?" + re.escape(label))
    for _ in range(36):
        terminal.settle()
        screen = terminal.screen.text()
        if pattern.search(screen) and footer_hint in screen.splitlines()[-1]:
            return
        terminal.key(b"\t")
    raise AssertionError("Tab did not reveal " + label + "\n" + terminal.screen.text())


def dismiss_toasts(terminal):
    for _ in range(8):
        terminal.settle()
        if "×" not in terminal.screen.text():
            return
        click(terminal, position(terminal, "×"))
    raise AssertionError("Toasts still cover the Settings cards")


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
        for heading in ("settings.interface", "settings.mihomo", "settings.serviceGroup", "settings.core", "settings.diagnostics"):
            terminal.wait(words[heading])
        terminal.wait(words["dashboard.network.mixedPort"] % "7897")
        terminal.wait("lazyclash doctor")
        preview.capture(terminal, output, "settings-wide", args)

        switch_defaults = {"settings.ipv6": True, "settings.unifiedDelay": False,
                           "dashboard.interface.keyHints": True, "settings.autostart": True}
        for key in ("dashboard.language.title", "settings.theme", "settings.switchStyle", "settings.logLevel", *switch_defaults):
            x, y = position(terminal, words[key])
            row = terminal.screen.cells[y]
            right = row.index("│", x)
            assert row[x-3:x] == ["│", " ", " "] and row[right-2:right] == [" ", " "], key
            if key in switch_defaults:
                check_switch(terminal, words, key, switch_defaults[key], profile)
            else:
                assert row[right-4] == "▾", "dropdown lacks its two-cell inset: " + key

        exercise_styles(preview, terminal, words, profile, output, args)
        native = {"en": "English", "zh-Hans": "简体中文"}
        current = native[language] + " ▾"
        terminal.resize(24, 24)
        terminal.settle()
        terminal.wait(current)
        label_x, label_y = position(terminal, words["dashboard.language.title"])
        trigger = position(terminal, current)
        # The button reserves two cells for its focus marker and one for face padding.
        assert trigger == (label_x + 3, label_y + 1), "wrapped dropdown did not start below its label"
        preview.capture(terminal, output, "settings-wrapped-24x24", args)
        click(terminal, trigger)
        terminal.wait("✓ " + native[language])
        terminal.key(b"\x1b")
        terminal.wait_absent("✓ " + native[language])
        terminal.key(b"\t")
        terminal.settle()
        wrapped = position(terminal, switch_track(True))
        label = position(terminal, words["dashboard.interface.keyHints"].split()[0])
        assert wrapped[1] > label[1] and wrapped[0] == label[0] + 2, "switch did not wrap after the focus marker"
        assert terminal.screen.cells[wrapped[1]][label[0]:wrapped[0]] == ["›", " "]
        preview.capture(terminal, output, "switch-wrapped-24x24", args)
        click(terminal, wrapped)
        terminal.wait(switch_track(False))
        terminal.key(b"\x14")
        terminal.wait(switch_track(True))
        dismiss_toasts(terminal)
        terminal.resize(160, 45)
        terminal.settle()

        # Exercise both edges of the six-cell slanted switch.
        x, y = check_switch(terminal, words, "settings.ipv6", True, profile)
        before_hover = terminal.screen.styles[y][x:x + 6]
        terminal.key(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())
        terminal.settle()
        check_switch(terminal, words, "settings.ipv6", True, profile)
        if profile != "no-color":
            assert terminal.screen.styles[y][x:x + 6] != before_hover, "hover did not highlight the switch"
        click(terminal, (x, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.ipv6", False, profile)
        x, y = check_switch(terminal, words, "settings.unifiedDelay", False, profile)
        click(terminal, (x + 5, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", True, profile)
        preview.capture(terminal, output, "mihomo-switches-wide", args)
        terminal.key(b"\r")
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", False, profile)

        terminal.click_text("info ▾")
        terminal.wait("✓ info")
        for level in ("debug", "warning", "error", "silent"):
            terminal.wait("    " + level + " ")
        assert all(value not in terminal.screen.text() for value in ("Follow profile", "跟随配置"))
        preview.capture(terminal, output, "log-level-menu-wide", args)
        terminal.click_text("    debug ")
        terminal.wait("debug ▾")
        dismiss_toasts(terminal)

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
        x, y = check_switch(terminal, words, "dashboard.interface.keyHints", True, profile)
        click(terminal, (x + 1, y))
        terminal.settle()
        check_switch(terminal, words, "dashboard.interface.keyHints", False, profile)
        preview.capture(terminal, output, "hints-hidden", args)
        terminal.key(b"\x14")  # Ctrl+T restores the hidden footer.
        terminal.settle()

        # Completed preference Toasts can cover the right card's top border.
        # Close them through their real mouse targets before measuring layout.
        dismiss_toasts(terminal)

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
                focus(terminal, "debug ▾")
                terminal.key(b"\r")
                terminal.wait("✓ debug")
                preview.capture(terminal, output, "log-level-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓ debug")
                terminal.settle()
                x, y = check_switch(terminal, words, "settings.ipv6", False, profile)
                click(terminal, (x, y))
                dismiss_toasts(terminal)
                check_switch(terminal, words, "settings.ipv6", True, profile)
                check_switch(terminal, words, "settings.unifiedDelay", False, profile)
                preview.capture(terminal, output, "mihomo-switches-40x16", args)
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
        assert calls["hint_saves"] == 4 and not calls["hints_hidden"], calls
        assert calls["switch_style_saves"] == 4 and calls["switch_style"] == "slanted", calls
        assert calls["theme_saves"] == 3 and calls["theme"] == "dark", calls
        assert calls["core_changes"] == [["ipv6", "off"], ["unified-delay", "on"],
                                         ["unified-delay", "off"], ["log-level", "debug"],
                                         ["ipv6", "on"]], calls
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
        print("PASS %s/%s: core switches and log level, theme and language dropdowns, busy guard, hints, adaptive width/columns, wheel, Tab, ports return and diagnostics Logs; %s" % (language, profile, json.dumps(calls)), flush=True)
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

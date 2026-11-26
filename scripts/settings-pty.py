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

from tui_layout import dropdown_option, dropdown_trigger, sidebar_visible


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


def switch_track(enabled):
    return "░░░░██" if enabled else "██░░░░"


def card_bounds(screen, label_x):
    sidebar = sidebar_visible(screen.columns, screen.rows)
    left, end = (22, screen.columns - 3) if sidebar else (2, screen.columns - 2)
    if end - left >= 80:
        split = left + (end - left - 2) // 2
        if label_x < split:
            end = split
        else:
            left = split + 2
    return left, end


def check_card_stacks(terminal, words, columns):
    screen = terminal.screen
    stacks = []
    for column in columns:
        titles = []
        for heading in column:
            x, y = position(terminal, words["settings." + heading])
            left, end = card_bounds(screen, x)
            assert x == left + 2, heading
            titles.append(y)
            if len(titles) > 1:
                # Bottom padding, inter-card gap and top padding.
                assert all(not "".join(row[left:end]).strip() for row in screen.cells[y-3:y]), heading
                assert "".join(screen.cells[y-4][left:end]).strip(), heading + " has trailing blank rows"
        stacks.append(titles)
    if len(stacks) == 2:
        assert stacks[0][0] == stacks[1][0] and stacks[0][1] < stacks[1][1]


def check_switch(terminal, words, key, enabled, profile):
    label_x, y = position(terminal, words[key])
    row = terminal.screen.cells[y]
    _, right = card_bounds(terminal.screen, label_x)
    track_x = right - 8
    assert "".join(row[track_x:right]) == switch_track(enabled) + "  ", key
    before_track = "".join(row[label_x:track_x])
    assert before_track[len(words[key]):].strip(" ›") == "", (key, before_track)
    if profile != "no-color":
        styles = terminal.screen.styles[y]
        rail_x, thumb_x = (track_x, track_x + 4) if enabled else (track_x + 2, track_x)
        rail = styles[rail_x]
        assert rail[0] == rail[1] and rail[1] != styles[right-1][1], (key, "rail", rail)
        assert styles[thumb_x][0] != rail[0], (key, "thumb", styles[thumb_x])
    return track_x, y


def focus(terminal, label, row_label, footer_hint=""):
    pattern = re.compile(r"›\s*(?:[<\[]\s*)?" + re.escape(label))
    for _ in range(36):
        terminal.settle()
        screen = terminal.screen.text()
        if (pattern.search(screen) or "› " + row_label.split()[0] in screen) and label in screen and footer_hint in screen.splitlines()[-1]:
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
        check_card_stacks(terminal, words, (("interface", "serviceGroup", "diagnostics"), ("mihomo", "core")))
        preview.capture(terminal, output, "settings-wide", args)
        # A tall single column lets the same terminal frame expose the entire reading order.
        terminal.resize(80, 80)
        terminal.settle()
        check_card_stacks(terminal, words, (("interface", "mihomo", "serviceGroup", "core", "diagnostics"),))
        preview.capture(terminal, output, "settings-single-column", args)
        terminal.resize(160, 45)
        terminal.settle()

        switch_defaults = {
            "settings.ipv6": True, "settings.unifiedDelay": False,
            "settings.tcpConcurrent": False, "settings.tcpKeepAlive": True, "settings.etagSupport": True,
            "dashboard.interface.keyHints": True, "settings.autostart": True,
        }
        for key in ("dashboard.language.title", "settings.theme", "settings.logLevel", "settings.findProcessMode", *switch_defaults):
            x, y = position(terminal, words[key])
            row = terminal.screen.cells[y]
            left, right = card_bounds(terminal.screen, x)
            assert x == left + 2 and row[right-2:right] == [" ", " "], key
            if key in switch_defaults:
                check_switch(terminal, words, key, switch_defaults[key], profile)
            else:
                assert row[right-5] == "▾", "dropdown lacks its two-cell face and card insets: " + key

        native = {"en": "English", "zh-Hans": "简体中文"}
        current = dropdown_trigger(terminal, native[language])
        terminal.resize(26, 24)
        terminal.settle()
        terminal.wait(current)
        label_x, label_y = position(terminal, words["dashboard.language.title"])
        trigger = position(terminal, current)
        # Two focus cells and two face-padding cells precede the value.
        assert trigger == (label_x + 4, label_y + 1), "wrapped dropdown did not start below its label"
        preview.capture(terminal, output, "settings-wrapped-26x24", args)
        click(terminal, trigger)
        dropdown_option(terminal, native[language], checked=True)
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.key(b"\t")
        terminal.settle()
        wrapped = position(terminal, switch_track(True))
        label = position(terminal, words["dashboard.interface.keyHints"].split()[0])
        assert wrapped[1] > label[1] and wrapped[0] == label[0] + 2, "switch did not wrap at the label inset"
        if profile == "no-color":
            assert terminal.screen.cells[label[1]][label[0]-2:label[0]] == ["›", " "]
            assert terminal.screen.cells[wrapped[1]][label[0]:wrapped[0]] == [" ", " "]
        else:
            assert terminal.screen.cells[wrapped[1]][label[0]:wrapped[0]] == ["›", " "]
        preview.capture(terminal, output, "switch-wrapped-26x24", args)
        click(terminal, wrapped)
        terminal.wait(switch_track(False))
        terminal.key(b"\x14")
        terminal.wait(switch_track(True))
        dismiss_toasts(terminal)
        terminal.resize(160, 45)
        terminal.settle()

        # The current IPv6 thumb, unified-delay rail and keyboard all activate
        # their own switch through the same controls users see in the cards.
        x, y = check_switch(terminal, words, "settings.ipv6", True, profile)
        thumb_before = terminal.screen.styles[y][x + 5]
        rail_before = terminal.screen.styles[y][x]
        terminal.key(("\x1b[<35;%d;%dM" % (x + 6, y + 1)).encode())
        terminal.settle()
        check_switch(terminal, words, "settings.ipv6", True, profile)
        if profile != "no-color":
            assert terminal.screen.styles[y][x + 5] == thumb_before, "hover changed the thumb"
            assert terminal.screen.styles[y][x] == rail_before, "hover changed the rail"
        click(terminal, (x + 5, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.ipv6", False, profile)
        x, y = check_switch(terminal, words, "settings.unifiedDelay", False, profile)
        click(terminal, (x + 2, y))
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", True, profile)
        preview.capture(terminal, output, "mihomo-switches-wide", args)
        terminal.key(b"\r")
        dismiss_toasts(terminal)
        check_switch(terminal, words, "settings.unifiedDelay", False, profile)

        terminal.click_text(dropdown_trigger(terminal, "info"))
        dropdown_option(terminal, "info", checked=True)
        for level in ("debug", "warning", "error", "silent"):
            dropdown_option(terminal, level)
        assert all(value not in terminal.screen.text() for value in ("Follow profile", "跟随配置"))
        preview.capture(terminal, output, "log-level-menu-wide", args)
        terminal.click_text(dropdown_option(terminal, "debug"))
        debug_trigger = dropdown_trigger(terminal, "debug")
        dismiss_toasts(terminal)

        for key in ("settings.tcpConcurrent", "settings.tcpKeepAlive", "settings.etagSupport"):
            point = check_switch(terminal, words, key, switch_defaults[key], profile)
            click(terminal, point)
            dismiss_toasts(terminal)
            check_switch(terminal, words, key, not switch_defaults[key], profile)

        process_trigger = dropdown_trigger(terminal, words["settings.processAsNeeded"])
        terminal.click_text(process_trigger)
        dropdown_option(terminal, words["settings.processAsNeeded"], checked=True)
        dropdown_option(terminal, words["settings.processOff"])
        terminal.click_text(dropdown_option(terminal, words["settings.processAlways"]))
        process_trigger = dropdown_trigger(terminal, words["settings.processAlways"])
        dismiss_toasts(terminal)

        dark_trigger = dropdown_trigger(terminal, "Dark")
        terminal.click_text(dark_trigger)
        _, checked_y = position(terminal, dropdown_option(terminal, "Dark", checked=True))
        assert position(terminal, dropdown_option(terminal, "Light"))[1] == checked_y - 1, "Light must precede Dark"
        assert position(terminal, dropdown_option(terminal, "Auto"))[1] == checked_y + 1, "Auto must follow Dark"
        preview.capture(terminal, output, "theme-menu-wide", args)
        terminal.key(b"\x1b[A")
        terminal.key(b"\x1b")
        terminal.wait_absent("✓")
        terminal.wait(dark_trigger)

        terminal.key(b"\r")  # Canceling the dropdown returns focus to its trigger.
        dropdown_option(terminal, "Dark", checked=True)
        terminal.key(b"\x1b[A")
        terminal.key(b"\r")
        light_trigger = dropdown_trigger(terminal, "Light")
        terminal.wait(words["settings.themeLightPlaceholder"])
        terminal.click_text(light_trigger)
        dropdown_option(terminal, "Light", checked=True)
        mark = len(terminal.raw)
        terminal.click_text(dropdown_option(terminal, "Auto"))
        auto_trigger = dropdown_trigger(terminal, "Auto")
        terminal.wait(words["settings.themeAuto"] % "Dark")
        terminal.settle()
        assert b"\x1b]11;?" in terminal.raw[mark:], "Auto did not query the terminal background"
        terminal.send(b"\x1b]11;rgb:ffff/ffff/ffff\x07")
        terminal.wait(words["settings.themeAuto"] % "Light")
        terminal.click_text(auto_trigger)
        dropdown_option(terminal, "Auto", checked=True)
        terminal.click_text(dropdown_option(terminal, "Dark"))
        terminal.wait(dark_trigger)

        other = "zh-Hans" if language == "en" else "en"
        assert native[other] not in terminal.screen.text(), "closed language dropdown shows both languages"
        assert position(terminal, words["dashboard.interface.keyHints"])[1] == position(terminal, current)[1] + 2, "unexpected text below the language control"
        current_point = position(terminal, current)
        click(terminal, current_point)
        expanded = dropdown_trigger(terminal, native[language], expanded=True)
        assert position(terminal, expanded) == current_point
        dropdown_option(terminal, native[language], checked=True)
        pending_option = dropdown_option(terminal, native[other])
        pending_x, pending_y = position(terminal, pending_option)
        normal_style = terminal.screen.styles[pending_y][pending_x]
        preview.capture(terminal, output, "language-menu-wide", args)
        move = b"\x1b[B" if language == "en" else b"\x1b[A"
        terminal.key(move)
        terminal.settle()
        dropdown_option(terminal, native[language], checked=True)
        dropdown_option(terminal, native[other])
        if profile == "no-color":
            assert "›" in terminal.screen.cells[pending_y][:pending_x], "pending row lacks a visible cursor"
        else:
            assert terminal.screen.styles[pending_y][pending_x][1] != normal_style[1], "pending row did not highlight"
            assert "›" not in terminal.screen.cells[pending_y], "color dropdown shows a redundant cursor"
        preview.capture(terminal, output, "language-menu-pending", args)
        terminal.key(b"\x1b")
        terminal.wait_absent(native[other])
        preview.capture(terminal, output, "language-focused", args)
        terminal.key(b"\r")
        dropdown_option(terminal, native[language], checked=True)
        terminal.key(move)
        terminal.key(b"\r")
        click(terminal, current_point)  # The pending save disables the trigger.
        terminal.wait(labels(other)["settings.interface"])
        switched = dropdown_trigger(terminal, native[other])
        terminal.settle()
        preview.capture(terminal, output, "language-switched", args)
        terminal.click_text(switched)
        dropdown_option(terminal, native[other], checked=True)
        terminal.click_text(dropdown_option(terminal, native[language]))
        terminal.wait(words["settings.interface"])
        terminal.settle()

        # Exercise the row's mouse target and the global recovery shortcut.
        x, y = check_switch(terminal, words, "dashboard.interface.keyHints", True, profile)
        click(terminal, (x + 4, y))
        terminal.settle()
        check_switch(terminal, words, "dashboard.interface.keyHints", False, profile)
        preview.capture(terminal, output, "hints-hidden", args)
        terminal.key(b"\x14")  # Ctrl+T restores the hidden footer.
        terminal.settle()

        # Completed preference Toasts can cover the right card's heading.
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
            title_x, title_y = position(terminal, words["settings.interface"])
            assert title_x == left + 2
            headings = "".join(terminal.screen.cells[title_y][left:right+1])
            assert (words["settings.mihomo"] in headings) == (columns == 2)
            assert not any("┌" in row[left:right+1] or "─" in row[left:right+1]
                           for row in terminal.screen.cells[title_y:-1]), "card decorations in settings body"
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
                focus(terminal, current, words["dashboard.language.title"])
                terminal.key(b"\r")
                dropdown_option(terminal, native[language], checked=True)
                preview.capture(terminal, output, "language-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent(native[other])
                focus(terminal, dark_trigger, words["settings.theme"])
                terminal.key(b"\r")
                dropdown_option(terminal, "Dark", checked=True)
                preview.capture(terminal, output, "theme-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
                focus(terminal, debug_trigger, words["settings.logLevel"])
                terminal.key(b"\r")
                dropdown_option(terminal, "debug", checked=True)
                preview.capture(terminal, output, "log-level-menu-compact", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
                terminal.settle()
                x, y = check_switch(terminal, words, "settings.ipv6", False, profile)
                click(terminal, (x, y))
                dismiss_toasts(terminal)
                check_switch(terminal, words, "settings.ipv6", True, profile)
                check_switch(terminal, words, "settings.unifiedDelay", False, profile)
                preview.capture(terminal, output, "mihomo-switches-40x16", args)
                focus(terminal, process_trigger, words["settings.findProcessMode"])
                terminal.key(b"\r")
                dropdown_option(terminal, words["settings.processAlways"], checked=True)
                preview.capture(terminal, output, "process-menu-40x16", args)
                terminal.key(b"\x1b")
                terminal.wait_absent("✓")
            focus(terminal, words["settings.openLogs"], words["settings.logs"])
            preview.capture(terminal, output, "tab-logs-%dx%d" % (width, height), args)
            focus(terminal, words["settings.edit"], words["settings.ports"], words["navigation.controls.editPorts"])
            terminal.key(b"\r")
            terminal.wait(words["ports.title"])
            terminal.wait("7897")
            preview.capture(terminal, output, "ports-%dx%d" % (width, height), args)
            terminal.key(b"\x1b")
            terminal.wait_absent(words["ports.title"])
            terminal.settle()
            assert words["navigation.controls.editPorts"] in terminal.screen.text().splitlines()[-1], "port return lost its trigger focus"
            for _ in range(8):
                terminal.key(("\x1b[<64;%d;%dM" % (width - 3, height // 2)).encode())
            terminal.key(b"\x1b[H")
            terminal.settle()

        # The diagnostics action only opens the read-only Logs page. The
        # Settings shortcut restores the existing page after its stream stops.
        focus(terminal, words["settings.openLogs"], words["settings.logs"])
        click(terminal, position(terminal, words["settings.openLogs"]))
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
        assert calls["theme_saves"] == 3 and calls["theme"] == "dark", calls
        assert calls["core_changes"] == [["ipv6", "off"], ["unified-delay", "on"],
                                         ["unified-delay", "off"], ["log-level", "debug"],
                                         ["tcp-concurrent", "on"], ["disable-keep-alive", "on"],
                                         ["etag-support", "off"], ["find-process-mode", "always"],
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

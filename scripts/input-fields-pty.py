#!/usr/bin/env python3
"""Exercise unified Bubbles inputs through an actual controlling PTY.

Build: go test -c ./internal/tui -o bin/input-fields-tui.test
The opt-in test fixture uses memory backends and isolated report files. It
does not install a core, access a real service, or change proxy settings.
Optional PNGs redraw actual PTY cells and styles, not a native app window.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
import time


SIZES = ((160, 45), (80, 24), (60, 20), (40, 16))
SCENARIOS = (
    "search-connections", "search-rules", "search-logs", "search-providers",
    "profile-url", "generic-form", "masked-form", "rename-form", "delete-form", "create-name", "create-description", "create-source",
    "ports-mixed", "ports-controller", "wizard-local", "wizard-version", "wizard-prefix",
    "inline-local", "inline-version",
)


def wait_state(terminal, report, predicate, label):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        terminal.read(0.03)
        if report.is_file():
            state = json.loads(report.read_text())
            if predicate(state) and not terminal.screen.synchronized:
                terminal.settle()
                return json.loads(report.read_text())
        if terminal.closed:
            break
    raise AssertionError(label + "\n" + terminal.screen.text())


def state_now(terminal, report):
    terminal.settle()
    return json.loads(report.read_text())


def hashed(value):
    return hashlib.sha256(value.encode()).hexdigest()


def paste(terminal, value):
    terminal.key(b"\x1b[200~" + value.encode() + b"\x1b[201~")


def click(terminal, target):
    x, y = target["X"], target["Y"]
    terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" % (x + 1, y + 1, x + 1, y + 1)).encode())


def hover(terminal, target):
    x, y = target["X"] + target["W"] // 2, target["Y"]
    terminal.key(("\x1b[<35;%d;%dM" % (x + 1, y + 1)).encode())


def target(state, identity):
    return next((item for item in state["targets"] or [] if item["ID"] == identity), None)


def hover_clear(terminal, report, state):
    value_hash, matching = state["value_hash"], state.get("matching")
    clear_id = state["clear_id"]
    region = target(state, clear_id)
    assert region, "nonempty input has no clear symbol target"
    hover(terminal, region)
    state = wait_state(terminal, report, lambda s: s["hover"] == clear_id, "mouse motion did not hover the clear symbol")
    assert state["value_hash"] == value_hash and state.get("matching") == matching, "clear hover changed the query or matching selection"
    y = region["Y"]
    x = next(x for x in range(region["X"], region["X"] + region["W"]) if terminal.screen.cells[y][x] == "x")
    assert not terminal.screen.styles[y][x][2], "clear symbol became bold on hover"
    assert not terminal.screen.styles[y][x][3], "clear symbol gained an underline on hover"
    assert_clear_layout(terminal, state)
    return state


def assert_clear_layout(terminal, state):
    field = state.get("field")
    if not field or field.get("InputWidth", 0) == 0:
        return
    clear = target(state, state["clear_id"])
    y = field["Y"] + 1
    clear_x = field["X"] + 2 + field["InputWidth"] + 1
    right = clear_x + 1
    inline = clear_x + 2 < field["X"] + field["W"] - 2
    if clear:
        assert clear["Y"] == y and clear["X"] == clear_x and clear["W"] == 2, "clear hit region does not follow the editor"
        assert terminal.screen.cells[y][right] == "x", "clear symbol does not follow the input text"
    if inline:
        assert terminal.screen.cells[y][clear_x + 2:clear_x + 4] == ([" ", "│"] if clear else [" ", " "]), "clear separator does not follow the nonempty-input state"
    else:
        assert right == field["X"] + field["W"] - 3 and terminal.screen.cells[y][right + 1] == " ", "normal input clear slot lost its one-cell right inset"
    if state["scenario"] == "search-rules":
        options = [target(state, identity) for identity in ("rules.case", "rules.word", "rules.regex")]
        if not all(options):
            return
        assert options[0]["X"] + options[0]["W"] <= options[1]["X"] and options[1]["X"] + options[1]["W"] <= options[2]["X"], "matching options changed their Aa/ab/.* order"
        if options[0]["Y"] == y:
            assert inline and options[0]["X"] == clear_x + 4, "matching options did not follow the clear symbol and separator"
            assert options[2]["X"] + options[2]["W"] == field["X"] + field["W"] - 2, "inline matching options are not aligned to the frame's right inset"
        else:
            assert options[0]["Y"] == field["Y"] + 3 and options[2]["X"] + options[2]["W"] == field["X"] + field["W"], "wrapped matching controls are not right-aligned below the frame"


def assert_geometry(terminal, state, allow_hidden=False):
    width, height = state["width"], state["height"]
    assert not state["writes"], "editing invoked a backend mutation"
    for item in state["targets"] or []:
        assert 0 <= item["X"] < width and 0 <= item["Y"] < height, item
        assert item["W"] > 0 and item["H"] > 0, item
        assert item["X"] + item["W"] <= width and item["Y"] + item["H"] <= height, item
    field, cursor = state.get("field"), state.get("cursor")
    if not field or field.get("InputWidth", 0) == 0:
        assert cursor is None, "a clipped input exposed its terminal cursor"
        assert allow_hidden, "input is not visible at %sx%s" % (width, height)
        return False
    assert field["H"] == 3, field
    assert cursor, "focused visible input has no native cursor"
    assert cursor["shape"] == 2, cursor  # Bubble Tea's CursorBar.
    assert cursor["y"] == field["Y"] + 1, (field, cursor)
    assert field["X"] + 2 <= cursor["x"] <= field["X"] + 2 + field["InputWidth"], (field, cursor)
    assert 0 <= cursor["x"] < width and 0 <= cursor["y"] < height, cursor
    rows = terminal.screen.cells
    assert rows[field["Y"]][field["X"]] == "╭", "shared rounded frame is absent"
    assert rows[field["Y"] + 2][field["X"]] == "╰", "shared rounded frame bottom is clipped"
    y, x = field["Y"] + 1, field["X"]
    assert terminal.screen.styles[y][x + 2][1] == terminal.screen.styles[y][x - 1][1], "input interior does not match its surrounding surface"
    assert_clear_layout(terminal, state)
    return True


def assert_cursor_cell(terminal, state, expected):
    if not state.get("cursor"):
        return
    cursor = state["cursor"]
    assert terminal.screen.cells[cursor["y"]][cursor["x"]] == expected, (
        "native cursor points to the wrong cell", expected, cursor, terminal.screen.text())
    y, x = cursor["y"], cursor["x"]
    if x > state["field"]["X"] + 2:
        assert terminal.screen.styles[y][x][0] == terminal.screen.styles[y][x - 1][0], "glyph under the native cursor inherited the frame color"


def fixture_terminal(preview, binary, report, language, profile, scenario):
    environment = {"LAZYCLASH_INPUT_FIELDS_PTY_REPORT": str(report),
                   "LAZYCLASH_INPUT_FIELDS_PTY_LANGUAGE": language,
                   "LAZYCLASH_INPUT_FIELDS_PTY_SCENARIO": scenario}
    wrapper = preview.WRAPPER.replace("os.execv(sys.argv[5], sys.argv[5:])",
        "os.environ.update(" + repr(environment) + "); os.execv(sys.argv[5], sys.argv[5:])")
    terminal = preview.SMOKE.Terminal([sys.executable, "-c", wrapper, "160", "45", profile,
        language, str(binary.resolve()), "-test.run=^TestInputFieldsPTYFixture$", "-test.count=1"])
    terminal.screen = preview.StyledScreen(160, 45)
    return terminal


def exercise(preview, args, output, language, profile, scenario):
    output.mkdir(parents=True)
    report = output / "state.json"
    terminal = fixture_terminal(preview, args.binary, report, language, profile, scenario)
    hidden = []
    try:
        state = wait_state(terminal, report, lambda s: s["width"] == 160 and s.get("cursor"), "initial input did not focus")
        assert_geometry(terminal, state)
        if scenario == "search-rules" and profile == "truecolor":
            region = target(state, "rules.word")
            y = region["Y"]
            x = next(x for x in range(region["X"], region["X"] + region["W"]) if "".join(terminal.screen.cells[y][x:]).startswith("ab"))
            assert terminal.screen.styles[y][x][3], "unselected whole-word icon has no underline"
        preview.capture(terminal, output, "initial", args)
        terminal.key(b"\x01\x0b")  # Bubbles Home, then kill to the end.
        value = "a中b文"
        paste(terminal, value)
        state = wait_state(terminal, report, lambda s: s["value_hash"] == hashed(value), "Unicode paste was not retained")
        assert_geometry(terminal, state)
        if scenario != "masked-form":
            assert state["cursor"]["x"] == state["field"]["X"] + 2 + 6, state
        terminal.key(b"\x1b[D")
        state = wait_state(terminal, report, lambda s: s["position"] == 3, "left arrow did not move the editor")
        assert_cursor_cell(terminal, state, "•" if scenario == "masked-form" else "文")
        preview.capture(terminal, output, "unicode-middle", args)

        terminal.key(b"\x01\x0b")
        long_value = "中文abcdefgh" * 24 + "VISIBLE-TAIL"
        if state["char_limit"] > 0:
            long_value = long_value[:state["char_limit"]]
        paste(terminal, long_value)
        state = wait_state(terminal, report, lambda s: s["value_hash"] == hashed(long_value), "long value was lost")
        terminal.key(b"\x1b[D\x1b[D\x1b[D")
        state = wait_state(terminal, report, lambda s: s["position"] == len(long_value) - 3, "middle edit lost its logical cursor")
        expected_cell = "•" if scenario == "masked-form" else long_value[-3]
        assert_cursor_cell(terminal, state, expected_cell)
        for width, height in args.sizes:
            terminal.resize(width, height)
            state = wait_state(terminal, report, lambda s: (s["width"], s["height"]) == (width, height), "resize did not reach the model")
            assert state["value_hash"] == hashed(long_value), "resize changed the input value"
            assert state["position"] == len(long_value) - 3, "resize changed the logical cursor"
            visible = assert_geometry(terminal, state, allow_hidden=width <= 40)
            if visible:
                assert_cursor_cell(terminal, state, expected_cell)
            else:
                hidden.append((width, height))
            preview.capture(terminal, output, "long-%dx%d" % (width, height), args)
        terminal.resize(160, 45)
        state = wait_state(terminal, report, lambda s: s["width"] == 160 and s["height"] == 45 and s.get("cursor"), "wide resize did not restore the cursor")
        assert_geometry(terminal, state)

        if scenario == "search-rules":
            click(terminal, target(state, "rules.case"))
            state = wait_state(terminal, report, lambda s: s["matching"][0], "case option did not toggle")
            click(terminal, target(state, "rules.word"))
            state = wait_state(terminal, report, lambda s: s["matching"][1], "word option did not toggle")
            preview.capture(terminal, output, "matching-selected", args)
            if profile == "truecolor":
                for identity, label in (("rules.case", "Aa"), ("rules.word", "ab")):
                    region = target(state, identity)
                    y = region["Y"]
                    x = next(x for x in range(region["X"], region["X"] + region["W"]) if "".join(terminal.screen.cells[y][x:]).startswith(label))
                    fg, bg, bold, underline = terminal.screen.styles[y][x]
                    assert bold, "selected matching option is not emphasized"
                    field = state["field"]
                    assert bg == terminal.screen.styles[field["Y"] + 1][field["X"] + 2][1], "selected matching option added a fill"
                    if label == "ab":
                        assert underline, "whole-word icon has no underline"
            selection, value_hash = state["matching"], state["value_hash"]
            for identity, label in (("rules.case", "Aa"), ("rules.word", "ab"), ("rules.regex", ".*")):
                hover(terminal, target(state, identity))
                state = wait_state(terminal, report, lambda s: s["hover"] == identity, "mouse motion did not hover " + label)
                assert state["matching"] == selection and state["value_hash"] == value_hash, "hover changed matching selection or input text"
                region = target(state, identity)
                y = region["Y"]
                x = next(x for x in range(region["X"], region["X"] + region["W"]) if "".join(terminal.screen.cells[y][x:]).startswith(label))
                for offset in range(len(label)):
                    underline = terminal.screen.styles[y][x + offset][3]
                    assert underline == (profile == "truecolor" and label == "ab"), "hover changed the icon underline: " + label
                preview.capture(terminal, output, "hover-" + identity.removeprefix("rules."), args)

        state = hover_clear(terminal, report, state)
        preview.capture(terminal, output, "hover-clear", args)
        click(terminal, target(state, state["clear_id"]))
        state = wait_state(terminal, report, lambda s: s["runes"] == 0, "clear symbol did not empty the value")
        assert_geometry(terminal, state)
        assert target(state, state["clear_id"]) is None, "empty input retained an active clear target"
        if scenario == "search-rules":
            assert state["matching"] == [True, True, False], "clear reset matching settings"
        preview.capture(terminal, output, "cleared", args)
        if scenario == "search-rules":
            # Keep one readable sample beside the long-text stress captures.
            paste(terminal, "github")
            state = wait_state(terminal, report, lambda s: s["value_hash"] == hashed("github"), "sample query was not retained")
            for identity, expected in (("rules.word", [True, False, False]),
                                       ("rules.case", [False, False, False]),
                                       ("rules.case", [True, False, False])):
                click(terminal, target(state, identity))
                state = wait_state(terminal, report, lambda s: s["matching"] == expected, "sample matching selection did not update")
            hover(terminal, target(state, "rules.case"))
            state = wait_state(terminal, report, lambda s: s["hover"] == "rules.case", "sample Aa hover did not update")
            preview.capture(terminal, output, "hover-case-clean", args)
            state = hover_clear(terminal, report, state)
            preview.capture(terminal, output, "hover-clear-clean", args)
        elif scenario == "profile-url":
            paste(terminal, "https://example.invalid/clash.yaml")
            state = wait_state(terminal, report, lambda s: s["value_hash"] == hashed("https://example.invalid/clash.yaml"), "sample profile URL was not retained")
            state = hover_clear(terminal, report, state)
            preview.capture(terminal, output, "hover-clear-clean", args)
        if scenario == "masked-form":
            assert value not in terminal.screen.text() and long_value not in terminal.text(), "masked input leaked plain text"
        raw = bytes(terminal.raw)
        assert re.search(rb"\x1b\[[56] q", raw), "PTY output never selected a native bar cursor"
        assert not preview.OSC_SET.search(raw), "input changed the terminal default palette"
        if profile == "truecolor":
            assert preview.RGB.search(raw), "True Color profile emitted no RGB colors"
        else:
            assert not [m.group(1) for m in preview.SGR.finditer(raw) if m.group(1) not in (b"", b"0")], "NO_COLOR emitted styles"
        terminal.exit(b"\x03")
        final = json.loads(report.read_text())
        assert final["writes"] == 0, "editing or clearing mutated the backend"
        return {"scenario": scenario, "language": language, "profile": profile, "status": "passed",
                "sizes": args.sizes, "hidden_sizes": hidden}
    finally:
        preview.capture(terminal, output, "final", args)
        terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="Already built opt-in Go test binary")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--lang", choices=("en", "zh-Hans"), action="append")
    parser.add_argument("--profile", choices=("truecolor", "no-color"), action="append")
    parser.add_argument("--scenario", choices=SCENARIOS, action="append")
    parser.add_argument("--size", action="append", help="Repeat COLSxROWS; defaults to 160x45,80x24,60x20,40x16")
    spec = importlib.util.spec_from_file_location("lazyclash_input_preview", Path(__file__).with_name("onboarding-pty.py"))
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    preview.capture_arguments(parser)
    args = parser.parse_args()
    if not args.binary.is_absolute() or not args.binary.is_file():
        parser.error("--binary must be an absolute path to an existing Go test binary")
    try:
        args.sizes = [tuple(map(int, size.lower().split("x"))) for size in args.size] if args.size else SIZES
        if any(len(size) != 2 or min(size) < 12 for size in args.sizes):
            raise ValueError()
    except ValueError:
        parser.error("--size must be COLSxROWS with dimensions at least 12")
    preview.configure_fonts(parser, args)
    args.output.mkdir(parents=True, exist_ok=True)
    results = []
    for language in args.lang or ("en", "zh-Hans"):
        for profile in args.profile or ("truecolor", "no-color"):
            for scenario in args.scenario or SCENARIOS:
                output = args.output / language / profile / scenario
                result = exercise(preview, args, output, language, profile, scenario)
                results.append(result)
                (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
                print("Input PTY %s/%s/%s: passed" % (language, profile, scenario), flush=True)


if __name__ == "__main__":
    main()

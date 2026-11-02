#!/usr/bin/env python3
"""Verify language switching in a real PTY with an isolated, uninstalled home.

Build lazyclash first and pass the binary path. This only opens settings, help
and the initial installation/network forms; it never starts an installation.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import time


def load_terminal():
    path = Path(__file__).with_name("pty-smoke.py")
    spec = importlib.util.spec_from_file_location("lazyclash_pty", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.Terminal


def click(terminal, label, last=False):
    """Locate text in terminal cells, including wide CJK characters."""
    terminal.wait(label)
    settle(terminal)
    matches = []
    for y, cells in enumerate(terminal.screen.cells):
        for x, cell in enumerate(cells):
            if cell and "".join(cells[x:]).startswith(label):
                matches.append((x, y))
    if matches:
        x, y = matches[-1] if last else matches[0]
        terminal.key(("\x1b[<0;%d;%dM\x1b[<0;%d;%dm" %
                      (x + 1, y + 1, x + 1, y + 1)).encode())
        return
    raise AssertionError("No clickable text: " + label)


def capture(terminal, output, name):
    settle(terminal)
    if output:
        output.mkdir(parents=True, exist_ok=True)
        (output / (name + ".txt")).write_text(terminal.screen.text())


def settle(terminal):
    """Wait for a complete quiet frame, including terminals without sync mode."""
    deadline = time.monotonic() + 3
    quiet = 0
    while time.monotonic() < deadline:
        before = len(terminal.raw)
        terminal.read(0.1)
        if len(terminal.raw) == before and not terminal.screen.synchronized:
            quiet += 1
            if quiet == 2:
                return
        else:
            quiet = 0
    raise AssertionError("Terminal did not finish drawing a frame")


def run(binary, output):
    Terminal = load_terminal()
    with tempfile.TemporaryDirectory(prefix="lazyclash-i18n-pty-") as home:
        base = ["/usr/bin/env", "LAZYCLASH_LANG=", "LC_ALL=C", str(binary),
                "--home", home, "--service-name", "lazyclash-i18n-" + str(os.getpid())]
        terminal = Terminal(base + ["--lang", "en"])
        try:
            terminal.wait("Overview")
            terminal.key(b"7")
            terminal.wait("Language")
            click(terminal, "简体中文")
            terminal.wait("语言已保存")
            terminal.wait("设置")
            capture(terminal, output, "settings-zh-120x40")
            assert json.loads((Path(home) / "preferences.json").read_text())["Language"] == "zh-Hans"
            mark = len(terminal.text())
            terminal.resize(80, 24)
            terminal.wait("语言", after=mark)
            capture(terminal, output, "settings-zh-80x24")
            mark = len(terminal.text())
            terminal.resize(120, 40)
            terminal.wait("设置", after=mark)
            terminal.exit()
        finally:
            terminal.cleanup()

        # An ordinary new invocation must pick up the saved choice.
        terminal = Terminal(base)
        try:
            terminal.wait("概览")
            terminal.key(b"7")
            terminal.wait("语言")
            terminal.key(b"?")
            terminal.wait("帮助 / 设置")
            capture(terminal, output, "help-zh")
            terminal.key(b"\x1b")
            terminal.wait_absent("帮助 / 设置")
            terminal.key(b"1")
            terminal.wait("安装 Mihomo 内核")
            terminal.key(b"n")
            terminal.wait("使用环境代理")
            terminal.wait("GitHub 加速")
            capture(terminal, output, "network-zh")
            terminal.key(b"\x1b")
            terminal.wait_absent("使用环境代理")
            terminal.key(b"7")
            terminal.wait("语言")
            click(terminal, "English")
            terminal.wait("Language saved")
            terminal.wait("Settings")
            capture(terminal, output, "settings-en")
            terminal.exit()
        finally:
            terminal.cleanup()
        assert json.loads((Path(home) / "preferences.json").read_text())["Language"] == "en"
        assert not (Path(home) / "installation").exists(), "smoke test installed a core"
    print("PASS: language switching, persistence, CJK mouse targets, help, network form and resize")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--output", type=Path, help="Optional directory for terminal text captures")
    args = parser.parse_args()
    run(args.binary.resolve(), args.output)


if __name__ == "__main__":
    main()

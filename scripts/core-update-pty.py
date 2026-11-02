#!/usr/bin/env python3
"""Exercise manual version checks and upgrade cancellation through a real PTY.

Requires a Go TUI test binary (go test -c ./internal/tui -o PATH). Its opt-in
fixture uses no network, managed storage, real core executable or system service.
The report counts calls so opening the management UI cannot silently auto-check.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import time


def exercise(smoke, binary, output, language):
    output.mkdir(parents=True, exist_ok=True)
    report = output / "calls.json"
    words = {
        "en": ("Overview", "Loading state", "Check updates", "Review core update",
               "A newer stable version is available", "Confirm upgrade", "Mihomo upgrade failed", "Done"),
        "zh-Hans": ("概览", "正在读取状态", "检查更新", "审阅内核更新",
                    "有新的正式版可升级", "确认升级", "Mihomo 升级失败", "完成"),
    }[language]
    overview, loading, check, review, available, upgrade, failed, done = words
    wrapper = "import os,sys; os.environ['LAZYCLASH_CORE_UPDATE_PTY_REPORT']=sys.argv[1]; os.environ['LAZYCLASH_CORE_UPDATE_PTY_LANGUAGE']=sys.argv[2]; os.execv(sys.argv[3],sys.argv[3:])"
    terminal = smoke.Terminal([sys.executable, "-c", wrapper, str(report), language, str(binary.resolve()), "-test.run=^TestCoreUpdatePTYFixture$", "-test.count=1"])
    try:
        terminal.wait(overview)
        terminal.wait_absent(loading)
        terminal.resize(120, 32)
        terminal.key(b"i")
        terminal.wait(check)
        assert review not in terminal.screen.text()
        (output / "manage.txt").write_text(terminal.screen.text())
        terminal.click_text(check)
        terminal.wait(available)
        terminal.wait(upgrade)
        (output / "check.txt").write_text(terminal.screen.text())
        for width, height in [(80, 24), (60, 20), (40, 16), (120, 32)]:
            terminal.resize(width, height)
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                terminal.read(0.1)
                screen = terminal.screen.text()
                status = "New stable version available" if language == "en" and height < 20 else available
                warning = "running core" if language == "en" else "内核将重启"
                ready = not terminal.screen.synchronized and upgrade in screen and status in screen
                ready = ready and "v1.19.32" in screen and "v1.19.33" in screen
                if ready and (height != 16 or warning in screen):
                    break
            else:
                raise AssertionError("upgrade confirmation became inaccessible after resize\n" + terminal.screen.text())
            (output / ("check-%dx%d.txt" % (width, height))).write_text(terminal.screen.text())
        terminal.key(b"\r")
        terminal.wait("Fixture transfer")
        terminal.key(b"\x1b")
        terminal.wait(failed)
        (output / "cancel-result.txt").write_text(terminal.screen.text())
        terminal.key(b"\r")  # Cancellation focuses the Done button.
        terminal.wait_absent(failed)
        terminal.wait(overview)
        terminal.exit()
        assert json.loads(report.read_text()) == {"checks": 1, "upgrades": 1}, "management/check duplicated a backend call"
        (output / "pty.raw").write_bytes(terminal.raw)
        print("PASS %s: manual check, compact review, explicit upgrade and cancellation cleanup" % language)
    finally:
        terminal.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("pty_smoke", Path(__file__).with_name("pty-smoke.py"))
    smoke = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(smoke)
    for language in ("en", "zh-Hans"):
        exercise(smoke, args.binary, args.output.resolve() / language, language)


if __name__ == "__main__":
    main()

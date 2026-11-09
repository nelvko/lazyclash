#!/usr/bin/env python3
"""Export reproducible Original/Thick/Smooth ANSI and Braille cell previews.

Build `go test -c ./internal/tui -o bin/traffic-chart.test`, then run this
script with --binary bin/traffic-chart.test --output DIRECTORY --png.
Alternative paths exist only in the test binary. PNGs redraw the actual ANSI
cell state with the existing terminal parser; they are not SVG illustrations
or screenshots of a particular native terminal font. The .ansi files can be
printed directly in a real terminal for font-specific comparison.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import signal
import sys
import time


def load_preview():
    path = Path(__file__).with_name("onboarding-pty.py")
    spec = importlib.util.spec_from_file_location("traffic_chart_terminal_preview", path)
    preview = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(preview)
    return preview


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path, help="Already built TUI test binary")
    parser.add_argument("--output", required=True, type=Path)
    preview = load_preview()
    preview.capture_arguments(parser)
    args = parser.parse_args()
    preview.configure_fonts(parser, args)
    binary = args.binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("--binary must identify an executable test binary")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    environment = os.environ.copy()
    environment["LAZYCLASH_TRAFFIC_CHART_EXPERIMENTS"] = str(output)
    result = subprocess.run([str(binary), "-test.run=^TestTrafficChartRenderExperiments$", "-test.count=1"],
                            env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (output / "generation.log").write_text(result.stdout)
    if result.returncode:
        print(result.stdout, file=sys.stderr)
        raise SystemExit(result.returncode)
    entries = json.loads((output / "manifest.json").read_text())
    data_hashes = {}
    for entry in entries:
        previous = data_hashes.setdefault(entry["scenario"], entry["data_sha256"])
        assert previous == entry["data_sha256"], "comparison changed source data"
        raw = (output / (entry["name"] + ".ansi")).read_bytes()
        # Print the exact ANSI through a real controlling PTY before inspecting
        # its cells. LF/CR handling therefore follows actual terminal output.
        writer = ("import fcntl,struct,sys,termios; "
                  "fcntl.ioctl(0,termios.TIOCSWINSZ,struct.pack('HHHH',int(sys.argv[2]),int(sys.argv[1]),0,0)); "
                  "sys.stdout.buffer.write(open(sys.argv[3],'rb').read()); sys.stdout.buffer.flush()")
        terminal = preview.SMOKE.Terminal([sys.executable, "-c", writer,
            str(entry["columns"]), str(entry["rows"]), str(output / (entry["name"] + ".ansi"))])
        terminal.screen = preview.StyledScreen(entry["columns"], entry["rows"])
        try:
            deadline = time.monotonic() + 5
            while not terminal.closed and time.monotonic() < deadline:
                terminal.read(0.05)
            assert terminal.closed, "ANSI comparison PTY did not finish"
            screen = terminal.screen
            assert not screen.synchronized and screen.state == "text", "incomplete ANSI output"
            (output / (entry["name"] + ".pty.raw")).write_bytes(terminal.raw)
            (output / (entry["name"] + ".screen.txt")).write_text(screen.text() + "\n")
            if args.png:
                preview.png(screen, output / (entry["name"] + ".png"), args.font, args.cjk_font, args.font_size)
        finally:
            # The preview writer has no children. Reap that exact PID rather
            # than killing an already-exited process group (EPERM on macOS).
            try:
                if not terminal.closed:
                    os.kill(terminal.pid, signal.SIGKILL)
                os.waitpid(terminal.pid, 0)
            except (ChildProcessError, ProcessLookupError):
                pass
            finally:
                os.close(terminal.fd)
        if entry["profile"] == "no-color":
            assert b"\x1b" not in raw, "no-color comparison retained styling"
        if entry["profile"] == "256":
            assert not preview.RGB.search(raw), "ANSI256 comparison emitted RGB SGR"
    summary = {"comparisons": len(entries), "scenarios": sorted(data_hashes),
               "sizes": sorted({f'{entry["columns"]}x{entry["plot_rows"]}' for entry in entries}),
               "profiles": sorted({entry["profile"] for entry in entries}),
               "all_sources_identical_per_scenario": True,
               "default_renderer": "Original", "alternatives_in_test_binary_only": True,
               "captured_from_controlling_pty": True}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary), flush=True)


if __name__ == "__main__":
    main()

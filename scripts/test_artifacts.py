#!/usr/bin/env python3
"""Keep short test summaries and up to 2 GB of completed failure evidence."""

import argparse
from contextlib import contextmanager, ExitStack
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
import traceback
import uuid


ROOT = Path(__file__).resolve().parents[1]
STORE = ROOT / ".test-artifacts" / "managed"
FAILURE_BUDGET = 2_000_000_000


def write_summary(path, summary):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as stream:
        json.dump(summary, stream, ensure_ascii=False)
        stream.write("\n")
    os.replace(stream.name, path)


@contextmanager
def locked_store(store):
    for directory in (store.parent, store, store / "active", store / "failures", store / "summaries"):
        if directory.is_symlink():
            raise ValueError(f"Refusing a symbolic link in the managed test store: {directory}")
    store.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (store / ".retention.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def owned_runs(directory):
    if directory.is_dir():
        for child in directory.iterdir():
            if (re.fullmatch(r"[\w-]+-[a-f0-9]{32}", child.name)
                    and not child.is_symlink() and (child / ".run-lock").is_file()):
                yield child


def evidence_size(path):
    size = path.lstat().st_blocks * 512
    for root, dirs, files in os.walk(path):
        for name in dirs + files:
            stat = (Path(root) / name).lstat()
            size += max(stat.st_size, stat.st_blocks * 512)
    return size


def remove_run(run):
    # Keep the ownership marker until the data is gone so interrupted deletion
    # can be retried without treating nearby user files as test artifacts.
    for path in run.iterdir():
        if path.name == ".run-lock":
            continue
        if not path.is_symlink() and path.is_dir():
            shutil.rmtree(path)
        else:
            path.unlink()
    (run / ".run-lock").unlink()
    run.rmdir()


def discard_failure(store, failure):
    suite = failure.name.rsplit("-", 1)[0]
    path = store / "summaries" / (suite + ".json")
    if path.is_file():
        summary = json.loads(path.read_text())
        if summary.get("evidence") == str(failure):
            summary["evidence"] = "expired"
            write_summary(path, summary)
    remove_run(failure)


def log_tail(path):
    if path.is_symlink() or not path.is_file():
        return b""
    with path.open("rb") as stream:
        stream.seek(max(0, path.stat().st_size - 8192))
        return stream.read()


def trim_failures(store, budget):
    retained = []
    for failure in owned_runs(store / "failures"):
        size = evidence_size(failure)
        if size > budget and (failure / "evidence").is_dir():
            reason = failure / "failure.txt"
            evidence = failure / "evidence"
            tail = log_tail(evidence / "run.log") or log_tail(evidence / "build.log")
            reason.write_bytes(b"Detailed evidence exceeded the failure budget.\n" + log_tail(reason) + tail)
            shutil.rmtree(evidence)
            size = evidence_size(failure)
        if size > budget:
            discard_failure(store, failure)
        else:
            retained.append((failure.stat().st_mtime_ns, failure, size))
    total = sum(size for _, _, size in retained)
    for _, failure, size in sorted(retained):
        if total <= budget:
            break
        discard_failure(store, failure)
        total -= size


def recover_interrupted(store, budget):
    for run in owned_runs(store / "active"):
        with (run / ".run-lock").open("r") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                continue
            suite = run.name.rsplit("-", 1)[0]
            failure = store / "failures" / run.name
            summary = {"suite": suite, "status": "interrupted", "evidence": str(failure)}
            write_summary(run / "summary.json", summary)
            write_summary(store / "summaries" / (suite + ".json"), summary)
            run.rename(failure)
    # Also finish an interrupted prune after an already-published failure.
    trim_failures(store, budget)


@contextmanager
def managed_run(suite, *, store=STORE, budget=FAILURE_BUDGET, output=None, keep=False):
    if not re.fullmatch(r"[\w-]+", suite):
        raise ValueError("invalid test suite name")
    if output is not None:
        output = Path(output).resolve()
        if output == store or store in output.parents:
            raise ValueError("select an output directory outside the managed test store")
    with locked_store(store):
        for subdir in ("active", "failures"):
            (store / subdir).mkdir(exist_ok=True)
        recover_interrupted(store, budget)
        run = store / "active" / (suite + "-" + uuid.uuid4().hex)
        run.mkdir(mode=0o700)
        lock = (run / ".run-lock").open("x")
        fcntl.flock(lock, fcntl.LOCK_EX)
    started = time.monotonic()
    evidence = run / "evidence"
    summary = {"suite": suite, "status": "passed"}
    try:
        evidence.mkdir()
        yield evidence, lock.fileno()
        if keep:
            shutil.copytree(evidence, output, dirs_exist_ok=True, symlinks=True)
            summary["export"] = str(output)
    except BaseException as error:
        if not isinstance(error, SystemExit) or error.code not in (None, 0):
            summary["status"] = "failed"
            summary["error"] = type(error).__name__
            with (run / "failure.txt").open("w") as stream:
                traceback.print_exception(type(error), error, error.__traceback__, file=stream)
        raise
    finally:
        try:
            summary["seconds"] = round(time.monotonic() - started, 2)
            with locked_store(store):
                if summary["status"] == "failed":
                    write_summary(run / "summary.json", summary)
                    failure = store / "failures" / run.name
                    run.rename(failure)
                    trim_failures(store, budget)
                    if failure.is_dir():
                        summary["evidence"] = str(failure)
                    else:
                        summary["evidence"] = "run exceeded failure storage budget"
                else:
                    remove_run(run)
                destination = store / "summaries" / (suite + ".json")
                write_summary(destination, summary)
                if output is not None:
                    write_summary(output / (suite + "-summary.json"), summary)
                print("Test summary:", destination, file=sys.stderr)
                if "evidence" in summary:
                    print("Failure evidence:", summary["evidence"], file=sys.stderr)
        finally:
            lock.close()


def logged_command(command, log_path, **kwargs):
    with log_path.open("wb") as log, subprocess.Popen(
            command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, **kwargs) as child:
        try:
            while chunk := child.stdout.read1(65536):
                log.write(chunk)
                sys.stdout.buffer.write(chunk)
                sys.stdout.buffer.flush()
            return child.wait()
        except BaseException:
            child.terminate()
            raise


def run_pty(parser, verify, *fixtures):
    parser.add_argument("--output", type=Path, help="Directory for a copy of the run summary")
    parser.add_argument("--keep-artifacts", action="store_true",
                        help="Also export the evidence of a successful run to --output")
    args = parser.parse_args()
    if args.keep_artifacts and args.output is None:
        parser.error("--keep-artifacts requires --output")
    suite = Path(sys.argv[0]).stem
    with managed_run(suite, output=args.output, keep=args.keep_artifacts) as (stage, lock_fd), ExitStack() as temporary:
        args.output = stage
        if hasattr(args, "binary") and args.binary is None:
            directory = temporary.enter_context(tempfile.TemporaryDirectory(prefix="lazyclash-tui-"))
            args.binary = Path(directory) / "tui.test"
            command = [str(ROOT / "scripts/project-go"), "test", "-c", "-o", str(args.binary), "./internal/tui"]
            code = logged_command(command, stage / "build.log", cwd=ROOT, pass_fds=(lock_fd,))
            if code:
                raise subprocess.CalledProcessError(code, command)
        verify(args, parser, *fixtures)


def main():
    if len(sys.argv) < 4 or sys.argv[2] != "--":
        sys.exit("Usage: test_artifacts.py SUITE -- COMMAND [ARGS...]")
    with managed_run(sys.argv[1]) as (stage, lock_fd):
        env = {**os.environ, "LAZYCLASH_TEST_OUTPUT": str(stage)}
        code = logged_command(sys.argv[3:], stage / "run.log", env=env, pass_fds=(lock_fd,))
        if code:
            raise SystemExit(code if code > 0 else 128 - code)


if __name__ == "__main__":
    main()

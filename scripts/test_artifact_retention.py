import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

import test_artifacts as artifacts


class TestArtifactRetention(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.store = self.root / ".test-artifacts" / "managed"

    def summary(self, suite):
        return json.loads((self.store / "summaries" / (suite + ".json")).read_text())

    def test_success_only_publishes_summary_and_keeps_requested_exports(self):
        output = self.root / "release-evidence"
        output.mkdir()
        (output / "notes").write_text("keep me")
        for keep in (False, True):
            with artifacts.managed_run("suite", store=self.store, output=output, keep=keep) as (work, _):
                (work / "screen.ansi").write_bytes(b"fixture")
            self.assertFalse(work.exists())
            self.assertEqual((output / "screen.ansi").exists(), keep)
            self.assertEqual(self.summary("suite")["status"], "passed")
            self.assertEqual((output / "notes").read_text(), "keep me")

    def test_failures_are_bounded_by_size_across_suites(self):
        def fail(suite, size, budget):
            with self.assertRaises(RuntimeError), artifacts.managed_run(suite, store=self.store, budget=budget) as (work, _):
                (work / "log").write_bytes(os.urandom(size))
                raise RuntimeError("fixture")
            return self.summary(suite)

        first = Path(fail("one", 100_000, 1_000_000)["evidence"])
        budget = artifacts.evidence_size(first) + 65_536
        manual = self.store / "failures" / "handwritten"
        manual.mkdir()
        second = Path(fail("two", 100_000, budget)["evidence"])
        self.assertFalse(first.exists())
        self.assertEqual(self.summary("one")["evidence"], "expired")
        self.assertTrue(second.is_dir())
        self.assertEqual((second / "evidence" / "log").stat().st_size, 100_000)
        huge = Path(fail("huge", 1_000_000, budget)["evidence"])
        self.assertIn("exceeded", (huge / "failure.txt").read_text())
        self.assertFalse((huge / "evidence").exists())
        self.assertTrue(second.is_dir())
        self.assertTrue(manual.exists())

    def test_interrupted_run_is_recovered_without_touching_an_active_run(self):
        command = """import os,sys; from pathlib import Path
from test_artifacts import managed_run
with managed_run('interrupted', store=Path(sys.argv[1])) as (stage, _):
    (stage/'partial').write_text('partial')
    os._exit(9)
"""
        with artifacts.managed_run("live", store=self.store) as (active, _):
            result = subprocess.run([sys.executable, "-c", command, str(self.store)],
                                    cwd=Path(artifacts.__file__).parent)
            self.assertEqual(result.returncode, 9)
            with artifacts.managed_run("next", store=self.store):
                self.assertTrue(active.is_dir())
                self.assertEqual(self.summary("interrupted")["status"], "interrupted")
            self.assertEqual(len(list((self.store / "failures").glob("*/evidence/partial"))), 1)

    def test_failure_storage_must_be_a_real_directory(self):
        self.store.mkdir(parents=True)
        other = self.root / "other-project"
        other.mkdir()
        (self.store / "failures").symlink_to(other, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symbolic link"), artifacts.managed_run("test", store=self.store):
            self.fail("run used another project's storage")
        self.assertEqual(list(other.iterdir()), [])

    def test_next_success_completes_an_interrupted_failure_prune(self):
        with self.assertRaises(RuntimeError), artifacts.managed_run("published", store=self.store) as (work, _):
            (work / "log").write_bytes(os.urandom(100_000))
            (work / "run.log").write_text("last compiler diagnostic\n")
            raise RuntimeError("fixture")
        with artifacts.managed_run("next", store=self.store, budget=64_000):
            pass
        failure = Path(self.summary("published")["evidence"])
        self.assertLessEqual(artifacts.evidence_size(failure), 64_000)
        self.assertIn("fixture", (failure / "failure.txt").read_text())
        self.assertIn("last compiler diagnostic", (failure / "failure.txt").read_text())

    def test_cli_retains_combined_output_and_preserves_exit_code(self):
        script = self.root / "scripts" / "test_artifacts.py"
        script.parent.mkdir()
        shutil.copy(artifacts.__file__, script)
        result = subprocess.run([sys.executable, str(script), "command", "--", sys.executable, "-c",
                                 "import sys;print('one');print('two',file=sys.stderr);sys.exit(23)"],
                                capture_output=True)
        self.assertEqual(result.returncode, 23)
        failure = Path(self.summary("command")["evidence"])
        self.assertEqual((failure / "evidence" / "run.log").read_bytes(), result.stdout)
        self.assertIn(b"one", result.stdout)
        self.assertIn(b"two", result.stdout)


if __name__ == "__main__":
    unittest.main()

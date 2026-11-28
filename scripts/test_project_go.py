import os
from pathlib import Path
import subprocess
import tempfile
import unittest


WRAPPER = Path(__file__).with_name("project-go").resolve()


class ProjectGoTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        self.cache = root / "lazyclash/go-build"
        self.record = root / "calls"
        self.env = {
            **os.environ,
            "XDG_CACHE_HOME": str(root),
            "GOCACHE": str(root / "unrelated"),
            "PATH": str(root) + os.pathsep + os.environ["PATH"],
            "RECORD": str(self.record),
            "BLOCKS": "9765625",  # exactly 10 decimal GB in 1 KiB blocks
        }
        for name, body in {
            "du": 'printf "%s\\n" "$BLOCKS"',
            "go": '''
printf '%s %s\\n' "$GOCACHE" "$*" >> "$RECORD"
case "$1" in
    wait) echo ready; read -r _ ;;
    fail) exit 17 ;;
esac
''',
        }.items():
            path = root / name
            path.write_text("#!/bin/sh\n" + body)
            path.chmod(0o755)

    def go(self, command):
        return subprocess.run([WRAPPER, command], env=self.env, capture_output=True, timeout=10)

    def test_cache_is_private_and_pruned_only_over_limit(self):
        self.assertEqual(self.go("build").returncode, 0)
        self.env["BLOCKS"] = "9765626"
        self.assertEqual(self.go("fail").returncode, 17)
        self.assertEqual(self.record.read_text().splitlines(), [
            f"{self.cache} build", f"{self.cache} fail", f"{self.cache} clean -cache",
        ])

    def test_only_last_concurrent_build_trims(self):
        self.env["BLOCKS"] = "9765626"
        with subprocess.Popen([WRAPPER, "wait"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, env=self.env) as active:
            try:
                self.assertEqual(active.stdout.readline(), b"ready\n")
                self.assertEqual(self.go("build").returncode, 0)
                self.assertNotIn("clean", self.record.read_text())
            finally:
                active.communicate(input=b"done\n", timeout=10)
        self.assertEqual(active.returncode, 0)
        self.assertEqual(self.record.read_text().splitlines()[-1], f"{self.cache} clean -cache")

    def test_symlink_to_unrelated_cache_is_refused(self):
        unrelated = Path(self.env["GOCACHE"])
        unrelated.mkdir()
        self.cache.parent.symlink_to(unrelated, target_is_directory=True)
        self.assertNotEqual(self.go("build").returncode, 0)
        self.assertFalse(self.record.exists())
        self.cache.parent.unlink()
        self.cache.parent.mkdir()
        self.cache.symlink_to(unrelated, target_is_directory=True)
        self.assertNotEqual(self.go("build").returncode, 0)
        self.assertFalse(self.record.exists())

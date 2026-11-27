"""Commands targeting an E2E harness's private ordinary-user instance."""
import os
from pathlib import Path
import sys


class UserInstance:
    def __init__(self, binary, home, name):
        self.binary = Path(binary).resolve()
        self.home = Path(home)
        self.name = name
        if not self.home.is_absolute() or not name.startswith("lazyclash-"):
            raise ValueError("expected a dedicated test home and service name")
        if sys.platform.startswith("linux") and os.geteuid() == 0:
            raise ValueError("run the ordinary-user harness without sudo")

    def product(self, *arguments):
        return ["/usr/bin/env", "LAZYCLASH_HOME=" + str(self.home),
                "LAZYCLASH_SERVICE=" + self.name, str(self.binary), *arguments]

    def fixture(self, *arguments):
        binary = self.binary.with_name("lazyclash-fixture")
        if not binary.is_file():
            raise FileNotFoundError(f"Build the E2E fixture beside the tested product with go build -o {binary} ./scripts/fixture")
        return [str(binary), "--home", str(self.home), "--service-name", self.name, *arguments]

    def command(self, *arguments):
        if arguments and arguments[0] in {"autostart", "connections", "logs", "mode", "nodes",
                                          "profile", "rules", "select", "settings", "stats", "tun"}:
            return self.fixture(*arguments)
        return self.product(*arguments)

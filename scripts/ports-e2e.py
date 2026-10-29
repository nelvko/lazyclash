#!/usr/bin/env python3
"""Runtime port checks inside an existing, explicitly isolated Linux E2E run.

Use through scripts/e2e.sh. This helper never creates a VM, chooses a real user
installation, or frees occupied ports by stopping their owner.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
from http.client import HTTPConnection
from urllib.request import ProxyHandler, Request, build_opener


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def without_ports(config):
    return b"".join(line for line in config.splitlines(keepends=True)
                    if not line.startswith((b"mixed-port:", b"external-controller:")))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--home", type=Path, required=True)
    parser.add_argument("--service-name", required=True)
    parser.add_argument("--unit", type=Path, required=True)
    parser.add_argument("--fixture-port", type=int, required=True)
    parser.add_argument("--pty-script", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not args.home.is_absolute() or not (args.home.parent / ".resources-started").is_file():
        parser.error("requires the temporary home and resource marker created by e2e.sh")
    if not re.fullmatch(r"lazyclash-e2e-[a-f0-9]{12}", args.service_name):
        parser.error("requires e2e.sh's randomized dedicated service namespace")
    if args.unit.is_symlink() or str(args.home / "installation" / "mihomo") not in args.unit.read_text():
        parser.error("dedicated test service ownership could not be established")
    command = [str(args.binary), "--home", str(args.home), "--service-name", args.service_name]

    def cli(*arguments, success=True):
        result = subprocess.run(command + list(arguments), stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=60)
        if (result.returncode == 0) != success:
            raise AssertionError("Dedicated port-test command returned unexpected status: " + " ".join(arguments[:2]))
        return result.stdout

    def settings():
        return json.loads(cli("settings", "show"))

    def status():
        return json.loads(cli("status"))

    def manifest():
        return json.loads((args.home / "installation" / "current" / "manifest.json").read_text())

    def api(path):
        stored = manifest()
        request = Request("http://" + stored["controller"] + path,
                          headers={"Authorization": "Bearer " + stored["secret"]})
        # Never inherit a test runner's download proxy for loopback API reads.
        with build_opener(ProxyHandler({})).open(request, timeout=5) as response:
            return json.load(response)

    def proxy_request(port):
        connection = HTTPConnection("127.0.0.1", port, timeout=5)
        try:
            connection.request("GET", "http://127.0.0.1:%d/port-check" % args.fixture_port)
            response = connection.getresponse()
            if response.status != 200:
                current = status()
                selections = [(group["name"], group["now"]) for group in current["Groups"]]
                raise AssertionError("Dedicated loopback proxy request failed: HTTP=%s mode=%s selections=%s" %
                                     (response.status, current["Mode"]["actual"], selections))
            response.read()
        finally:
            connection.close()

    def free_after_move(port):
        for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
            with socket.socket(socket.AF_INET, kind) as sock:
                if kind == socket.SOCK_STREAM:
                    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                sock.bind(("127.0.0.1", port))

    selected = []
    held = []
    record = args.home.parent / "runtime-ports.json"

    def allocate(keep=None):
        # Hold TCP and UDP until a complete choice has been made, and remember
        # every selected port before an application operation can bind it.
        for _ in range(100):
            tcp = socket.socket()
            tcp.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            tcp.bind(("127.0.0.1", 0))
            port = tcp.getsockname()[1]
            udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            try:
                udp.bind(("127.0.0.1", port))
            except OSError:
                tcp.close()
                udp.close()
                continue
            if port in selected or port in (7897, 9097, args.fixture_port):
                tcp.close()
                udp.close()
                continue
            selected.append(port)
            record.write_text(json.dumps(selected))
            if keep == "tcp":
                tcp.listen(1)
                udp.close()
                held.append(tcp)
            elif keep == "udp":
                tcp.close()
                held.append(udp)
            else:
                tcp.close()
                udp.close()
            return port
        raise AssertionError("Could not allocate a dedicated unprivileged test port")

    def change(mixed, controller, *extra, success=True):
        return cli("settings", "ports", "--mixed-port", str(mixed),
                   "--controller-port", str(controller), *extra, success=success)

    initial = settings()
    if initial != {"mixed_port": 7897, "controller_port": 9097}:
        raise AssertionError("Unexpected baseline settings in the dedicated test home")
    baseline_manifest = manifest()
    config_path = args.home / "installation" / "current" / "config.yaml"
    baseline_config = config_path.read_bytes()
    baseline_other = without_ports(baseline_config)
    profile = args.home / "configuration" / "library.json"
    saved_profile = profile.read_bytes()
    saved_overrides = json.loads(saved_profile)["profiles"]["fixture"]["spec"].get("overrides", "")
    override_file = args.home.parent / "ports-overrides.yaml"
    unit_hash = digest(args.unit)
    pointer = os.readlink(args.home / "installation" / "current")
    pid = status()["Service"]["PID"]

    def invariant(expected_profile):
        current = manifest()
        if current["secret"] != baseline_manifest["secret"] or current["id"] != baseline_manifest["id"]:
            raise AssertionError("Port changes replaced the managed credential or installation identity")
        if digest(args.unit) != unit_hash or profile.read_bytes() != expected_profile:
            raise AssertionError("Port changes modified the unit or saved profile")
        if without_ports(config_path.read_bytes()) != baseline_other:
            raise AssertionError("Port changes regenerated or changed unrelated effective configuration")

    try:
        mixed_a, control_a, mixed_b, control_b = (allocate() for _ in range(4))
        tcp_conflict, udp_conflict = allocate("tcp"), allocate("udp")
        for port in (tcp_conflict, udp_conflict):
            change(port, control_a, "--dry-run", success=False)
            if os.readlink(args.home / "installation" / "current") != pointer or status()["Service"]["PID"] != pid:
                raise AssertionError("Preflight conflict mutated installation or stopped the running core")
            if config_path.read_bytes() != baseline_config:
                raise AssertionError("Preflight conflict changed the active configuration")
        change(mixed_a, tcp_conflict, "--dry-run", success=False)
        if os.readlink(args.home / "installation" / "current") != pointer or status()["Service"]["PID"] != pid:
            raise AssertionError("Controller TCP conflict changed the running core")
        print("TCP/UDP conflict preflights preserved active process and configuration")

        change(7897, 9097)  # No-op requires no restart acknowledgement.
        if os.readlink(args.home / "installation" / "current") != pointer or status()["Service"]["PID"] != pid:
            raise AssertionError("An unchanged port request restarted or replaced the installation")
        change(mixed_a, control_a, "--dry-run")
        change(mixed_a, control_a, success=False)  # Running service requires explicit restart consent.
        if os.readlink(args.home / "installation" / "current") != pointer or status()["Service"]["PID"] != pid:
            raise AssertionError("Dry run or missing restart acknowledgment changed the core")

        # A saved profile may differ from the currently applied generation.
        # Runtime settings must preserve the running generation's other keys.
        override_file.write_text("prepend-rules: ['DOMAIN,pending-config.invalid,REJECT']\n")
        override_file.chmod(0o600)
        cli("profile", "override", "fixture", str(override_file))
        edited_profile = profile.read_bytes()
        if edited_profile == saved_profile or not status()["ProfilePending"]:
            raise AssertionError("Fixture cannot exercise unapplied profile-override preservation")
        change(mixed_a, control_a, "--restart")
        if settings() != {"mixed_port": mixed_a, "controller_port": control_a} or not status()["Service"]["Active"]:
            raise AssertionError("Running port move did not commit and restart successfully")
        if status()["Service"]["PID"] == pid:
            raise AssertionError("Running port move did not replace the old process")
        invariant(edited_profile)
        if not api("/version").get("version"):
            raise AssertionError("New authenticated controller was not reachable")
        proxy_request(mixed_a)
        free_after_move(7897)
        free_after_move(9097)
        exports = cli("env").decode()
        if str(mixed_a) not in exports or "7897" in exports:
            raise AssertionError("Shell exports did not use the committed proxy port")
        print("Running CLI port move preserved effective YAML, identity, secret, unit and saved-profile edits")

        change(control_a, mixed_a, "--restart")  # Both desired TCP endpoints belonged to this core.
        invariant(edited_profile)
        api("/version")
        proxy_request(control_a)
        change(mixed_a, control_a, "--restart")
        invariant(edited_profile)
        print("Running core can safely swap its own proxy/controller endpoints")

        subprocess.run(["python3", str(args.pty_script), "--binary", str(args.binary),
                        "--home", str(args.home), "--service-name", args.service_name,
                        "--mode", "runtime-settings", "--mixed-port", str(mixed_b),
                        "--controller-port", str(control_b), "--fixture-port", str(args.fixture_port),
                        "--conflict-port", str(tcp_conflict), "--output", str(args.output)], check=True)
        if settings() != {"mixed_port": mixed_b, "controller_port": control_b} or not status()["Service"]["Active"]:
            raise AssertionError("Real TUI port move did not commit or preserve active service state")
        invariant(edited_profile)
        api("/version")
        proxy_request(mixed_b)
        free_after_move(mixed_a)
        free_after_move(control_a)

        cli("service", "stop")
        change(mixed_a, control_a)
        if status()["Service"]["Active"]:
            raise AssertionError("Changing stopped-core settings started the service")
        invariant(edited_profile)
        free_after_move(mixed_b)
        free_after_move(control_b)
        override_file.write_text(saved_overrides)
        cli("profile", "override", "fixture", str(override_file))
        cli("profile", "apply", "fixture")
        if settings() != {"mixed_port": mixed_a, "controller_port": control_a} or status()["Service"]["Active"]:
            raise AssertionError("Profile application discarded the new ports or started the stopped core")
        invariant(saved_profile)
        cli("service", "start")
        api("/version")
        proxy_request(mixed_a)
        change(7897, 9097, "--restart")
        if settings() != initial or not status()["Service"]["Active"]:
            raise AssertionError("Test could not restore its original dedicated ports")
        invariant(saved_profile)
        for port in (mixed_a, control_a, mixed_b, control_b):
            free_after_move(port)
        print("Stopped settings stayed stopped; profile application kept settings; baseline restored")
    finally:
        for sock in held:
            sock.close()


if __name__ == "__main__":
    main()

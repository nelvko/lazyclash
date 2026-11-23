#!/usr/bin/env python3
"""Root/system, user/user and host TUN checks in a dedicated Linux test VM.

Called by service-scopes-e2e.sh with a packaged executable. No external service
or network endpoint is needed: controlled HTTP CONNECT and DNS fixtures prove
that host traffic traversed the TUN and its DNS interception path.
"""
import argparse
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import socket
import socketserver
import struct
import subprocess
import threading
import time
import uuid


def run(command, success=True, timeout=90, env=None):
    result = subprocess.run([str(x) for x in command], stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout, env=env)
    if success is not None and (result.returncode == 0) != success:
        # Commands print redacted application diagnostics; never print status or
        # profile output, which is retained only in process memory.
        raise AssertionError("Unexpected command result: %s\n%s" %
                             (" ".join(str(x) for x in command[:5]),
                              result.stderr.decode(errors="replace")[-2500:]))
    return result


def wait(predicate, timeout=20):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(0.1)
    raise AssertionError("Timed out waiting for dedicated runtime condition")


def available_port():
    with socket.socket() as tcp, socket.socket(type=socket.SOCK_DGRAM) as udp:
        tcp.bind(("127.0.0.1", 0))
        port = tcp.getsockname()[1]
        udp.bind(("127.0.0.1", port))
        return port


class HTTPFixture(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(5)
        request = self.rfile.readline()
        while self.rfile.readline() not in (b"\r\n", b"\n", b""):
            pass
        if request.startswith(b"CONNECT "):
            self.server.connects.append(request.split()[1].decode())
            self.wfile.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            self.wfile.flush()
            request = self.rfile.readline()
            while self.rfile.readline() not in (b"\r\n", b"\n", b""):
                pass
        body = b"lazyclash-scope-fixture\n"
        self.wfile.write(b"HTTP/1.1 200 OK\r\nContent-Length: " +
                         str(len(body)).encode() + b"\r\nConnection: close\r\n\r\n" + body)


class DNSFixture(socketserver.DatagramRequestHandler):
    def handle(self):
        query = self.rfile.read()
        if len(query) < 12:
            return
        offset = 12
        while query[offset]:
            offset += query[offset] + 1
        question = query[12:offset + 5]
        self.server.queries += 1
        response = query[:2] + b"\x81\x80\x00\x01\x00\x01\x00\x00\x00\x00" + question
        response += b"\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x3c\x00\x04" + socket.inet_aton("203.0.113.77")
        self.wfile.write(response)


class OrbStackServiceDefaults:
    """Temporarily remove OrbStack's global weakening of unit sandbox settings.

    The production ownership check intentionally rejects this drop-in. Existing
    services are never restarted; only the test units are started while absent.
    """
    def __init__(self, token):
        self.path = Path("/run/systemd/system/service.d/zzz-lxc-service.conf")
        self.saved = Path("/run/lazyclash-scopes-env-" + token)
        self.digest = None
        self.generator = Path("/etc/systemd/system-generators/lxc")
        self.generator_digest = None
        self.moved = False

    def suspend(self):
        present = run(["sudo", "-n", "test", "-e", self.path], success=None)
        if present.returncode != 0:
            return
        content = run(["sudo", "-n", "cat", self.path]).stdout
        expected = (b"[Service]\nProcSubset=all\nProtectProc=default\nProtectControlGroups=no\n"
                    b"ProtectKernelTunables=no\nNoNewPrivileges=no\nLoadCredential=\n"
                    b"ProtectHome=no\nProtectSystem=no\nPrivateDevices=no\nPrivateTmp=no\n"
                    b"ProtectKernelLogs=no\nProtectKernelModules=no\nReadWritePaths=\nReadOnlyPaths=\n")
        assert content == expected, "Unknown global service drop-in; test environment was not changed"
        self.digest = hashlib.sha256(content).hexdigest()
        generator = run(["sudo", "-n", "cat", self.generator]).stdout
        assert b"fix_systemd_override_unit" in generator and b"zzz-lxc-service.conf" in generator
        self.generator_digest = hashlib.sha256(generator).hexdigest()
        code = """import os,shutil,stat,sys
p,d,g=sys.argv[1:]
for path in (p,g):
 s=os.lstat(path)
 assert stat.S_ISREG(s.st_mode) and s.st_uid==0 and not s.st_mode&0o022
os.mkdir(d,0o700)
shutil.copy2(g,d+'/lxc-generator')
try:
 os.unlink(g)
 os.rename(p,d+'/saved.conf')
except BaseException:
 if not os.path.lexists(g): shutil.copy2(d+'/lxc-generator',g)
 os.unlink(d+'/lxc-generator')
 os.rmdir(d)
 raise
"""
        run(["sudo", "-n", "python3", "-c", code, self.path, self.saved, self.generator])
        self.moved = True
        run(["sudo", "-n", "systemctl", "daemon-reload"])
        run(["sudo", "-n", "test", "-e", self.path], success=False)
        print("Dedicated VM: saved OrbStack global service drop-in and its generator for strict unit verification", flush=True)

    def restore(self):
        if not self.moved:
            return
        content = run(["sudo", "-n", "cat", self.saved / "saved.conf"]).stdout
        assert hashlib.sha256(content).hexdigest() == self.digest, "Saved VM drop-in changed; refusing to overwrite"
        generator = run(["sudo", "-n", "cat", self.saved / "lxc-generator"]).stdout
        assert hashlib.sha256(generator).hexdigest() == self.generator_digest, "Saved LXC generator changed"
        code = """import os,shutil,sys
p,d,g=sys.argv[1:]
assert not os.path.lexists(g)
if os.path.lexists(p):
 assert open(p,'rb').read()==open(d+'/saved.conf','rb').read()
 os.unlink(p)
os.rename(d+'/saved.conf',p)
shutil.copy2(d+'/lxc-generator',g)
os.unlink(d+'/lxc-generator')
os.rmdir(d)
"""
        run(["sudo", "-n", "python3", "-c", code, self.path, self.saved, self.generator])
        run(["sudo", "-n", "systemctl", "daemon-reload"])
        content = run(["sudo", "-n", "cat", self.path]).stdout
        assert hashlib.sha256(content).hexdigest() == self.digest, "Restored VM drop-in digest differs"
        generator = run(["sudo", "-n", "cat", self.generator]).stdout
        assert hashlib.sha256(generator).hexdigest() == self.generator_digest, "Restored LXC generator digest differs"
        self.moved = False
        print("OrbStack global service drop-in and generator restored with original digests; existing services were not restarted", flush=True)


class Instance:
    def __init__(self, work, name, root=False, home=None):
        self.root = root
        self.name = name
        self.home = Path(home) if home else Path("/var/lib/lazyclash") / name
        self.command = (["sudo", "-n"] if root else []) + [str(work / "lazyclash"), "--lang", "en", "--service-name", name]
        if home:
            self.command += ["--home", str(home)]
        self.owned = False
        self.account = "lc-" + hashlib.sha256(name.encode()).hexdigest()[:12] if root else None
        self.unit = (Path("/etc/systemd/system") if root else Path.home() / ".config/systemd/user") / (name + ".service")

    def cli(self, *args, success=True):
        return run(self.command + list(args), success=success)

    def status(self):
        return json.loads(self.cli("status").stdout)

    def ctl(self, *args):
        return run((["sudo", "-n"] if self.root else []) + ["systemctl"] + ([] if self.root else ["--user"]) + list(args))

    def install(self, binary):
        assert self.ctl("show", self.name, "--property=LoadState", "--value").stdout.strip() == b"not-found"
        assert run(["sudo", "-n", "test", "-e", self.unit], success=False).returncode != 0
        run(["sudo", "-n", "test", "-e", self.home], success=False)
        if self.root:
            run(["getent", "passwd", self.account], success=False)
        self.owned = True
        self.cli("install", "--bin", binary)

    def ports(self, mixed=None, controller=None):
        mixed, controller = mixed or available_port(), controller or available_port()
        self.cli("settings", "ports", "--mixed-port", str(mixed), "--controller-port", str(controller))
        return mixed, controller

    def process(self):
        pid = int(self.ctl("show", self.name, "--property=MainPID", "--value").stdout)
        raw = run(["sudo", "-n", "cat", "/proc/%d/status" % pid]).stdout.decode()
        values = dict(line.split(":", 1) for line in raw.splitlines() if ":" in line)
        return pid, int(values["Uid"].split()[0]), int(values["CapEff"].strip(), 16)

    def cleanup(self):
        if not self.owned:
            return
        present = run(["sudo", "-n", "test", "-f", self.unit], success=None).returncode == 0
        if present:
            unit = run(["sudo", "-n", "cat", self.unit]).stdout.decode()
            assert str(self.home / "installation/mihomo") in unit, "Dedicated unit ownership changed"
            self.ctl("stop", self.name)
            self.ctl("disable", self.name)
            self.cli("uninstall")
        assert self.ctl("show", self.name, "--property=LoadState", "--value").stdout.strip() == b"not-found"
        if self.root:
            run(["sudo", "-n", "rm", "-rf", "--", self.home])
            account = run(["getent", "passwd", self.account], success=None)
            if account.returncode == 0:
                run(["sudo", "-n", "/usr/sbin/userdel", self.account])
        else:
            run(["rm", "-rf", "--", self.home])
        self.owned = False


def assert_tun_released(instance):
    digest = hashlib.sha256(instance.name.encode()).digest()
    device = "lc" + digest[:6].hex()
    table = 100000 + int.from_bytes(digest[:4], "big") % 1000000
    first = 10000 + (int.from_bytes(digest[4:8], "big") % 1000) * 11
    links = json.loads(run(["/usr/sbin/ip", "-j", "link", "show"]).stdout)
    assert not any(link["ifname"] == device for link in links), "Owned TUN interface survived stop"
    for family in ("-4", "-6"):
        rules = json.loads(run(["/usr/sbin/ip", "-j", family, "rule", "show"]).stdout)
        assert not any(first <= rule.get("priority", -1) <= first + 10 for rule in rules), "Owned TUN rule survived stop"
        routes = json.loads(run(["/usr/sbin/ip", "-j", family, "route", "show", "table", "all"]).stdout)
        assert not any(str(route.get("table", "")) == str(table) for route in routes), "Owned TUN route survived stop"
    claim_paths = run(["sudo", "-n", "find", "/run/lazyclash", "-maxdepth", "1", "-name", "netns-*.json", "-type", "f"]).stdout.decode().splitlines()
    for path in claim_paths:
        claim = json.loads(run(["sudo", "-n", "cat", path]).stdout)
        assert claim.get("Service") != instance.name, "Owned TUN claim survived stop"


def replace_private_bytes(path, content):
    code = "import sys; p=sys.argv[1]; f=open(p,'wb'); f.write(sys.stdin.buffer.read()); f.close()"
    result = subprocess.run(["sudo", "-n", "python3", "-c", code, str(path)], input=content,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
    assert result.returncode == 0, "Could not restore private fixture configuration"


def root_tun_ui_smoke(root, terminal_class, work):
    for columns, rows in ((120, 36), (80, 24)):
        terminal = terminal_class(root.command)
        try:
            terminal.wait("Overview")
            terminal.wait_absent("Loading state")
            terminal.resize(columns, rows)
            terminal.settle()
            terminal.wait("Overview")
            terminal.key(b"7")
            terminal.wait("Settings")
            def open_confirmation():
                for _ in range(18):
                    terminal.settle()
                    if "Enable host TUN" in terminal.screen.text():
                        break
                    # Scroll only the settings content, not the sidebar.
                    terminal.key(b"\x1b[<65;60;12M")
                terminal.click_text("Enable host TUN")
                terminal.wait("Enable TUN for this host?")
                terminal.wait("other users and services")
            open_confirmation()
            (work / ("pty-root-tun-%dx%d.txt" % (columns, rows))).write_text(terminal.screen.text())
            terminal.key(b"\r")
            terminal.wait_absent("Enable TUN for this host?")
            assert not root.status()["TUN"], "Default Enter enabled TUN instead of cancelling"
            assert_tun_released(root)
            print("Root TUI host-scope confirmation %dx%d: readable, defaults Cancel, no network changes" % (columns, rows), flush=True)
            if columns == 120:
                open_confirmation()
                terminal.key(b"\t\r")
                wait(lambda: root.status()["TUNActual"])
                assert root.status()["TUN"]
                print("Root TUI explicit confirmation enabled actual TUN", flush=True)
            terminal.exit()
        finally:
            terminal.cleanup()
            if root.status()["TUN"]:
                root.cli("tun", "off")
                assert_tun_released(root)
                print("CLI disabled TUN after PTY confirmation and released all owned network resources", flush=True)


def main():
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt("Scope test interrupted; restoring dedicated resources")
    signal.signal(signal.SIGTERM, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--proxy-only", action="store_true")
    parser.add_argument("--ui-tun-only", action="store_true")
    args = parser.parse_args()
    work = args.work
    assert os.geteuid() != 0 and work.is_absolute() and re.fullmatch(r"lazyclash-scopes\.[A-Za-z0-9]+", work.name)
    run(["sudo", "-n", "true"])
    token = uuid.uuid4().hex[:12]
    user = Instance(work, "lazyclash-scopes-" + token + "-user", home=work / "user-home")
    root = Instance(work, "lazyclash-scopes-" + token + "-root", root=True)
    other = Instance(work, "lazyclash-scopes-" + token + "-other", root=True)
    environment = OrbStackServiceDefaults(token)
    foreign_tun = "lcf" + token[:10]
    foreign_owned = False
    fixture = socketserver.ThreadingTCPServer(("0.0.0.0", 0), HTTPFixture)
    fixture.daemon_threads = True
    fixture.connects = []
    dns = socketserver.ThreadingUDPServer(("0.0.0.0", 0), DNSFixture)
    dns.daemon_threads = True
    dns.queries = 0
    for server in (fixture, dns):
        threading.Thread(target=server.serve_forever, daemon=True).start()
    address = json.loads(run(["/usr/sbin/ip", "-j", "-4", "addr", "show", "dev", "eth0"]).stdout)[0]["addr_info"][0]["local"]
    failure = None
    try:
        environment.suspend()
        links = json.loads(run(["/usr/sbin/ip", "-j", "-d", "link", "show"]).stdout)
        assert not any(link.get("linkinfo", {}).get("info_kind") == "tun" for link in links), "Dedicated VM already has a TUN owner; no existing service was stopped"
        print("Packaged executable: root/system and ordinary-user/user installation", flush=True)
        (work / "resources-started").touch()
        for instance in (user, root, other):
            instance.install(work / "mihomo")
        user_ports = user.ports()
        root_ports = root.ports()
        other.ports()
        user.cli("service", "start")
        assert user.status()["Capabilities"]["scope"] == "user"
        assert root.status()["Capabilities"]["scope"] == "system"
        assert not user.status()["Capabilities"]["tun"]["supported"]
        user.cli("tun", "on", "--scope", "host", "--confirm-host", success=False)
        # Occupied ports must be rejected without stopping the existing owner.
        root.cli("settings", "ports", "--mixed-port", str(user_ports[0]), success=False)
        assert user.status()["Service"]["Active"]
        root.cli("service", "start")
        _, uid, caps = root.process()
        assert uid != 0 and caps == 0, (uid, caps)
        _, uid, caps = user.process()
        assert uid == os.getuid() and caps == 0, (uid, caps)
        for port in (user_ports[0], root_ports[0]):
            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
            connection.request("GET", "http://%s:%d/ordinary" % (address, fixture.server_address[1]))
            assert connection.getresponse().read() == b"lazyclash-scope-fixture\n"
            connection.close()
        print("Concurrent proxy instances, occupied-port refusal and non-root core identities verified", flush=True)
        # Explicit root service selection plus inherited user environment must use
        # root's default data tree, not the invoking user's installation.
        env = dict(os.environ, LAZYCLASH_HOME=str(user.home), LAZYCLASH_SERVICE=user.name,
                   XDG_DATA_HOME=str(work / "inherited-xdg"))
        result = run(["sudo", "-n", "env", "LAZYCLASH_HOME=" + str(user.home),
                      "LAZYCLASH_SERVICE=" + user.name, "XDG_DATA_HOME=" + str(work / "inherited-xdg"),
                      work / "lazyclash", "--service-name", root.name, "status"], env=env)
        assert json.loads(result.stdout)["Controller"] == root.status()["Controller"]
        print("sudo selects the root system instance without copying or adopting user state", flush=True)
        # Root TUI must be reachable with the same packaged executable.
        spec = importlib.util.spec_from_file_location("scope_pty", work / "pty-smoke.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        terminal = module.Terminal(root.command)
        try:
            terminal.wait("Overview")
            terminal.resize(120, 36)
            terminal.settle()
            terminal.wait("Overview")
            terminal.wait("Scope: system")
            assert "Run lazyclash as your normal user" not in terminal.text()
            terminal.exit()
        finally:
            terminal.cleanup()
        print("Root packaged TUI opens successfully (120x36)", flush=True)
        if args.proxy_only:
            print("All packaged proxy-scope scenarios passed", flush=True)
            return
        if args.ui_tun_only:
            root_tun_ui_smoke(root, module.Terminal, work)
            print("All packaged root TUI TUN scenarios passed", flush=True)
            return
        root.cli("tun", "on", success=False)
        run(["sudo", "-n", "/usr/sbin/ip", "tuntap", "add", "dev", foreign_tun, "mode", "tun"])
        foreign_owned = True
        conflict = root.cli("tun", "on", "--scope", "host", "--confirm-host", success=False)
        assert foreign_tun.encode() in conflict.stderr
        run(["sudo", "-n", "/usr/sbin/ip", "link", "delete", "dev", foreign_tun])
        foreign_owned = False
        profile = work / "fixture.yaml"
        profile.write_text("dns:\n  enable: true\n  enhanced-mode: redir-host\n  nameserver: [udp://%s:%d]\nproxies:\n  - name: fixture\n    type: http\n    server: %s\n    port: %d\nproxy-groups:\n  - name: PROXY\n    type: select\n    proxies: [fixture]\nrules: [\"MATCH,PROXY\"]\n" %
                           (address, dns.server_address[1], address, fixture.server_address[1]))
        root.cli("profile", "add", profile, "--name", "scope-fixture")
        profiles = json.loads(root.cli("profile", "list").stdout)
        profile_id = next(p["id"] for p in profiles if p["name"] == "scope-fixture")
        root.cli("profile", "apply", profile_id)
        print("Starting explicitly confirmed host TUN in dedicated test VM", flush=True)
        root.cli("tun", "on", "--scope", "host", "--confirm-host")
        status = root.status()
        assert status["TUN"] and status["TUNActual"] and status["Service"]["Active"], {key:status[key] for key in ("TUN","TUNActual","TUNError","Service","ServiceError","APIError")}
        pid, uid, caps = root.process()
        assert uid != 0 and caps == (1 << 12) | (1 << 13), (uid, caps)
        conflict = other.cli("tun", "on", "--scope", "host", "--confirm-host", success=False)
        assert root.name.encode() in conflict.stderr
        connection = http.client.HTTPConnection("203.0.113.77", 80, timeout=10)
        connection.request("GET", "/through-tun")
        assert connection.getresponse().read() == b"lazyclash-scope-fixture\n"
        connection.close()
        assert "203.0.113.77:80" in fixture.connects
        question = b"\x09scope-tun\x07invalid\x00\x00\x01\x00\x01"
        packet = b"\x12\x34\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00" + question
        with socket.socket(type=socket.SOCK_DGRAM) as client:
            client.settimeout(10)
            client.sendto(packet, ("203.0.113.53", 53))
            response, _ = client.recvfrom(4096)
        assert response[:2] == packet[:2] and socket.inet_aton("203.0.113.77") in response
        assert dns.queries > 0
        print("Host TUN HTTP traffic and DNS hijack traverse controlled local fixtures; competing root instance refused", flush=True)
        # A changed profile must retain TUN and its owned capabilities.
        root.cli("profile", "apply", profile_id)
        reapplied = root.status()
        assert reapplied["TUNActual"], {key:reapplied[key] for key in ("TUN","TUNActual","TUNError","Service","ServiceError","APIError","Mode")}
        pid, _, _ = root.process()
        # A killed core must run cleanup before systemd restarts the same unit.
        run(["sudo", "-n", "kill", "-KILL", str(pid)])
        replacement = wait(lambda: int(root.ctl("show", root.name, "--property=MainPID", "--value").stdout) not in (0, pid))
        wait(lambda: root.status()["Service"]["Active"] and root.status()["TUNActual"])
        assert replacement
        print("SIGKILL cleanup and automatic TUN restart succeeded", flush=True)
        config_path = root.home / "installation/current/config.yaml"
        original_config = run(["sudo", "-n", "cat", config_path]).stdout
        try:
            replace_private_bytes(config_path, original_config + b"\ninvalid: [\n")
            root.cli("service", "stop", success=False)
            assert int(root.ctl("show", root.name, "--property=MainPID", "--value").stdout) == 0
            assert_tun_released(root)
        finally:
            replace_private_bytes(config_path, original_config)
        assert not root.status()["Service"]["Active"]
        print("Corrupt current configuration reports inspection error while Stop still releases TUN resources", flush=True)
        root.cli("tun", "off")
        root.cli("service", "start")
        _, uid, caps = root.process()
        assert uid != 0 and caps == 0
        print("Stop releases ownership and routes; disabling TUN removes network capabilities", flush=True)
        # Clean handoff requires an explicit subsequent administrator action.
        other.cli("tun", "on", "--scope", "host", "--confirm-host")
        other.cli("service", "start")
        assert other.status()["TUNActual"]
        other.cli("service", "stop")
        assert_tun_released(other)
        other.cli("tun", "off")
        print("Explicit TUN handoff to the other root instance succeeded", flush=True)
    except BaseException as error:
        failure = error
    finally:
        failures = []
        if foreign_owned:
            try:
                run(["sudo", "-n", "/usr/sbin/ip", "link", "delete", "dev", foreign_tun])
            except BaseException as error:
                failures.append("dedicated foreign TUN fixture: %s" % error)
        for instance in (other, root, user):
            try:
                instance.cleanup()
            except BaseException as error:
                failures.append("%s: %s" % (instance.name, error))
        try:
            environment.restore()
        except BaseException as error:
            failures.append("VM service defaults: %s" % error)
        for server in (fixture, dns):
            server.shutdown()
            server.server_close()
        if failures:
            raise AssertionError("Dedicated cleanup failed; preserve test directory: " + "; ".join(failures)) from failure
        (work / "cleanup-complete").touch()
        print("Dedicated units, accounts, data, fixtures and TUN resources cleaned", flush=True)
    if failure:
        raise failure
    print("All packaged service-scope scenarios passed", flush=True)


if __name__ == "__main__":
    main()

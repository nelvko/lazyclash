"""GeoData transactions exercised through real Mihomo and both systemd scopes."""
import errno
import http.client
import os
from pathlib import Path
import socket
import subprocess
import threading
import time
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def geosite(domain):
    # GeoSiteList.entry → GeoSite{country_code, domain} → Domain{Full, value}.
    # These fixture strings fit protobuf's single-byte length encoding.
    def field(tag, value):
        assert len(value) < 128
        return bytes([tag << 3 | 2, len(value)]) + value

    match = b'\x08\x03' + field(2, domain.encode())
    return field(1, field(1, b'CN') + field(2, match))


@contextmanager
def local_sources():
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            data = self.server.content.get(self.path)
            self.server.requests.append(self.path)
            if data is None:
                self.send_error(503)
                return
            self.send_response(200)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *_):
            pass

    with ThreadingHTTPServer(('127.0.0.1', 0), Handler) as server:
        server.content = {'/routed': b'geodata-e2e-origin'}
        server.requests = []
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            yield server
        finally:
            server.shutdown()
            worker.join()


def accepted(port, origin_port, domain):
    link = http.client.HTTPConnection('127.0.0.1', port, timeout=4)
    try:
        link.request('GET', f'http://{domain}:{origin_port}/routed')
        response = link.getresponse()
        return response.status == 200 and response.read() == b'geodata-e2e-origin'
    except (ConnectionError, http.client.HTTPException):
        return False
    finally:
        link.close()


def capture_controller(port):
    # Hold the port as soon as the old service has exited. The candidate
    # cannot bind its controller, so real authenticated readiness fails.
    sock = socket.socket()
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    deadline = time.monotonic() + 35
    try:
        while True:
            try:
                sock.bind(('127.0.0.1', port))
                sock.listen()
                return sock
            except OSError as error:
                if error.errno != errno.EADDRINUSE or time.monotonic() >= deadline:
                    raise
                time.sleep(.002)
    except BaseException:
        sock.close()
        raise


def verify_geodata(instances, work, run, wait):
    binary = work / 'mihomo'
    print(run([binary, '-v']).stdout.decode().splitlines()[0], flush=True)
    active = []
    for svc in instances:
        with local_sources() as source:
            svc.install(binary)
            mixed, controller = svc.ports()
            svc.cli('start')
            uid = svc.process()[1]
            assert uid != 0
            scope = 'system' if svc.root else 'user'
            origin_port = source.server_port
            url = f'http://127.0.0.1:{origin_port}'
            names = ['a.fixture.invalid', 'b.fixture.invalid']
            databases = {key: geosite(name) for key, name in zip(('a', 'b'), names)}
            for key, data in databases.items():
                source.content['/db-' + key] = data

            def yaml(key):
                return (f'geox-url: {{geosite: "{url}/db-{key}"}}\n'
                        'hosts: {"a.fixture.invalid": 127.0.0.1, "b.fixture.invalid": 127.0.0.1}\n'
                        'rules: ["GEOSITE,cn,DIRECT", "MATCH,REJECT"]\n').encode()

            def read(*args, success=True):
                return run((['sudo', '-n'] if svc.root else []) + list(args), success=success)

            def generation():
                return read('readlink', svc.home / 'installation/current').stdout.decode().strip()

            runtime = svc.home / 'installation/runtime'

            def database():
                files = read('find', runtime, '-maxdepth', '1', '-iname', 'geosite.dat').stdout.splitlines()
                assert len(files) <= 1, 'multiple GeoSite aliases in active directory'
                return Path(os.fsdecode(files[0])) if files else None

            def expect_live(key):
                assert database(), 'GeoSite database is missing'
                assert read('cat', database()).stdout == databases[key], 'wrong deployed GeoSite bytes'
                owner, mode = read('stat', '-c', '%u %a', database()).stdout.split()
                assert int(owner) == uid and int(mode, 8) & 0o077 == 0, 'GeoSite publication ownership'
                assert [accepted(mixed, origin_port, domain) for domain in names] == [key == 'a', key == 'b'], 'Mihomo routing differs from the deployed GeoSite bytes'

            def add(name, key):
                source.content['/' + name] = yaml(key)
                svc.use_case('profile', 'add', url + '/' + name, '--name', name)

            default_pid = svc.process()[0]
            add('alpha', 'a')
            assert not database() and svc.process()[0] == default_pid, 'non-active validation wrote the live directory'
            svc.use_case('profile', 'apply', 'alpha')
            expect_live('a')
            original, pid = generation(), svc.process()[0]
            add('beta', 'b')
            assert '/db-b' in source.requests and original == generation()
            expect_live('a')
            source.content.pop('/db-b')
            svc.use_case('profile', 'apply', 'beta', success=False)
            assert svc.process()[0] == pid and generation() == original, 'failed validation replaced the running process'
            expect_live('a')
            source.content['/db-b'] = databases['b']
            source.content.pop('/db-a')
            svc.use_case('profile', 'apply', 'beta')
            expect_live('b')
            pid = svc.process()[0]
            assert original != generation()
            source.content['/db-a'] = databases['a']
            source.content['/beta'] = yaml('a')
            svc.use_case('profile', 'refresh', 'beta')
            expect_live('a')
            assert svc.process()[0] != pid, 'refresh that changed GeoData source reused the old process'
            source.content['/alpha'] = yaml('b')
            svc.use_case('profile', 'refresh', 'alpha')
            old = generation()
            source.content.pop('/db-a')
            print(f'{scope}: inactive import, unreachable source, different-source switch and selected-profile source change verified by live routing', flush=True)

            for crash in (False, True):
                svc.ctl('reset-failed', svc.name)
                old_pid = svc.process()[0]
                # Capture the actual manager PID across sudo, then replace that
                # exact subprocess with the unmodified E2E app driver.
                prefix = ['sudo', '-n'] if svc.root else []
                command = svc.fixture[len(prefix):] if svc.root else svc.fixture
                announce = 'import os,sys; print(os.getpid(),flush=True); os.execvp(sys.argv[1],sys.argv[1:])'
                with subprocess.Popen(prefix + ['python3', '-c', announce] + command + ['profile', 'apply', 'alpha'],
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE) as applying:
                    manager = int(applying.stdout.readline())
                    with capture_controller(controller):
                        wait(lambda: generation() != old and database() is not None and
                             read('cat', database()).stdout == databases['b'], timeout=15)
                        wait(lambda: int(svc.ctl('show', svc.name, '--property=MainPID', '--value').stdout) not in (0, old_pid))
                        if crash:
                            assert read('readlink', f'/proc/{manager}/exe').stdout.decode().strip() == str(work / 'lazyclash-fixture')
                            read('kill', '-KILL', str(manager))
                        else:
                            # Release before systemd starts the restored old generation.
                            wait(lambda: generation() == old, timeout=30)
                    _, errors = applying.communicate(timeout=40)
                    assert applying.returncode != 0, f'{scope}: update unexpectedly succeeded'
                    if not crash:
                        assert errors.strip(), 'expected a failure diagnostic'
                if crash:
                    read('test', '-f', svc.home / 'apply-pending.json')
                    svc.use_case('profile', 'list')  # next management operation replays the journal
                assert generation() == old and svc.status()['Service']['Active']
                read('test', '-e', svc.home / 'apply-pending.json', success=False)
                expect_live('a')
                print(f'{scope}: {"SIGKILL and next-invocation recovery" if crash else "real failed API readiness and rollback"} restored old config, exact database and routing with old source offline', flush=True)
            active.append((svc, generation(), svc.process()[0]))
    for svc, expected, pid in active:
        current = (['sudo', '-n'] if svc.root else []) + ['readlink', svc.home / 'installation/current']
        assert run(current).stdout.decode().strip() == expected and svc.process()[0] == pid
    print('Independent scope ownership and final processes verified', flush=True)

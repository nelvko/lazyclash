#!/usr/bin/env python3
"""Configuration business flows inside e2e.sh's dedicated Linux installation."""
import argparse
from contextlib import contextmanager
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile
from threading import Thread
import time
import uuid


@contextmanager
def subscription(path):
    headers = {}

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(path.read_bytes())

        def log_message(self, *_):
            pass

    with ThreadingHTTPServer(('127.0.0.1', 0), Handler) as server:
        worker = Thread(target=server.serve_forever)
        worker.start()
        try:
            yield f'http://127.0.0.1:{server.server_port}/test.yaml', headers
        finally:
            server.shutdown()
            worker.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--home', type=Path, required=True)
    parser.add_argument('--service-name', required=True)
    args = parser.parse_args()
    if (not args.home.is_absolute() or not args.binary.is_absolute()
            or args.home.parent != args.binary.parent
            or not (args.home.parent / '.resources-started').is_file()):
        parser.error('requires the private installation and resource marker created by e2e.sh')
    if not re.fullmatch(r'lazyclash-e2e-[a-f0-9]{12}', args.service_name):
        parser.error('requires e2e.sh\'s dedicated service namespace')
    assert os.geteuid() != 0, 'configuration checks require the ordinary test user'
    command = [str(args.binary), '--home', str(args.home), '--service-name', args.service_name]

    def cli(*words, success=True):
        result = subprocess.run(command + list(words), capture_output=True, timeout=60)
        if (result.returncode == 0) != success:
            raise AssertionError('Configuration command returned unexpected status: ' + ' '.join(words[:2]))
        return result.stdout

    def status():
        value = json.loads(cli('status'))
        assert value['Service']['Active'] and not value['APIError'], 'dedicated core is not ready'
        assert value['Service']['PID'] > 1, 'dedicated core has no running process'
        return value

    def generation():
        return os.readlink(args.home / 'installation' / 'current')

    def rules():
        return [(item['type'], item['payload'], item['proxy']) for item in json.loads(cli('rules'))]

    def write(path, value):
        path.write_text(value)
        path.chmod(0o600)

    library = args.home / 'configuration' / 'library.json'
    suffix = uuid.uuid4().hex[:8]
    full_profile, second_profile = 'full-' + suffix, 'second-' + suffix
    current = args.home / 'installation' / 'current'
    initial = status()
    assert initial['Applied'] == 'fixture', 'configuration checks require the fixture profile'
    initial_generation = generation()

    def record(profile_id):
        return json.loads(library.read_bytes())['profiles'][profile_id]

    def snapshot(profile_id, expected):
        profile = record(profile_id)
        revision = profile['spec']['revision']
        assert revision == hashlib.sha256(expected).hexdigest(), 'profile does not pin the accepted bytes'
        blob = args.home / 'configuration' / 'snapshots' / (revision + '.yaml')
        assert blob.read_bytes() == expected, 'saved configuration snapshot differs from accepted bytes'
        return revision

    def runtime(profile_id, revision, expected_rules):
        value = status()
        manifest = json.loads((current / 'manifest.json').read_bytes())
        build = manifest['build']
        assert value['MixedPort'] == initial['MixedPort'], 'profile replaced the instance port'
        assert value['Applied'] == profile_id and manifest['applied'] == profile_id, 'saved and observed profile identities differ'
        assert build['profile'] == profile_id and build['recipe']['revision'] == revision, 'generation does not pin the expected profile input'
        assert hashlib.sha256((current / 'config.yaml').read_bytes()).hexdigest() == manifest['config_sha256'], 'runtime file differs from its committed digest'
        blob = args.home / 'configuration' / 'snapshots' / (revision + '.yaml')
        assert hashlib.sha256(blob.read_bytes()).hexdigest() == revision, 'pinned runtime snapshot is not immutable'
        assert rules() == expected_rules, 'live rules differ from the committed profile generation'
        return value

    def reset_test_start_count():
        # These replacements share e2e.sh's existing dedicated test unit.
        result = subprocess.run(['systemctl', '--user', 'reset-failed', args.service_name + '.service'],
                                capture_output=True, timeout=10)
        assert result.returncode == 0, 'could not reset the dedicated test unit start count'

    @contextmanager
    def paused(pid):
        fd = os.pidfd_open(pid)
        try:
            main_pid = subprocess.check_output(['systemctl', '--user', 'show', args.service_name + '.service',
                                                '--property=MainPID', '--value'], timeout=10)
            assert int(main_pid) == pid and Path(f'/proc/{pid}/exe').samefile(args.home / 'installation' / 'mihomo')
            signal.pidfd_send_signal(fd, signal.SIGSTOP)
            try:
                yield
            finally:
                try:
                    signal.pidfd_send_signal(fd, signal.SIGCONT)
                except ProcessLookupError:
                    pass  # Timeout recovery may already have replaced this exact process.
        finally:
            os.close(fd)

    reset_test_start_count()

    with tempfile.TemporaryDirectory(prefix='configuration-', dir=args.home.parent) as directory, \
            subscription(Path(directory) / 'full.yaml') as (source, headers):
        work = Path(directory)
        full_path, override_path = work / 'full.yaml', work / 'override.yaml'
        second_path = work / 'second.yaml'

        def full(version):
            return ('mixed-port: 7899\ntun: {enable: true, device: foreign}\n'
                    f'proxies: [{{name: node-v{version}, type: http, server: 127.0.0.1, port: 9}}]\n'
                    f'proxy-groups: [{{name: FULL-E2E, type: select, proxies: [DIRECT, REJECT, node-v{version}]}}]\n'
                    f'rules:\n - DOMAIN,download-v{version}.invalid,DIRECT\n - MATCH,FULL-E2E\n')

        def expected_full(version, custom='user-saved.invalid'):
            return [('Domain', custom, 'REJECT'),
                    ('Domain', 'download-v%s.invalid' % version, 'DIRECT'),
                    ('Match', '', 'FULL-E2E')]

        write(full_path, full(1))
        write(override_path, "prepend-rules: ['DOMAIN,user-custom.invalid,REJECT']\n")
        cli('profile', 'add', source, '--name', full_profile, '--remember-source')
        cli('profile', 'override', full_profile, str(override_path))
        first_revision = snapshot(full_profile, full(1).encode())
        assert record(full_profile)['address'] == source, 'remembered profile lost its refresh address'
        assert status()['Service']['PID'] == initial['Service']['PID'] and generation() == initial_generation, 'saving configuration changed the running core'
        cli('profile', 'apply', full_profile)
        applied = runtime(full_profile, first_revision, expected_full(1, 'user-custom.invalid'))
        assert not applied['ProfilePending']
        before_refresh = generation()

        headers['Subscription-Userinfo'] = 'upload=8; download=24; total=64'
        with paused(applied['Service']['PID']):
            cli('profile', 'refresh', full_profile)
        quota = json.loads(cli('profile', 'show', full_profile))
        assert quota['quota'] == {'used_bytes': 32, 'total_bytes': 64} and not quota['pending']
        assert generation() == before_refresh and status()['Service']['PID'] == applied['Service']['PID']

        write(override_path, "prepend-rules: ['DOMAIN,user-saved.invalid,REJECT']\n")
        cli('profile', 'override', full_profile, str(override_path))
        edited = runtime(full_profile, first_revision, expected_full(1))
        assert edited['Service']['PID'] == applied['Service']['PID'] and not edited['ProfilePending']
        write(override_path, "# New source comments only\nprepend-rules: ['DOMAIN,user-saved.invalid,REJECT']\n")
        effective = (current / 'config.yaml').read_bytes()
        with paused(edited['Service']['PID']):
            cli('profile', 'override', full_profile, str(override_path))
        assert (current / 'config.yaml').read_bytes() == effective
        assert runtime(full_profile, first_revision, expected_full(1))['Service']['PID'] == edited['Service']['PID']
        write(full_path, full(2))
        cli('profile', 'refresh', full_profile)
        second_revision = snapshot(full_profile, full(2).encode())
        refreshed = runtime(full_profile, second_revision, expected_full(2))
        assert refreshed['Service']['PID'] == applied['Service']['PID'] and generation() != before_refresh, 'changed rules did not hot reload in the same process'
        assert next(group['all'] for group in refreshed['Groups'] if group['name'] == 'FULL-E2E') == ['DIRECT', 'REJECT', 'node-v2']
        assert not refreshed['ProfilePending'] and not json.loads(cli('profile', 'show', full_profile))['pending']

        equivalent = '# Provider timestamp changed\n' + full(2).replace('mixed-port: 7899', 'mixed-port: 7988')
        write(full_path, equivalent)
        old_config = (current / 'config.yaml').read_bytes()
        with paused(refreshed['Service']['PID']):
            cli('profile', 'refresh', full_profile)
        second_revision = snapshot(full_profile, equivalent.encode())
        same = runtime(full_profile, second_revision, expected_full(2))
        assert same['Service']['PID'] == refreshed['Service']['PID'] and not same['ProfilePending']
        assert (current / 'config.yaml').read_bytes() == old_config
        print('Current subscription hot reloaded in place; quota-only and equivalent content preserved the running core; saved overrides hot loaded immediately and survived source replacement')

        def wait_pointer(predicate, process, failure):
            deadline = time.monotonic() + 40
            while time.monotonic() < deadline and process.poll() is None:
                if predicate(generation()):
                    return
                time.sleep(0.01)
            raise AssertionError(failure)

        # Pause only this test unit's process. systemd still reports it active,
        # while its controller cannot acknowledge the new configuration.
        old_generation = generation()
        write(full_path, full(3))
        with subprocess.Popen(command + ['profile', 'refresh', full_profile],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as refresh:
            with paused(same['Service']['PID']):
                wait_pointer(lambda link: link != old_generation, refresh, 'timed-out refresh did not switch to the candidate')
                wait_pointer(lambda link: link == old_generation, refresh, 'timed-out refresh did not restore the previous configuration')
            assert refresh.wait(timeout=30) != 0, 'controller timeout was reported as successful'
        restored = runtime(full_profile, second_revision, expected_full(2))
        assert restored['ProfilePending']
        recovered_pid = restored['Service']['PID']
        assert recovered_pid != same['Service']['PID'], 'an unacknowledged in-flight reload could still reach the recovered core'
        recovered_revision = snapshot(full_profile, full(3).encode())
        cli('profile', 'refresh', full_profile)
        assert runtime(full_profile, recovered_revision, expected_full(3))['Service']['PID'] == recovered_pid
        write(full_path, equivalent)
        cli('profile', 'refresh', full_profile)
        runtime(full_profile, second_revision, expected_full(2))
        print(f"A real controller timeout restored old rules and the original generation (PID {same['Service']['PID']} → {recovered_pid}); the next refresh hot loaded the saved source")

        # The editor/import transaction must roll back the library as well as the
        # active generation. Retrying the unchanged input then deploys the edit.
        old_library, old_generation = library.read_bytes(), generation()
        retry_input = "prepend-rules: ['DOMAIN,retry-edit.invalid,DIRECT']\n"
        write(override_path, retry_input)
        with subprocess.Popen(command + ['profile', 'override', full_profile, str(override_path)],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as edit:
            with paused(recovered_pid):
                wait_pointer(lambda link: link != old_generation, edit, 'timed-out edit did not reach the candidate')
                wait_pointer(lambda link: link == old_generation, edit, 'timed-out edit did not restore the old generation')
            assert edit.wait(timeout=30) != 0
        restored = runtime(full_profile, second_revision, expected_full(2))
        assert not restored['ProfilePending'] and library.read_bytes() == old_library
        assert override_path.read_text() == retry_input
        recovered_pid = restored['Service']['PID']
        cli('profile', 'override', full_profile, str(override_path))
        assert rules()[0] == ('Domain', 'retry-edit.invalid', 'DIRECT')
        assert status()['Service']['PID'] == recovered_pid
        write(override_path, "prepend-rules: ['DOMAIN,user-saved.invalid,REJECT']\n")
        cli('profile', 'override', full_profile, str(override_path))
        runtime(full_profile, second_revision, expected_full(2))
        print('An edit that timed out restored the accepted source and live rules; retry loaded the preserved file')
        saved_library, saved_generation, saved_pid = library.read_bytes(), generation(), recovered_pid
        for response in ('proxies: [malformed YAML\n',
                         '{"error":"private-response-token","code":403}',
                         '{"outbounds":[{"type":"direct"}],"dns":{}}'):
            write(full_path, response)
            cli('profile', 'refresh', full_profile, success=False)
            assert library.read_bytes() == saved_library and generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'failed refresh replaced the accepted profile or running generation'
            runtime(full_profile, second_revision, expected_full(2))
        write(override_path, "prepend-rules: ['DOMAIN,invalid-custom.invalid,missing-group']\n")
        cli('profile', 'override', full_profile, str(override_path), success=False)
        cli('profile', 'delete', full_profile, success=False)
        assert library.read_bytes() == saved_library, 'invalid override or protected deletion changed saved inputs'
        write(full_path, equivalent)

        second = ("proxy-groups: [{name: SECOND-E2E, type: select, proxies: [DIRECT, REJECT]}]\n"
                  "rules: ['DOMAIN,second-profile.invalid,REJECT', 'MATCH,SECOND-E2E']\n")
        second_rules = [('Domain', 'second-profile.invalid', 'REJECT'), ('Match', '', 'SECOND-E2E')]
        write(second_path, second)
        cli('profile', 'add', str(second_path), '--name', second_profile)
        imported_revision = snapshot(second_profile, second.encode())
        assert 'address' not in record(second_profile), 'one-time local import retained a private path'
        assert generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'saving another full profile applied it'
        saved_library = library.read_bytes()
        cli('profile', 'refresh', second_profile, success=False)
        assert library.read_bytes() == saved_library, 'one-time import unexpectedly became refreshable'

        # Corrupt only this inactive profile's blob, then restore it in finally.
        # This tests Apply's input-integrity rejection, not restart rollback.
        inactive_blob = args.home / 'configuration' / 'snapshots' / (imported_revision + '.yaml')
        accepted = inactive_blob.read_bytes()
        try:
            inactive_blob.write_bytes(b'corrupted inactive profile\n')
            cli('profile', 'delete', full_profile, '--use', second_profile, success=False)
            assert library.read_bytes() == saved_library and generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'failed apply changed the profile library or running generation'
            runtime(full_profile, second_revision, expected_full(2))
        finally:
            inactive_blob.write_bytes(accepted)
        snapshot(second_profile, second.encode())

        cli('profile', 'apply', second_profile)
        switched = runtime(second_profile, imported_revision, second_rules)
        assert switched['Service']['PID'] != saved_pid, 'explicit profile switch did not restart the running core'
        cli('select', 'SECOND-E2E', 'REJECT')
        selected = status()
        cli('service', 'restart')
        restarted = runtime(second_profile, imported_revision, second_rules)
        assert restarted['Service']['PID'] != selected['Service']['PID'], 'explicit restart did not replace the process'
        assert any(item['name'] == 'SECOND-E2E' and item['now'] == 'REJECT' for item in restarted['Groups']), 'selected node did not survive restart'
        cli('profile', 'delete', second_profile, success=False)
        assert library.read_bytes() == saved_library, 'selected-profile deletion changed saved inputs'
        print('Two full profiles switched independently; failed refresh/override/apply preserved live state; selection survived restart')

        active_generation = generation()
        write(full_path, full(3))
        with paused(restarted['Service']['PID']):
            cli('profile', 'refresh', full_profile)
        other_revision = snapshot(full_profile, full(3).encode())
        assert generation() == active_generation and status()['Service']['PID'] == restarted['Service']['PID']
        runtime(second_profile, imported_revision, second_rules)

        public = cli('profile', 'list')
        assert str(work).encode() not in public, 'public profile listing exposed private refresh addresses'
        reset_test_start_count()
        cli('profile', 'delete', second_profile, '--use', full_profile)
        runtime(full_profile, other_revision, expected_full(3))
        cli('service', 'stop')
        write(full_path, full(4))
        cli('profile', 'refresh', full_profile)
        write(override_path, "prepend-rules: ['DOMAIN,stopped-edit.invalid,REJECT']\n")
        cli('profile', 'override', full_profile, str(override_path))
        stopped_revision = snapshot(full_profile, full(4).encode())
        stopped = json.loads(cli('status'))
        assert stopped['Applied'] == full_profile and not stopped['Service']['Active'] and not stopped['ProfilePending']
        cli('service', 'start')
        runtime(full_profile, stopped_revision, expected_full(4, "stopped-edit.invalid"))
        print('Refreshing another subscription kept the selected core unchanged; a stopped core loaded the updated rules only on explicit start')
        cli('profile', 'delete', full_profile, '--no-profile')
        idle = status()
        assert idle['Applied'] == '' and idle['MixedPort'] == initial['MixedPort']
        cli('profile', 'apply', 'fixture')
        cli('select', 'PROXY', 'DIRECT')
        profile_ids = {item['id'] for item in json.loads(cli('profile', 'list'))}
        assert not profile_ids.intersection({full_profile, second_profile})
        for revision in (first_revision, second_revision, imported_revision):
            blob = args.home / 'configuration' / 'snapshots' / (revision + '.yaml')
            assert hashlib.sha256(blob.read_bytes()).hexdigest() == revision, 'deleting a profile destroyed a historical runtime input'
        assert status()['Applied'] == 'fixture' and not status()['ProfilePending']
        reset_test_start_count()
        print('Snapshot/manifest/live-rule checks and deletion guards passed; fixture restored')


if __name__ == '__main__':
    main()

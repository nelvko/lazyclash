#!/usr/bin/env python3
"""Configuration business flows inside e2e.sh's dedicated Linux installation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid


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

    reset_test_start_count()

    with tempfile.TemporaryDirectory(prefix='configuration-', dir=args.home.parent) as directory:
        work = Path(directory)
        full_path, override_path = work / 'full.yaml', work / 'override.yaml'
        second_path = work / 'second.yaml'

        def full(version):
            return ('proxy-groups: [{name: FULL-E2E, type: select, proxies: [DIRECT, REJECT]}]\n'
                    'rules:\n - DOMAIN,download-v%s.invalid,DIRECT\n - MATCH,FULL-E2E\n' % version)

        def expected_full(version):
            return [('Domain', 'user-custom.invalid', 'REJECT'),
                    ('Domain', 'download-v%s.invalid' % version, 'DIRECT'),
                    ('Match', '', 'FULL-E2E')]

        write(full_path, full(1))
        write(override_path, "prepend-rules: ['DOMAIN,user-custom.invalid,REJECT']\n")
        cli('profile', 'add', str(full_path), '--name', full_profile, '--remember-source')
        cli('profile', 'override', full_profile, str(override_path))
        first_revision = snapshot(full_profile, full(1).encode())
        assert record(full_profile)['address'] == str(full_path), 'remembered profile lost its refresh address'
        assert status()['Service']['PID'] == initial['Service']['PID'] and generation() == initial_generation, 'saving configuration changed the running core'
        cli('profile', 'apply', full_profile)
        applied = runtime(full_profile, first_revision, expected_full(1))
        assert not applied['ProfilePending']
        before_refresh = generation()

        write(full_path, full(2))
        cli('profile', 'refresh', full_profile)
        second_revision = snapshot(full_profile, full(2).encode())
        refreshed = runtime(full_profile, first_revision, expected_full(1))
        assert refreshed['Service']['PID'] == applied['Service']['PID'] and generation() == before_refresh, 'refresh changed the running process or configuration'
        assert refreshed['ProfilePending'] and json.loads(cli('profile', 'show', full_profile))['pending'], 'pending profile revision was not exposed'
        cli('profile', 'apply', full_profile)
        reapplied = runtime(full_profile, second_revision, expected_full(2))
        assert not reapplied['ProfilePending'] and generation() != before_refresh
        print('Full profile refresh preserved the running generation and custom rules until explicit apply')

        saved_library, saved_generation, saved_pid = library.read_bytes(), generation(), reapplied['Service']['PID']
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
        write(full_path, full(2))

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
            cli('profile', 'apply', second_profile, success=False)
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

        public = cli('profile', 'list')
        assert str(work).encode() not in public, 'public profile listing exposed private refresh addresses'
        cli('profile', 'apply', 'fixture')
        cli('select', 'PROXY', 'DIRECT')
        for profile_id in (full_profile, second_profile):
            cli('profile', 'delete', profile_id)
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

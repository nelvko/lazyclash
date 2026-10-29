#!/usr/bin/env python3
"""Configuration business flows inside e2e.sh's dedicated Linux installation."""
import argparse
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
    full_source, full_profile = 'full-source-' + suffix, 'full-profile-' + suffix
    left_source, right_source = 'left-' + suffix, 'right-' + suffix
    managed_profile = 'managed-' + suffix
    initial = status()
    assert initial['Applied'] == 'fixture', 'configuration checks require the fixture profile'
    initial_generation = generation()

    def reset_test_start_count():
        # The owned unit intentionally limits starts to five per minute. This
        # scenario performs five successful explicit replacements in succession;
        # isolate its start count from neighboring tests without changing policy.
        result = subprocess.run(['systemctl', '--user', 'reset-failed', args.service_name + '.service'],
                                capture_output=True, timeout=10)
        assert result.returncode == 0, 'could not reset the dedicated test unit start count'

    reset_test_start_count()

    with tempfile.TemporaryDirectory(prefix='configuration-', dir=args.home.parent) as directory:
        work = Path(directory)
        full_path, override_path = work / 'full.yaml', work / 'override.yaml'
        left_path, right_path, template_path = work / 'left.yaml', work / 'right.yaml', work / 'template.yaml'

        def full(version):
            return ('proxy-groups: [{name: FULL-E2E, type: select, proxies: [DIRECT, REJECT]}]\n'
                    'rules:\n - DOMAIN,source-v%s.invalid,DIRECT\n - MATCH,FULL-E2E\n' % version)

        write(full_path, full(1))
        write(override_path, "prepend-rules: ['DOMAIN,user-custom.invalid,REJECT']\n")
        cli('subscription', 'add', full_source, str(full_path))
        cli('profile', 'create', full_profile, '--source', full_source)
        cli('profile', 'override', full_profile, str(override_path))
        assert status()['Service']['PID'] == initial['Service']['PID'] and generation() == initial_generation, 'saving configuration changed the running core'
        cli('profile', 'apply', full_profile)
        applied = status()
        assert applied['Applied'] == full_profile and not applied['ProfilePending']
        assert rules() == [('Domain', 'user-custom.invalid', 'REJECT'), ('Domain', 'source-v1.invalid', 'DIRECT'), ('Match', '', 'FULL-E2E')], 'full profile and user rules were not composed'
        before_refresh = generation()

        write(full_path, full(2))
        cli('subscription', 'refresh', full_source)
        refreshed = status()
        assert refreshed['Service']['PID'] == applied['Service']['PID'] and generation() == before_refresh, 'refresh changed the running process or configuration'
        assert refreshed['ProfilePending'] and json.loads(cli('profile', 'show', full_profile))['pending'], 'pending source revision was not exposed'
        assert rules()[1] == ('Domain', 'source-v1.invalid', 'DIRECT'), 'refresh changed live rules before apply'
        cli('profile', 'apply', full_profile)
        reapplied = status()
        assert not reapplied['ProfilePending'] and generation() != before_refresh
        assert rules()[:2] == [('Domain', 'user-custom.invalid', 'REJECT'), ('Domain', 'source-v2.invalid', 'DIRECT')], 'explicit apply lost custom rules or the new source revision'
        print('Full profile source refresh preserved PID and custom rules until explicit apply')

        saved_library, saved_generation, saved_pid = library.read_bytes(), generation(), reapplied['Service']['PID']
        write(full_path, 'proxies: [malformed YAML\n')
        cli('subscription', 'refresh', full_source, success=False)
        assert library.read_bytes() == saved_library and generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'failed refresh changed the library or running generation'
        # Mihomo -t accepts these as empty/default configurations. The source
        # boundary must reject them before they replace a useful subscription.
        for response in ('{"error":"private-response-token","code":403}',
                         '{"outbounds":[{"type":"direct"}],"dns":{}}'):
            write(full_path, response)
            cli('subscription', 'refresh', full_source, success=False)
            assert library.read_bytes() == saved_library and generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'unrecognized source response replaced the working subscription'
        cli('profile', 'delete', full_profile, success=False)
        cli('subscription', 'delete', full_source, success=False)
        assert library.read_bytes() == saved_library, 'protected deletion changed saved inputs'
        write(full_path, full(2))

        write(left_path, "proxies: [{name: local-one, type: direct}]\nproxy-groups: [{name: ignored, type: select, proxies: [missing]}]\nrules: ['MATCH,ignored']\n")
        write(right_path, 'proxies: [{name: local-two, type: direct}]\n')
        write(template_path, "proxy-groups: [{name: MANAGED-E2E, type: select, proxies: [DIRECT, REJECT], include-all-proxies: true}]\nrules: ['MATCH,MANAGED-E2E']\n")
        cli('subscription', 'add', left_source, str(left_path))
        cli('subscription', 'add', right_source, str(right_path))
        cli('profile', 'create', managed_profile, '--managed', '--sources', left_source + ',' + right_source, '--template', str(template_path))
        assert generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'saving a managed recipe applied it'
        cli('profile', 'apply', managed_profile)
        managed = status()
        group = next(item for item in managed['Groups'] if item['name'] == 'MANAGED-E2E')
        assert {'local-one', 'local-two', 'DIRECT', 'REJECT'} <= set(group['all']), 'managed profile lost an inline node or template selection'
        assert rules() == [('Match', '', 'MANAGED-E2E')], 'managed profile used discarded source rules'
        cli('select', 'MANAGED-E2E', 'REJECT')
        selected = status()
        cli('service', 'restart')
        restarted = status()
        assert restarted['Service']['PID'] != selected['Service']['PID'], 'explicit restart did not replace the process'
        assert any(item['name'] == 'MANAGED-E2E' and item['now'] == 'REJECT' for item in restarted['Groups']), 'selected node did not survive restart'

        saved_library, saved_generation, saved_pid = library.read_bytes(), generation(), restarted['Service']['PID']
        write(left_path, 'proxies: [{name: local-two, type: direct}]\n')
        cli('subscription', 'refresh', left_source, success=False)
        assert library.read_bytes() == saved_library and generation() == saved_generation and status()['Service']['PID'] == saved_pid, 'conflicting managed update changed the saved or running configuration'
        cli('profile', 'delete', managed_profile, success=False)
        cli('subscription', 'delete', left_source, success=False)
        assert library.read_bytes() == saved_library, 'managed dependency deletion changed saved inputs'
        print('Managed profile extracted two node sources; selection survived real systemd restart; invalid updates preserved the working generation')

        public = cli('subscription', 'list')
        assert str(work).encode() not in public, 'public source listing exposed private source addresses'
        cli('profile', 'apply', 'fixture')
        cli('select', 'PROXY', 'DIRECT')
        for name in (full_profile, managed_profile):
            cli('profile', 'delete', name)
        for name in (full_source, left_source, right_source):
            cli('subscription', 'delete', name)
        names = {item['name'] for item in json.loads(cli('profile', 'list'))}
        assert full_profile not in names and managed_profile not in names
        names = {item['name'] for item in json.loads(cli('subscription', 'list'))}
        assert not names.intersection({full_source, left_source, right_source})
        assert status()['Applied'] == 'fixture' and not status()['ProfilePending']
        reset_test_start_count()
        print('Configuration deletion guards passed; fixture restored and temporary library entries removed')


if __name__ == '__main__':
    main()

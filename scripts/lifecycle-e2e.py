#!/usr/bin/env python3
"""Opt-in checks against the E2E harness's isolated, owned Linux installation."""
import argparse
import json
import os
import signal
import subprocess
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', required=True)
parser.add_argument('--home', required=True)
parser.add_argument('--service-name', required=True)
args = parser.parse_args()
command = [args.binary, '--home', args.home, '--service-name', args.service_name]


def lp(*words, success=True):
    result = subprocess.run(command + list(words), capture_output=True, timeout=30)
    if success and result.returncode:
        raise AssertionError('lifecycle command failed: ' + ' '.join(words))
    if not success:
        assert result.returncode != 0, 'unsupported capability unexpectedly succeeded'
    return result.stdout


def status():
    return json.loads(lp('status'))


def ready(previous_pid=None, selected=None):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        current = status()
        if (current['Service']['Active'] and not current['APIError']
                and current['Service']['PID'] != previous_pid
                and (selected is None or any(g['name'] == 'PROXY' and g['now'] == selected
                                             for g in current['Groups']))):
            return current
        time.sleep(.2)
    raise AssertionError('owned Mihomo did not become ready after failure')


assert os.geteuid() != 0, 'privilege checks must run as the ordinary test user'
lp('service', 'start')
initial = ready()
assert initial['Service']['LingerKnown'] and initial['Service']['Linger']
assert initial['Desired']['running'] and initial['Capabilities']['scope'] == 'user'
assert not initial['Capabilities']['tun']['supported']
lp('tun', 'on', success=False)

# Observe an already-running service from a controlling terminal; an actual
# terminal hangup must not kill the process in systemd's service cgroup.
pid, terminal = os.forkpty()
if pid == 0:
    os.execv(args.binary, command)
try:
    time.sleep(1)
    os.kill(pid, signal.SIGHUP)
    os.waitpid(pid, 0)
finally:
    os.close(terminal)
assert ready()['Service']['PID'] == initial['Service']['PID'], 'terminal hangup changed Mihomo'
print('Real terminal SIGHUP preserved the systemd-managed PID')

lp('select', 'PROXY', 'REJECT')
lp('mode', 'direct')
changed = ready(selected='REJECT')
assert changed['Service']['PID'] == initial['Service']['PID'], 'dynamic mode restarted Mihomo'
assert changed['Mode']['actual'] == changed['Mode']['desired'] == 'direct'
lp('profile', 'apply', 'default')
lp('service', 'restart')
changed = ready(selected='REJECT')
assert changed['Mode']['actual'] == changed['Mode']['desired'] == 'direct'
assert any(g['name'] == 'PROXY' and g['now'] == 'REJECT' for g in changed['Groups'])
print('Mode and selector survived configuration application and explicit restart')

# Only kill the PID returned by this dedicated, ownership-verified installation.
old_pid = changed['Service']['PID']
assert old_pid > 1 and os.stat('/proc/%d' % old_pid).st_uid == os.geteuid()
os.kill(old_pid, signal.SIGKILL)
recovered = ready(previous_pid=old_pid, selected='REJECT')
assert recovered['Service']['Restarts'] > changed['Service']['Restarts']
assert recovered['Desired']['running'] and recovered['Mode']['actual'] == 'direct'
assert any(g['name'] == 'PROXY' and g['now'] == 'REJECT' for g in recovered['Groups'])
print('Real SIGKILL triggered systemd recovery with the persisted mode and selector')

running_pid = recovered['Service']['PID']
lp('service', 'enable')
assert ready()['Service']['Enabled'] and ready()['Service']['PID'] == running_pid
lp('service', 'disable')
assert not ready()['Service']['Enabled'] and ready()['Service']['PID'] == running_pid
lp('service', 'stop')
time.sleep(3)  # exceeds RestartSec; an explicit stop must suppress restart
stopped = status()
assert not stopped['Service']['Active'] and stopped['Service']['PID'] == 0
assert not stopped['Desired']['running'] and not stopped['Drift']
print('Enable/disable preserved PID; explicit Stop stayed stopped beyond RestartSec')
lp('mode', 'rule')
lp('select', 'PROXY', 'DIRECT', success=False)  # no fake success against a stopped API
print('Persistent lifecycle scenarios passed; SSH logout/reboot still require explicit host testing')

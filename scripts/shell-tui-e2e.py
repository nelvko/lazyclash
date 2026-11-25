#!/usr/bin/env python3
"""Real Linux TUI + isolated systemd/Mihomo terminal-proxy acceptance.

Requires an existing local Mihomo binary. Uses random service name, temporary
HOME/config/cache/data, and removes only its own temporary systemd link.
"""
import argparse
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import signal
import socket
import shutil
import struct
import subprocess
import tempfile
import termios
import time


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


HOOKS = load('shell_hooks', 'shell-integration-pty.py')
VT = load('shell_vt', 'pty-smoke.py')


class Terminal(HOOKS.Shell):
    def __init__(self, name, env, output):
        self.output = output
        self.screen = VT.Screen(160, 60)
        super().__init__(name, env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack('HHHH', 60, 160, 0, 0))
        os.kill(self.pid, signal.SIGWINCH)

    def read_for(self, duration):
        data = super().read_for(duration)
        self.screen.feed(data)
        return data

    def wait(self, text, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.read_for(.05)
            if text in self.screen.text():
                return
        raise AssertionError('missing '+text+'\n'+self.screen.text())

    def toggle(self, command, label, install=False):
        os.write(self.fd, (command+'\n').encode())
        state = '● On' if label == 'Disable' else ('○ Not set up' if install else '○ Off')
        column, row = self.wait_terminal_state(state)
        (self.output/('network-'+label.lower()+'.txt')).write_text(self.screen.text())
        os.write(self.fd, f'\x1b[<0;{column+1};{row+1}M\x1b[<0;{column+1};{row+1}m'.encode())
        if install:
            self.wait('Install Shell Integration?')
            (self.output/'install-consent.txt').write_text(self.screen.text())
            os.write(self.fd, b'y')
        pending = 'Pending off' if label == 'Disable' else 'Pending on'
        self.wait_terminal_state('◌ ' + ('Activation needed' if install else pending))
        os.write(self.fd, b'q')
        self.read_for(.7)

    def wait_terminal_state(self, state):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            self.read_for(.05)
            point = self.screen.point_below('Terminal proxy', 1, state)
            if not self.screen.synchronized and point is not None:
                return point
        raise AssertionError('Terminal proxy did not show '+state+'\n'+self.screen.text())


def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--mihomo', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    result = {}
    with tempfile.TemporaryDirectory(prefix='lazyclash-shell-tui-') as root:
        root = Path(root)
        env = HOOKS.env_for(root, binary)
        actual_config = Path(os.environ.get('XDG_CONFIG_HOME', str(Path.home()/'.config')))
        integration_dir = actual_config/'lazyclash/shell'
        assert not integration_dir.exists(), 'use a VM without an existing Shell Integration installation'
        # systemd reads the actual user unit search path. Shell startup files
        # remain in temporary HOME; only our own new integration subdir is used.
        env['XDG_CONFIG_HOME'] = str(actual_config)
        env.update(LAZYCLASH_LANG='en', LANG='C.UTF-8', LC_ALL='C.UTF-8', TERM='xterm-256color')
        name = 'lazyclash-shell-'+os.urandom(5).hex()
        home = str(root/'data')
        argv = [binary, '--home', home, '--service-name', name, '--lang', 'en']
        unit = Path(env['XDG_CONFIG_HOME'])/'systemd/user'/(name+'.service')
        actual_config = Path(os.environ.get('XDG_CONFIG_HOME', str(Path.home()/'.config')))
        link = actual_config/'systemd/user'/(name+'.service')
        shell = None
        def run(parts, **kw):
            completed = subprocess.run(parts, env=env, capture_output=True, text=True, timeout=60, **kw)
            if completed.returncode:
                raise AssertionError(str(parts)+'\n'+completed.stdout+completed.stderr)
            return completed.stdout
        try:
            run(argv+['install', '--bin', str(Path(args.mihomo).resolve())])
            run(['systemctl', '--user', 'daemon-reload'])
            mixed, controller = port(), port()
            run(argv+['settings', 'ports', '--mixed-port', str(mixed), '--controller-port', str(controller)])
            run(argv+['service', 'start'])
            shell = Terminal('bash', env, output)
            shell.command("export http_proxy='original-lower' HTTP_PROXY='original-upper'; unset https_proxy HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY")
            command = shlex.join(argv)
            start = len(shell.transcript)
            shell.toggle(command, 'Enable', install=True)
            first_exit = bytes(shell.transcript[start:])
            assert b'Run once in this terminal:' in first_exit, first_exit[-3000:]
            assert first_exit.rfind(b'Run once in this terminal:') > first_exit.rfind(b'\x1b[?1049l'), 'notice printed before alternate screen restored'
            script = Path(env['XDG_CONFIG_HOME'])/'lazyclash/shell/bash.sh'
            shell.command('source '+shlex.quote(str(script))+f'; test "$http_proxy" = http://127.0.0.1:{mixed} && test "$HTTP_PROXY" = "$http_proxy" && test "$all_proxy" = socks5h://127.0.0.1:{mixed}')
            status = shell.command(shlex.quote(binary)+' shell status --json')
            assert '"state":"on"' in status and '"pending":false' in status, status
            result['first_source_immediate'] = True
            curl = shell.command("curl -v -I --max-time 30 https://github.com", timeout=40)
            (output/'curl.txt').write_text(curl)
            assert 'CONNECT github.com:443' in curl and re.search(r'HTTP/[12](?:\.\d)? 200', curl), curl
            result['curl_github_via_mihomo'] = True
            newer = port()
            shell.command(shlex.join(argv+['settings','ports','--mixed-port',str(newer),'--restart']),timeout=40)
            shell.command(f'test "$http_proxy" = http://127.0.0.1:{newer}')
            result['committed_port_sync'] = True
            shell.toggle(command, 'Disable')
            shell.command('test "$http_proxy" = original-lower && test "$HTTP_PROXY" = original-upper && test "${https_proxy+x}" != x && test "${all_proxy+x}" != x')
            result['tui_disable_restores_original'] = True
            # A SIGKILL during a later TUI leaves the already applied shell state intact.
            shell.toggle(command, 'Enable')
            child_text = shell.command('printf "LC_OWNER:%s\\n" "$$"')
            tui_before = len(shell.transcript)
            os.write(shell.fd,(command+'\n').encode()); shell.wait('Terminal proxy'); shell.read_for(.3)
            children = Path(f'/proc/{shell.pid}/task/{shell.pid}/children').read_text().split()
            tui_pid = next(int(pid) for pid in children if Path(f'/proc/{pid}/exe').resolve()==Path(binary))
            os.kill(tui_pid,signal.SIGKILL)
            # SIGKILL cannot restore terminal modes. This is an acceptance driver reset.
            shell.read_for(.2); os.write(shell.fd,b'stty sane\n'); shell.read_for(.2)
            shell.command(f'test "$http_proxy" = http://127.0.0.1:{newer}')
            result['tui_sigkill_retains_applied_proxy'] = True
            assert bytes(shell.transcript[start:]).count(b'Run once in this terminal:') == 1, 'first activation notice repeated'
            # Uninstall must return, allowing the shell prompt to restore before cleanup.
            shell.command(shlex.quote(binary)+' shell uninstall')
            shell.read_for(.7)
            shell.command('test "$http_proxy" = original-lower && test "$HTTP_PROXY" = original-upper')
            deadline=time.monotonic()+5
            while script.exists() and time.monotonic()<deadline:
                time.sleep(.1)
            assert not script.exists(), 'uninstall worker did not complete'
            result['uninstall_restores_before_cleanup'] = True
        finally:
            if shell:
                (output/'terminal.ansi').write_bytes(shell.transcript)
                (output/'terminal.txt').write_text(shell.screen.text())
                shell.close()
            subprocess.run(argv+['service','stop'],env=env,capture_output=True,timeout=30)
            # The random unit is ours even if the app's verification reports a
            # test failure; ensure no test process survives cleanup.
            subprocess.run(['systemctl','--user','stop',name+'.service'],capture_output=True,timeout=30)
            subprocess.run(argv+['uninstall'],env=env,capture_output=True,timeout=30)
            if link.is_symlink() and link.resolve()==unit.resolve():
                link.unlink()
            subprocess.run(['systemctl','--user','daemon-reload'],capture_output=True,timeout=30)
            if unit.exists(): unit.unlink()
            subprocess.run(['systemctl','--user','daemon-reload'],capture_output=True,timeout=30)
            if integration_dir.exists(): shutil.rmtree(integration_dir)
        (output/'result.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps(result))


if __name__=='__main__':
    main()

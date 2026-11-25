#!/usr/bin/env python3
"""Exercise the Overview core action against real isolated Linux Mihomo/systemd.

The random service uses temporary data, cache and HOME. Only its own unit is
written into the actual systemd user-unit search path; no existing unit or
shell startup file is edited. --mihomo must name an existing local executable.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time


SPEC = importlib.util.spec_from_file_location('core_toggle_smoke', Path(__file__).with_name('pty-smoke.py'))
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def capture(terminal, output, label):
    (output/(label+'.txt')).write_text(terminal.screen.text())
    (output/(label+'.ansi')).write_bytes(terminal.raw)


def sidebar_seconds(terminal):
    for row in terminal.screen.cells:
        match = re.fullmatch(r'(\d{2,}):([0-5]\d):([0-5]\d)', ''.join(row[:20]).strip())
        if match:
            hours, minutes, seconds = map(int, match.groups())
            return hours * 3600 + minutes * 60 + seconds
    return None


def wait_sidebar_clock(terminal, lower, upper):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        terminal.read()
        seconds = sidebar_seconds(terminal)
        if (not terminal.screen.synchronized and terminal.screen.state == 'text'
                and seconds is not None and lower <= seconds < upper):
            assert 'Up for ' not in terminal.screen.text(), 'wide core card repeated the sidebar clock'
            return seconds
    raise AssertionError('sidebar clock did not reach the expected process age\n' + terminal.screen.text())


def heading_action(terminal, label):
    for y, row in enumerate(terminal.screen.cells):
        if 'Core Status' not in ''.join(row):
            continue
        for x, cell in enumerate(row):
            if cell and ''.join(row[x:]).startswith(label):
                return x, y
    return None


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',required=True,type=Path)
    parser.add_argument('--mihomo',required=True,type=Path)
    parser.add_argument('--output',required=True,type=Path)
    args=parser.parse_args()
    binary=args.binary.resolve()
    output=args.output.resolve();output.mkdir(parents=True,exist_ok=True)
    results={}
    with tempfile.TemporaryDirectory(prefix='lazyclash-core-toggle-') as directory:
        root=Path(directory)
        env={key:value for key,value in os.environ.items() if key.lower() not in {'http_proxy','https_proxy','all_proxy','no_proxy'} and not key.startswith('LAZYCLASH_SHELL_')}
        config=Path(env.get('XDG_CONFIG_HOME',str(Path.home()/'.config')))
        env.update(HOME=str(root),XDG_CONFIG_HOME=str(config),XDG_CACHE_HOME=str(root/'cache'),XDG_DATA_HOME=str(root/'data'),LAZYCLASH_LANG='en',LC_ALL='C.UTF-8')
        name='lazyclash-toggle-'+os.urandom(5).hex()
        home=root/'managed'
        argv=[str(binary),'--home',str(home),'--service-name',name,'--lang','en']
        unit=config/'systemd/user'/(name+'.service')
        terminal=None;unit_bytes=None
        def run(parts):
            completed=subprocess.run(parts,env=env,capture_output=True,text=True,timeout=45)
            if completed.returncode:
                raise AssertionError(str(parts)+'\n'+completed.stdout+completed.stderr)
            return completed.stdout
        def status():
            return json.loads(run(argv+['status']))['Service']
        try:
            run(argv+['install','--bin',str(args.mihomo.resolve())])
            unit_bytes=unit.read_bytes()
            mixed,controller=free_port(),free_port()
            while controller==mixed: controller=free_port()
            run(argv+['settings','ports','--mixed-port',str(mixed),'--controller-port',str(controller)])
            wrapper='import json,os,sys; os.environ.update(json.loads(sys.argv[1])); os.execv(sys.argv[2],sys.argv[2:])'
            terminal=SMOKE.Terminal([sys.executable,'-c',wrapper,json.dumps(env),*argv])
            terminal.wait('○ Stopped');terminal.settle()
            assert not status()['Active']
            assert heading_action(terminal, 'Start') is not None
            assert 'Up for ' not in terminal.screen.text()
            assert sidebar_seconds(terminal) is None
            capture(terminal,output,'stopped')
            results['stopped_observation']=True

            # All queued repeats arrive while the first lifecycle operation is
            # pending. Their state changes must not create a second operation.
            x, y = heading_action(terminal, 'Start')
            terminal.send(f'\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m'.encode()+b'\r \r')
            terminal.wait('● Running');wait_sidebar_clock(terminal, 0, 60);terminal.settle()
            active=status();assert active['Active'] and active['PID']>0,active
            first_pid=active['PID']
            assert heading_action(terminal, 'Stop') is not None
            capture(terminal,output,'toggle-started')
            results['toggle_start_and_pending_dedup']=True

            terminal.send(b' \r \r')
            terminal.wait('○ Stopped');terminal.settle()
            stopped=status();assert not stopped['Active'] and stopped['PID']==0,stopped
            assert 'Up for ' not in terminal.screen.text()
            assert sidebar_seconds(terminal) is None
            capture(terminal,output,'toggle-stopped')
            results['toggle_stop_and_pending_dedup']=True

            # A real occupied port causes the existing application start path
            # to fail before systemd starts. The core must stay stopped.
            with socket.socket() as blocker:
                blocker.bind(('127.0.0.1',mixed));blocker.listen()
                terminal.send(b' ')
                terminal.wait('other service was stopped');terminal.settle()
                capture(terminal,output,'failed-start-error')
                # Dismiss the Toast before checking the heading action below it.
                for _ in range(4):
                    if '×' not in terminal.screen.text(): break
                    terminal.click_text('×');terminal.settle()
                terminal.wait('○ Stopped')
                assert heading_action(terminal, 'Start') is not None
                assert not status()['Active']
                capture(terminal,output,'failed-start-actual-off')
            results['start_failure_rereads_off']=True

            run(argv+['service','start'])
            terminal.wait('● Running');initial = wait_sidebar_clock(terminal, 0, 60);terminal.settle()
            external=status();assert external['Active'],external
            capture(terminal,output,'external-start')
            results['external_start_sync']=True
            wait_sidebar_clock(terminal, initial + 1, 60)
            results['sidebar_seconds_advance']=True
            terminal.resize(80, 24)
            terminal.wait('Up for <1m')
            assert sidebar_seconds(terminal) is None
            terminal.wait('Up for 1m', timeout=85)
            capture(terminal,output,'uptime-advanced')
            terminal.resize(120, 40)
            wait_sidebar_clock(terminal, 60, 120)
            results['uptime_updates']=True
            old_pid=external['PID']
            run(argv+['service','restart'])
            restarted=status();assert restarted['Active'] and restarted['PID']!=old_pid,restarted
            wait_sidebar_clock(terminal, 0, 60)
            capture(terminal,output,'external-restart')
            results['external_restart_and_uptime_reset']=True
            terminal.send(b'r');terminal.settle()
            assert status()['PID']==restarted['PID'],'Overview retained a restart shortcut'
            results['overview_restart_shortcut_removed']=True

            run(argv+['service','stop'])
            terminal.wait('○ Stopped');terminal.settle()
            assert not status()['Active']
            capture(terminal,output,'external-stop')
            results['external_stop_sync']=True
            for columns,rows in ((80,24),(60,20),(40,16),(120,40)):
                terminal.resize(columns,rows);terminal.settle()
                terminal.wait('○ Stopped')
                assert heading_action(terminal, 'Start') is not None
                capture(terminal,output,f'stopped-{columns}x{rows}')
            results['narrow_layout_and_recovery']=True
            results['saw_starting_frame']='◌ Starting' in terminal.text()
            results['saw_stopping_frame']='◌ Stopping' in terminal.text()
            results['first_pid']=first_pid
            terminal.exit()
            assert b'\x1b[?1049l' in terminal.raw,'TUI did not restore the terminal'
        finally:
            if terminal:
                capture(terminal,output,'final')
                terminal.cleanup()
            subprocess.run(argv+['service','stop'],env=env,capture_output=True,timeout=30)
            subprocess.run(['systemctl','--user','stop',name+'.service'],capture_output=True,timeout=30)
            subprocess.run(argv+['uninstall'],env=env,capture_output=True,timeout=30)
            if unit.exists():
                if unit_bytes is None or unit.read_bytes()!=unit_bytes:
                    raise AssertionError('test unit changed externally; preserving it for inspection')
                unit.unlink()
            subprocess.run(['systemctl','--user','daemon-reload'],capture_output=True,timeout=30)
            results['cleanup_complete']=not unit.exists()
        (output/'result.json').write_text(json.dumps(results,indent=2)+'\n')
        print(json.dumps(results),flush=True)


if __name__=='__main__':
    main()

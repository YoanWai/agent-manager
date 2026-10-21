#!/usr/bin/env python3
"""Real-process heartbeat takeover on one disposable profile and two sockets.

Pauses one manager past the heartbeat horizon. This proves takeover and
reclamation, not exclusive message delivery or fencing of an in-flight send.
"""
import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import pty
import threading
import sqlite3
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'e2e'))
from smoke import Sandbox
from scenarios import profile_dir


def run(binary, work):
    sandbox = Sandbox(work / 'artifacts')
    real_tmux = shutil.which('tmux')
    sockets = {name: sandbox.socket_dir / f'tmux-{os.getuid()}' / f'manager-{name}' for name in ('a', 'b')}
    pids = {}
    processes = {}
    masters = []
    observations = []
    config = profile_dir(sandbox)
    config.mkdir(parents=True, exist_ok=True)
    (config / 'config.toml').write_text('poll_interval = "1s"\n')
    db = config / 'state.db'

    def tmux(name, *args, check=True):
        return subprocess.run([real_tmux, '-f', '/dev/null', '-S', str(sockets[name]), *args],
                              env=sandbox.env, text=True, capture_output=True, timeout=10, check=check)

    def claim():
        if not db.exists():
            return ''
        with sqlite3.connect(f'file:{db}?mode=ro', uri=True, timeout=1) as conn:
            row = conn.execute("SELECT value FROM settings WHERE key='poller_socket'").fetchone()
        return row[0] if row else ''

    def wait_claim(name, timeout):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            current = claim()
            observations.append(dict(at=time.time(), owner=current))
            if current == str(sockets[name]):
                return
            time.sleep(0.2)
        raise TimeoutError(f'manager {name} did not claim profile within {timeout}s')

    def start(name):
        bin_dir = work / f'bin-{name}'
        bin_dir.mkdir()
        shim = bin_dir / 'tmux'
        shim.write_text('#!/bin/sh\nif [ "$1" = -L ]; then shift 2; fi\n' +
                        f'exec {shlex.quote(real_tmux)} -S {shlex.quote(str(sockets[name]))} "$@"\n')
        shim.chmod(0o700)
        env = dict(sandbox.env, PATH=str(bin_dir) + os.pathsep + sandbox.env['PATH'])
        tmux(name, 'new-session', '-d', '-s', 'fixture', 'sleep 300')
        master, slave = pty.openpty()
        masters.append(master)
        process = subprocess.Popen([str(binary)], env=env, cwd=work, stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
        os.close(slave)
        processes[name] = process
        pids[name] = process.pid
        def read_output():
            with (work / f'manager-{name}.log').open('wb') as log:
                while True:
                    try:
                        data = os.read(master, 65536)
                    except OSError:
                        return
                    if not data:
                        return
                    log.write(data)
                    log.flush()
        threading.Thread(target=read_output, daemon=True).start()


    try:
        start('a')
        wait_claim('a', 15)
        start('b')
        time.sleep(3)
        if claim() != str(sockets['a']):
            raise AssertionError('fresh A heartbeat did not retain ownership')
        os.kill(pids['a'], signal.SIGSTOP)
        wait_claim('b', 50)
        os.kill(pids['a'], signal.SIGCONT)
        time.sleep(3)
        if claim() != str(sockets['b']):
            raise AssertionError('resumed A stole fresh B heartbeat')
        processes['b'].terminate()
        processes['b'].wait(timeout=10)
        pids.pop('b')
        wait_claim('a', 50)
        return dict(status='passed', checks=['fresh owner retained', 'stale owner replaced', 'fresh competitor retained', 'owner reclaimed'],
                    gaps=['in-flight message delivery', 'mixed released managers', 'real SSH'])
    finally:
        for name, pid in pids.items():
            try:
                os.kill(pid, signal.SIGCONT)
            except ProcessLookupError:
                pass
        for process in processes.values():
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        for master in masters:
            os.close(master)
        for name in sockets:
            tmux(name, 'kill-server', check=False)
        sandbox.close()
        (work / 'timeline.json').write_text(json.dumps(observations, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--work', type=Path, required=True)
    args = parser.parse_args()
    started = time.monotonic()
    try:
        result = run(args.binary.resolve(), args.work.resolve())
    except Exception as error:
        result = dict(status='failed', error=str(error))
    result['seconds'] = round(time.monotonic() - started, 2)
    (args.work / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(args.work))))
    sys.exit(0 if result['status'] == 'passed' else 1)

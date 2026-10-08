#!/usr/bin/env python3
"""SSH connection scenario against the real binary; Python standard library only.

A fixture `ssh` first on PATH drops every argument but the remote command and
runs it with /bin/sh under a second disposable profile, so the manager talks
to a real agent-manager "host" without a network or the user's ~/.ssh. The
scenario adds a connection through the dialog, sees the remote row,
quick-sends to it and kills it. Every process it started is ended by PID.
"""
import argparse
import json
import os
import shlex
import shutil
import signal
import socket
import sqlite3
import subprocess
import tempfile
import time
from pathlib import Path
from types import SimpleNamespace

from scenarios import (FAKE_BIN, FAKE_CLI, READY_MARKER, capture, config_fixture_text,
                       ensure_binary, fake_cli_script, frame, key, preview_of, profile_dir)
from smoke import Sandbox

CONNECTION = 'box'
REMOTE_AGENT = 'remote-agent'
QUICK_TEXT = 'ssh-quick-input'


def fake_ssh_script(remote_env):
    exports = ' '.join(f'{name}={shlex.quote(value)}' for name, value in remote_env.items())
    return ('#!/bin/sh\n'
            '# disposable fixture ssh: runs the remote command here, under a second profile\n'
            'for last; do :; done\n'
            f'exec env -i PATH="$PATH" {exports} /bin/sh -c "$last"\n')


class Remote:
    """The fixture host: its own HOME, profile and short tmux socket dir."""

    def __init__(self, artifacts):
        self.home = artifacts / 'remote-home'
        self.home.mkdir()
        self.socket_dir = Path(tempfile.mkdtemp(prefix='amr-', dir='/tmp'))
        (self.socket_dir / f'tmux-{os.getuid()}').mkdir(mode=0o700)
        self.env = {
            'HOME': str(self.home),
            'TMUX_TMPDIR': str(self.socket_dir),
            'SHELL': '/bin/sh',
            'TERM': 'xterm-256color',
            'XDG_CONFIG_HOME': str(self.home / '.config'),
            'XDG_DATA_HOME': str(self.home / '.local/share'),
            'XDG_STATE_HOME': str(self.home / '.local/state'),
        }

    def profile(self):
        return profile_dir(SimpleNamespace(home=self.home))

    def socket(self):
        return self.socket_dir / f'tmux-{os.getuid()}' / 'agentmgr'

    def run(self, sandbox, args, check=True):
        saved = sandbox.env
        sandbox.env = dict(self.env, PATH=saved['PATH'])
        try:
            return sandbox.run(args, check=check)
        finally:
            sandbox.env = saved

    def tmux(self, sandbox, *args):
        return self.run(sandbox, ['tmux', '-S', str(self.socket()), *args], check=False)

    def sessions(self):
        db = self.profile() / 'state.db'
        if not db.exists():
            return []
        conn = sqlite3.connect(str(db), timeout=5)
        try:
            return conn.execute('SELECT id, name, status FROM sessions').fetchall()
        finally:
            conn.close()

    def inbox(self):
        conn = sqlite3.connect(str(self.profile() / 'state.db'), timeout=5)
        try:
            return conn.execute('SELECT sender_name, body FROM session_inbox').fetchall()
        finally:
            conn.close()


def write_fixtures(sandbox, remote, binary):
    bin_dir = sandbox.home / 'bin'
    bin_dir.mkdir()
    # A copy at a path unique to this run, so its processes are found by PID.
    manager = bin_dir / 'agent-manager'
    shutil.copy2(binary, manager)
    (bin_dir / FAKE_BIN).write_text(fake_cli_script(READY_MARKER))
    (bin_dir / 'ssh').write_text(fake_ssh_script(remote.env))
    for path in (bin_dir / FAKE_BIN, bin_dir / 'ssh'):
        path.chmod(0o755)
    for profile in (profile_dir(sandbox), remote.profile()):
        profile.mkdir(parents=True)
        (profile / 'config.toml').write_text(config_fixture_text())
    sandbox.env['PATH'] = str(bin_dir) + ':' + sandbox.env.get('PATH', '')
    return manager


def seed_remote_agent(sandbox, remote, manager):
    workdir = remote.home / 'work'
    workdir.mkdir()
    remote.run(sandbox, [str(manager), 'spawn', '--json', '--name=' + REMOTE_AGENT,
                         '--tool=' + FAKE_CLI, '--directory=' + str(workdir)])
    rows = [row for row in remote.sessions() if row[1] == REMOTE_AGENT]
    if not rows:
        raise AssertionError(f'remote spawn left no row: {remote.sessions()}')
    pane = 'am_' + rows[0][0]
    sandbox.wait('remote-agent-ready', lambda: remote.tmux(sandbox, 'capture-pane', '-p', '-t', pane).stdout,
                 lambda text: READY_MARKER in text)
    return rows[0][0]


def select_remote_agent(sandbox):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        key(sandbox, 'j')
        time.sleep(0.4)  # render settle; the capture right after a keypress can be stale
        if f'{REMOTE_AGENT}  on {CONNECTION}' in preview_of(capture(sandbox)):
            (sandbox.artifacts / 'remote-agent-selected.txt').write_text(capture(sandbox))
            return
    raise AssertionError('remote agent not reachable in the list')


def scenario(sandbox, remote, binary):
    manager = write_fixtures(sandbox, remote, binary)
    agent_id = seed_remote_agent(sandbox, remote, manager)
    sandbox.start_manager('scen', sandbox.home, manager)
    frame(sandbox, 'startup', 'A G E N T')
    key(sandbox, 'Escape')

    key(sandbox, 'C')
    frame(sandbox, 'connection-dialog', 'New SSH Connection')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', CONNECTION)
    key(sandbox, 'Tab')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'fixture@' + CONNECTION)
    frame(sandbox, 'connection-typed', 'fixture@' + CONNECTION)
    key(sandbox, 'Enter')
    frame(sandbox, 'connection-saved', 'ssh', 'New SSH Connection')
    conn = sqlite3.connect(str(profile_dir(sandbox) / 'state.db'), timeout=5)
    try:
        stored = conn.execute('SELECT name, destination FROM connections').fetchall()
    finally:
        conn.close()
    if stored != [(CONNECTION, 'fixture@' + CONNECTION)]:
        raise AssertionError(f'connection not stored: {stored}')
    frame(sandbox, 'remote-row', REMOTE_AGENT)

    select_remote_agent(sandbox)
    key(sandbox, 'Space')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', QUICK_TEXT)
    frame(sandbox, 'quick-typed', QUICK_TEXT)
    key(sandbox, 'Enter')
    sandbox.wait('quick-sent', lambda: capture(sandbox),
                 lambda value: f'sent to {REMOTE_AGENT}' in value or f'queued for {REMOTE_AGENT}' in value)
    sandbox.wait('remote-inbox', lambda: json.dumps(remote.inbox()),
                 lambda value: any(QUICK_TEXT in body for _, body in json.loads(value)))
    # send --from takes 64 bytes; hostnames are ASCII.
    sender = socket.gethostname().split('.')[0][:59] + ': you'
    senders = [name for name, body in remote.inbox() if QUICK_TEXT in body]
    if senders != [sender]:
        raise AssertionError(f'senders {senders}, want [{sender!r}]')

    key(sandbox, 'Escape')
    frame(sandbox, 'quick-closed', f'{REMOTE_AGENT}  on {CONNECTION}', 'Quick prompt mode')
    key(sandbox, 'x')
    frame(sandbox, 'remote-killed', f'killed {REMOTE_AGENT} on {CONNECTION}')
    sandbox.wait('remote-pane-gone', lambda: str(remote.tmux(sandbox, 'has-session', '-t', 'am_' + agent_id).returncode),
                 lambda code: code != '0')
    local = sqlite3.connect(str(profile_dir(sandbox) / 'state.db'), timeout=5)
    try:
        if local.execute('SELECT count(*) FROM sessions').fetchone()[0]:
            raise AssertionError('a remote action wrote a local session')
    finally:
        local.close()
    key(sandbox, 'C-c')
    sandbox.wait_manager_exit('scen:0.0')
    return manager


def server_pid(sandbox, socket_path):
    result = sandbox.run(['tmux', '-S', str(socket_path), 'display-message', '-p', '#{pid}'], check=False)
    return int(result.stdout.strip()) if result.returncode == 0 and result.stdout.strip().isdigit() else None


def binary_processes(manager):
    """The copy's processes still running: the remote serve, at least."""
    listing = subprocess.run(['ps', '-axo', 'pid=,command='], text=True, capture_output=True, check=True).stdout
    found = {}
    for line in listing.splitlines():
        pid, _, command = line.strip().partition(' ')
        if command.startswith(str(manager) + ' ') or command == str(manager):
            found[int(pid)] = command
    return found


def end_pids(pids, timeout=5):
    """SIGTERM each PID, then SIGKILL whatever outlives the deadline."""
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    deadline = time.monotonic() + timeout
    alive = list(pids)
    while alive and time.monotonic() < deadline:
        time.sleep(0.05)
        alive = [pid for pid in alive if _alive(pid)]
    for pid in alive:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    return [pid for pid in alive if _alive(pid)]


def _alive(pid):
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    return True


def cleanup(sandbox, remote, manager):
    """Ends the remote serve, the manager copies and the three tmux servers
    this run started, each by PID; no server is told to kill itself."""
    ended = binary_processes(manager) if manager else {}
    for socket_path in (remote.socket(),
                        sandbox.socket_dir / f'tmux-{os.getuid()}' / 'agentmgr',
                        sandbox.socket_dir / f'tmux-{os.getuid()}' / 'e2e-outer'):
        pid = server_pid(sandbox, socket_path)
        if pid:
            ended[pid] = 'tmux server ' + str(socket_path)
    (sandbox.artifacts / 'ended-processes.json').write_text(json.dumps(ended, indent=2) + '\n')
    stuck = end_pids(list(ended))
    if stuck:
        return [f'processes still running: {stuck}']
    for directory in (remote.socket_dir, sandbox.socket_dir):
        shutil.rmtree(directory, ignore_errors=True)
    return []


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, help='existing binary; otherwise build once before isolating HOME')
    parser.add_argument('--artifacts', type=Path, help='new output directory (always retained)')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    artifacts = (args.artifacts or Path(tempfile.gettempdir()) / f'am-ssh-{time.time_ns()}').resolve()
    started = time.monotonic()
    sandbox = Sandbox(artifacts)
    remote = Remote(artifacts)
    manager = None
    try:
        binary = ensure_binary(root, artifacts, args.binary)
        manager = sandbox.home / 'bin' / 'agent-manager'
        scenario(sandbox, remote, binary)
        result = dict(status='passed', seconds=round(time.monotonic()-started, 2))
    except (Exception, KeyboardInterrupt) as error:
        (sandbox.artifacts / 'scenario-last-frame.txt').write_text(capture(sandbox))
        result = dict(status='failed', error=str(error) or type(error).__name__,
                      seconds=round(time.monotonic()-started, 2))
    finally:
        cleanup_errors = cleanup(sandbox, remote, manager)
    if cleanup_errors:
        result.update(status='failed', cleanup_errors=cleanup_errors)
    (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(artifacts))))
    return 0 if result['status'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())

#!/usr/bin/env python3
"""SSH connection scenario against the real binary; Python standard library only.

A fixture `ssh` first on PATH drops every argument but the remote command and
runs it with /bin/sh under a second disposable profile, so the manager talks
to a real agent-manager "host" without a network or the user's ~/.ssh. The
scenario adds a connection through the dialog and drives every action a
remote row offers: preview, quick send, kill, revive, archive, restore,
attach, quick spawn and a new terminal. A second connection whose host
refuses stays offline and is removed. Every process it started is ended by PID.
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
                       ensure_binary, fake_cli_script, frame, key, preview_of, profile_dir, seed_store)
from smoke import Sandbox

CONNECTION = 'box'
DOWN = 'down'
REMOTE_AGENT = 'remote-agent'
REMOTE_GROUP = 'web'
GROUPED_AGENT = 'api'
QUICK_TEXT = 'ssh-quick-input'
SPAWN_TEXT = 'ssh-spawn-prompt'


def fake_ssh_script(remote_env):
    exports = ' '.join(f'{name}={shlex.quote(value)}' for name, value in remote_env.items())
    return ('#!/bin/sh\n'
            '# disposable fixture ssh: runs the remote command here, under a second profile\n'
            f'case " $* " in *" fixture@{DOWN} "*)\n'
        f'  echo "ssh: connect to host {DOWN} port 22: Connection refused" >&2; exit 255;;\n'
        'esac\n'
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
        return self.query('SELECT id, name, status FROM sessions')

    def query(self, sql):
        db = self.profile() / 'state.db'
        if not db.exists():
            return []
        conn = sqlite3.connect(str(db), timeout=5)
        try:
            return conn.execute(sql).fetchall()
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


def remote_pane_code(sandbox, remote, session_id):
    return str(remote.tmux(sandbox, 'has-session', '-t', 'am_' + session_id).returncode)


def seed_remote(sandbox, remote, manager):
    """The host's own fleet: an agent in its root and one in a group."""
    workdir = remote.home / 'work'
    workdir.mkdir()
    remote.run(sandbox, [str(manager), 'create-group', '--json', '--', REMOTE_GROUP])
    for name, group in ((REMOTE_AGENT, ''), (GROUPED_AGENT, REMOTE_GROUP)):
        remote.run(sandbox, [str(manager), 'spawn', '--json', '--name=' + name, '--tool=' + FAKE_CLI,
                             '--directory=' + str(workdir)] + (['--group=' + group] if group else []))
    ids = {name: session_id for session_id, name, _ in remote.sessions()}
    for name in (REMOTE_AGENT, GROUPED_AGENT):
        if name not in ids:
            raise AssertionError(f'remote spawn left no {name} row: {remote.sessions()}')
        pane = 'am_' + ids[name]
        sandbox.wait(name + '-ready', lambda: remote.tmux(sandbox, 'capture-pane', '-p', '-t', pane).stdout,
                     lambda text: READY_MARKER in text)
    return ids[REMOTE_AGENT]


def select(sandbox, name, needle):
    """Moves the cursor until the preview shows needle: down first, then up."""
    for step in ['j'] * 12 + ['k'] * 24:
        time.sleep(0.4)  # render settle; the capture right after a keypress can be stale
        if needle in preview_of(capture(sandbox)):
            (sandbox.artifacts / (name + '-selected.txt')).write_text(capture(sandbox))
            return
        key(sandbox, step)
    raise AssertionError(f'{name} not reachable in the list')


def select_remote_agent(sandbox):
    select(sandbox, 'remote-agent', f'{REMOTE_AGENT}  on {CONNECTION}')


def add_connection(sandbox, name):
    key(sandbox, 'C')
    frame(sandbox, name + '-dialog', 'New SSH Connection')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', name)
    key(sandbox, 'Tab')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'fixture@' + name)
    frame(sandbox, name + '-typed', 'fixture@' + name)
    key(sandbox, 'Enter')
    frame(sandbox, name + '-saved', 'ssh', 'New SSH Connection')


def stored_connections(sandbox):
    conn = sqlite3.connect(str(profile_dir(sandbox) / 'state.db'), timeout=5)
    try:
        return conn.execute('SELECT name, destination FROM connections ORDER BY name').fetchall()
    finally:
        conn.close()


def confirm(sandbox, name, question, done):
    frame(sandbox, name + '-confirm', question)
    key(sandbox, 'y')
    frame(sandbox, name + '-done', done)


def lifecycle(sandbox, remote, agent_id):
    """Kill, revive, archive and restore, each confirmed, each landing on the host."""
    key(sandbox, 'x')
    confirm(sandbox, 'remote-kill', f'kill {REMOTE_AGENT} on {CONNECTION}?', f'killed {REMOTE_AGENT} on {CONNECTION}')
    sandbox.wait('remote-pane-gone', lambda: remote_pane_code(sandbox, remote, agent_id), lambda code: code != '0')
    frame(sandbox, 'remote-dead-row', '✕ dead')

    key(sandbox, 'v')
    confirm(sandbox, 'remote-revive', f'revive {REMOTE_AGENT} on {CONNECTION}?',
            f'revived {REMOTE_AGENT} on {CONNECTION}')
    sandbox.wait('remote-pane-back', lambda: remote_pane_code(sandbox, remote, agent_id), lambda code: code == '0')

    key(sandbox, 'a')
    confirm(sandbox, 'remote-archive', f'archive {REMOTE_AGENT} on {CONNECTION}?',
            f'archived {REMOTE_AGENT} on {CONNECTION}')
    sandbox.wait('remote-archived', lambda: str(remote.query(f"SELECT archived FROM sessions WHERE id = '{agent_id}'")),
                 lambda value: value == '[(1,)]')
    key(sandbox, 't')
    frame(sandbox, 'archived-view', 'ARCHIVED')
    select(sandbox, 'archived-agent', f'{REMOTE_AGENT}  on {CONNECTION}')
    key(sandbox, 'u')
    confirm(sandbox, 'remote-restore', f'restore {REMOTE_AGENT} on {CONNECTION}?',
            f'restored {REMOTE_AGENT} on {CONNECTION}')
    sandbox.wait('remote-restored', lambda: str(remote.query(f"SELECT archived FROM sessions WHERE id = '{agent_id}'")),
                 lambda value: value == '[(0,)]')
    key(sandbox, 't')
    frame(sandbox, 'active-view', f'{CONNECTION}  ssh', 'ARCHIVED')


def attach(sandbox, remote, agent_id):
    """Attach runs ssh -t in this terminal; the remote pane fills it until detach."""
    select_remote_agent(sandbox)
    sandbox.wait('remote-pane-ready', lambda: remote_pane_code(sandbox, remote, agent_id), lambda code: code == '0')
    key(sandbox, 'A')
    frame(sandbox, 'remote-attached', READY_MARKER, 'A G E N T')
    key(sandbox, 'C-q')
    frame(sandbox, 'remote-detached', f'{REMOTE_AGENT}  on {CONNECTION}')


def spawn_and_terminal(sandbox, remote):
    """Quick prompt on the connection row starts an agent there; T opens a terminal there."""
    select(sandbox, 'connection-row', 'ssh to fixture@' + CONNECTION)
    before = len(remote.sessions())
    key(sandbox, 'Space')
    frame(sandbox, 'quick-spawn-open', f'root on {CONNECTION}')
    frame(sandbox, 'quick-spawn-tool', FAKE_CLI)
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', SPAWN_TEXT)
    key(sandbox, 'Enter')
    frame(sandbox, 'quick-spawned', f'started {FAKE_CLI}')
    rows = remote.query(f"SELECT name, launch_prompt FROM sessions WHERE launch_prompt LIKE '%{SPAWN_TEXT}%'")
    if len(remote.sessions()) != before + 1 or len(rows) != 1:
        raise AssertionError(f'quick spawn: want one new remote session with the prompt, got {remote.sessions()}')
    key(sandbox, 'Escape')

    select(sandbox, 'connection-row-again', 'ssh to fixture@' + CONNECTION)
    key(sandbox, 'T')
    frame(sandbox, 'remote-terminal', 'opened terminal ')
    terminals = remote.query("SELECT id FROM sessions WHERE tool = 'terminal' AND opened_without_session = 1")
    if len(terminals) != 1:
        raise AssertionError(f'want one remote terminal opened without a session, got {terminals}')
    frame(sandbox, 'remote-terminal-row', '1 terminal ·')


def offline_connection(sandbox):
    """A host ssh cannot reach shows offline with ssh's error, refuses actions, and comes out."""
    add_connection(sandbox, DOWN)
    select(sandbox, 'offline-row', 'ssh to fixture@' + DOWN)
    frame(sandbox, 'offline', 'Connection refused')
    if 'offline' not in preview_of(capture(sandbox)):
        raise AssertionError('unreachable host not marked offline')
    key(sandbox, 'T')
    frame(sandbox, 'offline-refusal', f'{DOWN} is offline')
    key(sandbox, 'd')
    confirm(sandbox, 'remove-connection', f'remove connection {DOWN}?', CONNECTION)
    sandbox.wait('connection-removed', lambda: str(stored_connections(sandbox)),
                 lambda value: value == str([(CONNECTION, 'fixture@' + CONNECTION)]))
    frame(sandbox, 'offline-row-gone', CONNECTION, 'fixture@' + DOWN)


def scenario(sandbox, remote, binary):
    manager = write_fixtures(sandbox, remote, binary)
    agent_id = seed_remote(sandbox, remote, manager)
    sandbox.start_manager('scen', sandbox.home, manager)
    frame(sandbox, 'startup', 'A G E N T')
    key(sandbox, 'Escape')
    seed_store(sandbox)

    add_connection(sandbox, CONNECTION)
    if stored_connections(sandbox) != [(CONNECTION, 'fixture@' + CONNECTION)]:
        raise AssertionError(f'connection not stored: {stored_connections(sandbox)}')
    frame(sandbox, 'remote-row', REMOTE_AGENT)
    frame(sandbox, 'remote-group', GROUPED_AGENT)

    select_remote_agent(sandbox)
    frame(sandbox, 'remote-preview', READY_MARKER)
    key(sandbox, 'r')
    frame(sandbox, 'remote-refusal', "rename isn't available on SSH connections yet")
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
    lifecycle(sandbox, remote, agent_id)
    attach(sandbox, remote, agent_id)
    spawn_and_terminal(sandbox, remote)
    offline_connection(sandbox)
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

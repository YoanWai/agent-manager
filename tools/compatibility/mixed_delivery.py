#!/usr/bin/env python3
"""A released manager and the candidate deliver on one profile, one server.

The released binary predates the delivery guard. It keeps typing queued
messages and pending input while the candidate runs beside it, and the
candidate takes delivery over only once the released manager has quit.
Every body is counted in the worker's scrollback exactly once.
"""
import argparse
import json
import os
from pathlib import Path
import shlex
import sqlite3
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'e2e'))
from smoke import Sandbox
from scenarios import profile_dir, config_fixture_text
from matrix import released_binary

CALLER = 'beefcafe'
TOOL = 'command-code'
FENCES = ('session_inbox_delivery_claim_fence', 'session_inbox_delivery_receipt_fence',
          'session_pending_delivery_claim_fence', 'session_pending_delivery_receipt_fence')


def fixture_cli():
    # The manager registers its MCP server through the CLI's own "mcp"
    # subcommand before launch, so that path exits at once. The agent draws
    # a prompt after every line it reads, the way the suite's ready-tool
    # does, so a long envelope never scrolls the prompt off the pane.
    return ('#!/bin/sh\n'
            '# disposable fixture agent CLI; never a real install\n'
            'if [ "$1" = "mcp" ]; then exit 0; fi\n'
            'printf "fixture-cli ready\\n\\342\\235\\257 "\n'
            'while IFS= read -r line; do printf "\\n\\342\\235\\257 "; done\n')


class Gate:
    def __init__(self, work, candidate, release):
        self.work = work
        self.sandbox = Sandbox(work / 'artifacts')
        self.binaries = dict(candidate=candidate, released=release)
        self.managers = {}
        self.checks = []
        self.bodies = []
        bin_dir = self.sandbox.home / 'bin'
        bin_dir.mkdir()
        fake = bin_dir / 'cmd'
        fake.write_text(fixture_cli())
        fake.chmod(0o755)
        self.sandbox.env['PATH'] = str(bin_dir) + os.pathsep + self.sandbox.env['PATH']
        config = profile_dir(self.sandbox)
        config.mkdir(parents=True)
        (config / 'config.toml').write_text(config_fixture_text())
        self.db = config / 'state.db'

    def cli(self, which, *args, caller=CALLER):
        env = dict(self.sandbox.env, AGENT_MANAGER_SESSION_ID=caller)
        result = subprocess.run([str(self.binaries[which]), *args], cwd=self.sandbox.home, env=env,
                                text=True, capture_output=True, timeout=30)
        with (self.work / 'commands.jsonl').open('a') as log:
            log.write(json.dumps(dict(binary=which, args=args, code=result.returncode,
                                      stdout=result.stdout, stderr=result.stderr)) + '\n')
        if result.returncode:
            raise RuntimeError(f'{which} {args}: exit {result.returncode}: {result.stderr[-800:]}')
        return result.stdout

    def sql(self, query, *params):
        with sqlite3.connect(f'file:{self.db}', uri=True, timeout=5) as conn:
            return conn.execute(query, params).fetchall()

    def setting(self, key):
        rows = self.sql('SELECT value FROM settings WHERE key = ?', key)
        return rows[0][0] if rows else ''

    def receipt(self, table, token_column, outcome_column, row_id):
        # The released binary created the profile, so the receipt columns
        # only exist once the candidate has opened the store and migrated.
        present = {row[1] for row in self.sql(f'PRAGMA table_info({table})')}
        if token_column not in present or outcome_column not in present:
            return '', ''
        return self.sql(f'SELECT {token_column}, {outcome_column} FROM {table} WHERE id = ?', row_id)[0]

    def wait(self, name, observe, predicate, timeout=20):
        return self.sandbox.wait(name, observe, predicate, timeout)

    def start_manager(self, which):
        binary = self.binaries[which]
        stderr = self.work / f'manager-{which}-stderr.txt'
        exit_file = self.work / f'manager-{which}-exit.txt'
        self.sandbox.tmux('new-session', '-d', '-s', which, '-x', '110', '-y', '30', '-c', str(self.sandbox.home),
                          f'env TERM=xterm-256color COLORTERM=truecolor NO_COLOR=1 {shlex.quote(str(binary))} '
                          f'2>{shlex.quote(str(stderr))}; am_status=$?; printf "%s\\n" "$am_status" '
                          f'>{shlex.quote(str(exit_file))}; exit "$am_status"')
        self.sandbox.tmux('set-option', '-w', '-t', which, 'remain-on-exit', 'on')
        self.managers[which] = exit_file
        # A first run opens the messages panel over the banner; Escape
        # closes it either way, and the heartbeat waits prove the poller.
        self.wait(f'{which}-manager-frame',
                  lambda: self.sandbox.tmux('capture-pane', '-p', '-t', f'{which}:0.0').stdout,
                  lambda frame: 'A G E N T' in frame or 'messages' in frame)
        self.sandbox.tmux('send-keys', '-t', f'{which}:0.0', 'Escape')

    def manager_frame(self, which, name):
        frame = self.sandbox.tmux('capture-pane', '-p', '-t', f'{which}:0.0', check=False).stdout
        (self.work / f'manager-{which}-{name}.txt').write_text(frame)
        return frame

    def quit_manager(self, which):
        # The notices panel swallows "q" and ctrl+c on both binaries and
        # reopens when the feed loads, so Escape closes it first; ctrl+c then
        # quits from the list, as the quit-drain scenarios rely on.
        exit_file = self.managers.pop(which)
        self.manager_frame(which, 'before-quit')
        self.sandbox.tmux('send-keys', '-t', f'{which}:0.0', 'Escape')
        self.sandbox.tmux('send-keys', '-t', f'{which}:0.0', 'C-c')
        try:
            self.wait(f'{which}-manager-exit', lambda: exit_file.read_text().strip() if exit_file.exists() else '',
                      lambda code: code != '')
        except Exception:
            self.manager_frame(which, 'quit-failure')
            raise
        if exit_file.read_text().strip() != '0':
            raise AssertionError(f'{which} manager exited {exit_file.read_text().strip()}')

    def heartbeat_age(self):
        # Observed values are written to the evidence directory, so this is
        # text: seconds since the stamp, or empty before any manager stamped.
        raw = self.setting('poller_heartbeat')
        return f'{time.time() - int(raw) / 1e9:.1f}' if raw else ''

    def pane(self, session_id):
        # The manager names a session's tmux session "am_<id>" and keeps the
        # agent in its first window's first pane (tmux.PaneTarget).
        result = self.sandbox.tmux('capture-pane', '-p', '-S', '-', '-t', f'am_{session_id}:^.0',
                                   socket='agentmgr', check=False)
        return result.stdout

    def spawn_worker(self):
        created = json.loads(self.cli('released', 'spawn', '--tool', TOOL, '--name', 'worker', '--json'))
        session_id = created['id']
        self.wait('worker-ready', lambda: self.pane(session_id), lambda text: 'fixture-cli ready' in text)
        return session_id

    def send(self, which, session_id, body):
        sent = json.loads(self.cli(which, 'send', session_id, body, '--json'))
        self.bodies.append(body)
        return sent['message_id']

    def wait_delivered(self, which, message_id, body, session_id, step, timeout=30):
        def status():
            return json.loads(self.cli(which, 'message-status', str(message_id), '--json'))['state']
        self.wait(f'{step}-delivered', status, lambda state: state == 'delivered', timeout=timeout)
        self.wait(f'{step}-in-pane', lambda: self.pane(session_id), lambda text: body in text)
        return self.receipt('session_inbox', 'attempt_token', 'delivery_outcome', message_id)

    def seed_pending(self, session_id, text):
        self.sql('UPDATE sessions SET pending_inputs = ? WHERE id = ?', json.dumps([text]), session_id)
        self.bodies.append(text)

    def wait_pending_typed(self, session_id, text, step, timeout=30):
        self.wait(f'{step}-consumed', lambda: self.sql('SELECT pending_inputs FROM sessions WHERE id = ?', session_id)[0][0],
                  lambda value: value == '[]', timeout=timeout)
        self.wait(f'{step}-in-pane', lambda: self.pane(session_id), lambda value: text in value)
        return self.receipt('sessions', 'pending_attempt_token', 'pending_delivery_outcome', session_id)

    def assert_once(self, session_id, step):
        text = self.pane(session_id)
        (self.work / f'{step}-scrollback.txt').write_text(text)
        counts = {body: text.count(body) for body in self.bodies}
        if any(count != 1 for count in counts.values()):
            raise AssertionError(f'{step}: bodies typed other than once: {counts}')

    def assert_no_fences(self):
        names = {row[0] for row in self.sql("SELECT name FROM sqlite_master WHERE type = 'trigger'")}
        if names & set(FENCES):
            raise AssertionError(f'fence triggers present: {sorted(names & set(FENCES))}')

    def run(self):
        # The released binary creates the profile, as it does for a user who
        # upgrades: the candidate finds a store it did not open first.
        subprocess.run([str(self.binaries['released']), 'sessions'], cwd=self.sandbox.home, env=self.sandbox.env,
                       capture_output=True, timeout=30)
        if not self.db.exists():
            raise AssertionError('released binary did not create the disposable profile')
        self.sql("INSERT OR IGNORE INTO sessions(id,name,tool,cwd,group_name,status,archived,created_at,last_status_at) "
                 "VALUES(?,'fixture','terminal',?,'','finished',0,?,?)",
                 CALLER, str(self.sandbox.home), int(time.time()), int(time.time()))
        self.start_manager('released')
        self.wait('released-heartbeat', self.heartbeat_age, lambda age: age != '' and float(age) < 30)
        worker = self.spawn_worker()
        first = self.send('released', worker, 'mixed-one released alone')
        token, outcome = self.wait_delivered('released', first, 'mixed-one released alone', worker, 'first')
        if token or outcome:
            raise AssertionError(f'released manager wrote receipt columns: {token!r} {outcome!r}')
        self.checks.append('released manager delivers alone')

        self.start_manager('candidate')
        self.wait('candidate-mirror', lambda: self.setting('delivery_heartbeat'), lambda value: value != '')
        self.wait('candidate-columns', lambda: ','.join(row[1] for row in self.sql('PRAGMA table_info(session_inbox)')),
                  lambda names: 'attempt_token' in names.split(','))
        self.wait('candidate-sees-older', lambda: self.setting('legacy_poller_heartbeat'), lambda value: value != '')
        self.assert_no_fences()
        self.cli('released', 'sessions', '--json')
        self.cli('candidate', 'sessions', '--json')
        self.checks.append('both binaries list sessions on the shared profile')

        second = self.send('candidate', worker, 'mixed-two both running')
        token, outcome = self.wait_delivered('candidate', second, 'mixed-two both running', worker, 'second')
        if token or outcome:
            raise AssertionError(f'candidate delivered beside the released manager: {token!r} {outcome!r}')
        self.seed_pending(worker, 'mixed-pending both running')
        token, outcome = self.wait_pending_typed(worker, 'mixed-pending both running', 'second-pending')
        if token or outcome:
            raise AssertionError(f'candidate typed pending input beside the released manager: {token!r} {outcome!r}')
        self.assert_once(worker, 'both-running')
        self.checks.append('released manager keeps delivering while the candidate runs')

        # The released manager's last stamp ages past PollerHeartbeatStale
        # (30s), and the candidate reads that on its next claim (10s).
        self.quit_manager('released')
        third = self.send('released', worker, 'mixed-three candidate alone')
        token, outcome = self.wait_delivered('released', third, 'mixed-three candidate alone', worker, 'third', timeout=75)
        if not token or outcome != 'confirmed':
            raise AssertionError(f'candidate takeover left no confirmed receipt: {token!r} {outcome!r}')
        self.seed_pending(worker, 'mixed-pending candidate alone')
        token, outcome = self.wait_pending_typed(worker, 'mixed-pending candidate alone', 'third-pending', timeout=75)
        # A finished pending attempt clears its token; the outcome is the receipt.
        if token or outcome != 'confirmed':
            raise AssertionError(f'candidate pending takeover left no confirmed receipt: {token!r} {outcome!r}')
        self.assert_once(worker, 'candidate-alone')
        self.assert_no_fences()
        if json.loads(self.cli('released', 'message-status', str(third), '--json'))['state'] != 'delivered':
            raise AssertionError('released reader does not see the candidate delivery')
        if json.loads(self.cli('candidate', 'message-status', str(first), '--json'))['state'] != 'delivered':
            raise AssertionError('candidate reader does not see the released delivery')
        self.checks.append('candidate takes delivery over after the released manager quits')
        self.quit_manager('candidate')
        return dict(status='passed', checks=self.checks,
                    gaps=['a released manager started beside a running candidate', 'real SSH'])

    def close(self):
        for which in list(self.managers):
            self.sandbox.tmux('kill-session', '-t', which, check=False)
        self.sandbox.close()


def run(candidate, work, tag, peer=None):
    if peer:
        release = peer.resolve()
    else:
        release, _ = released_binary(work, tag)
    gate = Gate(work, candidate, release)
    try:
        return gate.run()
    finally:
        gate.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--release', default='v0.42.0')
    parser.add_argument('--peer-binary', type=Path, help='run an explicitly selected older binary instead of downloading a release')
    parser.add_argument('--artifacts', type=Path)
    args = parser.parse_args()
    work = args.artifacts or Path(tempfile.mkdtemp(prefix='am-mixed-'))
    work.mkdir(parents=True, exist_ok=True)
    started = time.monotonic()
    try:
        result = run(args.binary.resolve(), work.resolve(), args.release, args.peer_binary)
    except Exception as error:
        result = dict(status='failed', error=f'{type(error).__name__}: {error}')
    result['seconds'] = round(time.monotonic() - started, 2)
    (work / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(work))))
    sys.exit(0 if result['status'] == 'passed' else 1)

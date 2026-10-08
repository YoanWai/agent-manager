#!/usr/bin/env python3
"""Deterministic scenario suite against the real binary; Python standard library only.

Shares one binary build per invocation, drives a disposable HOME/profile and a
private tmux socket pair, and asserts each transition on observable frames.
Keep the smoke separate; this suite is the medium-budget scenario gate.
"""
import argparse
import contextlib
import json
import re
import shlex
import sqlite3
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path

from smoke import Sandbox

BUILTIN_TOOLS = ('antigravity', 'claude', 'command-code', 'codex', 'gemini',
                 'grok', 'hermes', 'muse', 'opencode', 'pi', 'terminal')
FAKE_CLI = 'command-code'
FAKE_BIN = 'cmd'
# [tools.*] blocks in config.toml only ignore builtins, so the fixture swap
# must live on PATH, not in the file; hidden_tools keeps the pickers to the
# two fixture tools.
HIDDEN_TOOLS = [name for name in BUILTIN_TOOLS if name != FAKE_CLI]
READY_MARKER = 'fixture-cli ready'
SESSION_NAME = 'fixagent'


def fake_cli_script(marker, delay=0):
    # The manager registers its MCP server through the CLI's own
    # "mcp add" subcommand before launch, so the fixture must exit that
    # path immediately; only the bare agent invocation stays alive.
    lines = [
        '#!/bin/sh',
        '# disposable fixture agent CLI; never a real install',
        'if [ "$1" = "mcp" ]; then exit 0; fi',
    ]
    if delay:
        lines.append(f'sleep {int(delay)}')
    lines.append(f'printf {shlex.quote(marker + chr(10))}')
    lines.append('exec /bin/sleep 3600')
    return '\n'.join(lines) + '\n'


def config_fixture_text():
    # poll_interval shortens the poller so readiness observations stay fast.
    return 'poll_interval = "1s"\n'


def profile_dir(sandbox):
    # os.UserConfigDir: Library/Application Support on macOS, XDG_CONFIG_HOME elsewhere.
    if sys.platform == 'darwin':
        return sandbox.home / 'Library' / 'Application Support' / 'agent-manager'
    return sandbox.home / '.config' / 'agent-manager'


def write_fixture(sandbox, slow_seconds=0):
    bin_dir = sandbox.home / 'bin'
    bin_dir.mkdir()
    fake = bin_dir / FAKE_BIN
    fake.write_text(fake_cli_script(READY_MARKER, slow_seconds))
    fake.chmod(0o755)
    config_dir = profile_dir(sandbox)
    config_dir.mkdir(parents=True)
    (config_dir / 'config.toml').write_text(config_fixture_text())
    sandbox.env['PATH'] = str(bin_dir) + ':' + sandbox.env.get('PATH', '')


def ensure_binary(root, artifacts, given):
    """One build per invocation; reuse an existing binary untouched."""
    if given:
        return Path(given).resolve()
    binary = artifacts / 'agent-manager'
    if not binary.exists():
        with (artifacts / 'build.log').open('w') as log:
            subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=root,
                           stdout=log, stderr=subprocess.STDOUT, check=True)
    return binary


def seed_store(sandbox):
    """Seed picker settings once the manager has created its store.

    hidden_tools lives in the store, not config.toml, so the fixture seeds
    the table a second connection writes to while the manager runs.
    """
    db = profile_dir(sandbox) / 'state.db'
    sandbox.wait('state-db', lambda: str(db) if db.exists() else '', lambda value: value != '')
    conn = sqlite3.connect(str(db), timeout=5)
    try:
        conn.execute("INSERT OR REPLACE INTO settings(key, value) VALUES ('hidden_tools', ?)",
                     (','.join(HIDDEN_TOOLS),))
        conn.execute("INSERT OR REPLACE INTO settings(key, value) VALUES ('default_tool', ?)", (FAKE_CLI,))
        conn.commit()
    finally:
        conn.close()


def start_manager(sandbox, binary, session='scen'):
    sandbox.start_manager(session, sandbox.home, binary)
    frame(sandbox, 'startup', 'A G E N T')
    key(sandbox, 'Escape')


def capture(sandbox):
    return sandbox.tmux('capture-pane', '-p', '-t', 'scen:0.0').stdout


def key(sandbox, text):
    sandbox.tmux('send-keys', '-t', 'scen:0.0', text)


def frame(sandbox, name, present, absent=''):
    return sandbox.wait(name, lambda: capture(sandbox),
                        lambda value: present in value and (not absent or absent not in value))


def preview_of(frame):
    return '\n'.join(line[35:] for line in frame.splitlines())


def select_row(sandbox, name, step):
    """Select the fixture agent by its live-pane marker, excluding group tables."""
    def session_detail(frame):
        return READY_MARKER in preview_of(frame)

    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        key(sandbox, 'j')
        time.sleep(0.4)  # render settle; the capture right after a keypress can be stale
        if session_detail(capture(sandbox)):
            time.sleep(0.4)
            if session_detail(capture(sandbox)):
                (sandbox.artifacts / (step + '-selected.txt')).write_text(capture(sandbox))
                return
    raise AssertionError(f'fixture agent {name} not reachable in the preview')


def agent_panes(sandbox, command, name='pane'):
    value = sandbox.wait(f'{name}-{command[:12]}',
                         lambda: sandbox.tmux('list-panes', '-a', '-F',
                                               '#{pane_id} #{pane_current_command}',
                                               socket='agentmgr', check=False).stdout,
                         lambda value: any(line.split()[-1:] == [command] for line in value.splitlines()))
    return [line.split()[0] for line in value.splitlines() if line.split()[-1:] == [command]]


def agent_pane(sandbox, command, name='pane'):
    return agent_panes(sandbox, command, name)[0]


def pane_text(sandbox, pane):
    return sandbox.tmux('capture-pane', '-p', '-t', pane, socket='agentmgr', check=False).stdout


def store_sessions(sandbox):
    """Read the store directly; the CLI list command needs a managed session env."""
    db = profile_dir(sandbox) / 'state.db'
    sandbox.wait('store-read', lambda: str(db) if db.exists() else '', lambda value: value)
    conn = sqlite3.connect(str(db), timeout=5)
    try:
        rows = conn.execute('select id, name, tool, group_name from sessions').fetchall()
    finally:
        conn.close()
    return [dict(id=i, name=n, tool=t, group=g) for i, n, t, g in rows]


def spawn_form(sandbox, prompt):
    """The n form; the prompt token becomes the row's displayed name."""
    key(sandbox, 'n')
    frame(sandbox, 'form-open', '◆ New Session')
    sandbox.tmux('send-keys','-l','-t','scen:0.0',prompt)
    for _ in range(4):  # name → tool → dir → worktree → prompt
        key(sandbox, 'Tab')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', prompt)
    frame(sandbox, 'form-prompt-typed', prompt)
    key(sandbox, 'Enter')
    sandbox.wait('spawn-row', lambda: json.dumps(store_sessions(sandbox)),
                 lambda value: any(row['name'] == prompt for row in json.loads(value)))
    return prompt


def scenario(sandbox, binary):
    write_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    name = spawn_form(sandbox, SESSION_NAME)
    with sqlite3.connect(profile_dir(sandbox) / 'state.db') as conn:
        conn.execute('UPDATE sessions SET agent_session_id=? WHERE name=?', (str(uuid.uuid4()), name))
    pane = agent_pane(sandbox, 'sleep')
    sandbox.wait('spawn-ready', lambda: pane_text(sandbox, pane), lambda text: READY_MARKER in text)
    select_row(sandbox, name, 'quick-send')
    key(sandbox, 'Space')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'scen-quick-input')
    frame(sandbox, 'quick-prompt-typed', 'scen-quick-input')
    key(sandbox, 'Enter')
    def quick_prompt():
        with contextlib.closing(sqlite3.connect(profile_dir(sandbox) / 'state.db')) as conn:
            row = conn.execute('SELECT last_prompt FROM sessions WHERE name=?', (name,)).fetchone()
        return row[0] if row else ''
    sandbox.wait('quick-prompt-recorded', quick_prompt, lambda value: value == 'scen-quick-input')
    sandbox.wait('quick-prompt-reached-pane', lambda: pane_text(sandbox, pane),
                 lambda text: 'scen-quick-input' in text)
    key(sandbox, 'Escape')
    key(sandbox, 'T')
    frame(sandbox, 'terminal-tab', 'terminal-')
    key(sandbox, 'g')
    frame(sandbox, 'group-form', 'New Group')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'scen-group')
    frame(sandbox, 'group-form-name', 'scen-group')
    key(sandbox, 'Enter')
    frame(sandbox, 'group-created', 'scen-group', 'New Group')
    select_row(sandbox, name, 'moving')
    key(sandbox, 'm')
    frame(sandbox, 'move-open', '⇄ Move')
    key(sandbox, 'Down')
    sandbox.wait('move-group-selected', lambda: capture(sandbox),
                 lambda value: re.search(r'❯\s+scen-group', value) is not None)
    key(sandbox, 'Enter')
    sandbox.wait('move-committed', lambda: json.dumps([row for row in store_sessions(sandbox) if row['name'] == name]),
                 lambda rows: json.loads(rows) and json.loads(rows)[0]['group'] == 'scen-group')
    select_row(sandbox, name, 'renaming')
    key(sandbox, 'r')
    frame(sandbox, 'rename-open', name)
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'X')
    key(sandbox, 'Enter')
    sandbox.wait('rename-committed', lambda: json.dumps(store_sessions(sandbox)),
                 lambda value: any(row['name'] == name + 'X' for row in json.loads(value)))
    sandbox.wait('rename-closed', lambda: capture(sandbox), lambda value: 'Rename' not in value)
    renamed = [row for row in store_sessions(sandbox) if row['name'] == name + 'X']
    assert renamed and renamed[0]['group'] == 'scen-group', f'rename not persisted in group: {renamed}'
    key(sandbox, 's')
    frame(sandbox, 'settings-open', 'compact')
    # default tool, theme, theme follows OS and background sit above density.
    for _ in range(4):
        key(sandbox, 'Down')
    key(sandbox, 'Right')
    sandbox.wait('settings-density-cycled', lambda: capture(sandbox), lambda value: 'comfortable' in value)
    key(sandbox, 'Escape')
    def density():
        with sqlite3.connect(profile_dir(sandbox) / 'state.db') as conn:
            row = conn.execute("SELECT value FROM settings WHERE key='list_density'").fetchone()
        return row[0] if row else ''
    sandbox.wait('settings-persisted', density, lambda value: value == 'comfortable')
    key(sandbox, 's')
    frame(sandbox, 'settings-reopened', 'comfortable')
    key(sandbox, 'Escape')
    select_row(sandbox, name + 'X', 'forking')
    key(sandbox, 'f')
    frame(sandbox, 'fork-open', name + 'X-fork')
    key(sandbox, 'Enter')
    sandbox.wait('fork-row', lambda: json.dumps(store_sessions(sandbox)),
                 lambda value: any(row['name'] == name + 'X-fork' for row in json.loads(value)))
    sandbox.wait('fork-closed', lambda: capture(sandbox), lambda value: 'Fork Session' not in value and '◆ Fork' not in value)
    fork_pane = agent_panes(sandbox, 'sleep', 'fork-pane')[-1]
    sandbox.wait('fork-pane-ready', lambda: pane_text(sandbox, fork_pane),
                 lambda text: READY_MARKER in text)
    # remain-on-exit exposes the process exit code.
    key(sandbox, 'C-c')
    sandbox.wait_manager_exit('scen:0.0')


def quit_drain(sandbox, binary):
    write_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    key(sandbox, 'n')
    frame(sandbox, 'drain-form', '◆ New Session')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', 'drained-agent')
    frame(sandbox, 'drain-name', 'drained-agent')
    lock = sqlite3.connect(profile_dir(sandbox) / 'state.db', timeout=1)
    lock.execute('BEGIN IMMEDIATE')
    try:
        key(sandbox, 'Enter')
        key(sandbox, 'Enter')
        frame(sandbox, 'drain-accepted', 'already in progress')
        key(sandbox, 'C-c')
        state = sandbox.tmux('display-message', '-p', '-t', 'scen:0.0', '#{pane_dead}').stdout.strip()
        if state != '0':
            raise AssertionError('manager exited before accepted write could commit')
    finally:
        lock.rollback()
        lock.close()
    sandbox.wait('drain-committed', lambda: json.dumps(store_sessions(sandbox)),
                 lambda value: any(row['name'] == 'drained-agent' for row in json.loads(value)))
    sandbox.wait_manager_exit('scen:0.0', 'drain-exit')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, help='existing binary; otherwise build once before isolating HOME')
    parser.add_argument('--artifacts', type=Path, help='new output directory (always retained)')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    artifacts = (args.artifacts or Path(tempfile.gettempdir()) / f'am-scenarios-{time.time_ns()}').resolve()
    started = time.monotonic()
    sandbox = Sandbox(artifacts)
    try:
        binary = ensure_binary(root, artifacts, args.binary)
        scenario(sandbox, binary)
        cleanup_errors = sandbox.close()
        if cleanup_errors:
            raise RuntimeError(str(cleanup_errors))
        sandbox = Sandbox(artifacts / 'quit-drain')
        quit_drain(sandbox, binary)
        result = dict(status='passed', seconds=round(time.monotonic()-started, 2))
    except (Exception, KeyboardInterrupt) as error:
        (sandbox.artifacts / 'scenario-last-frame.txt').write_text(capture(sandbox))
        result = dict(status='failed', error=str(error) or type(error).__name__,
                      capture_errors=sandbox.failure_frames(), seconds=round(time.monotonic()-started, 2))
    finally:
        cleanup_errors = sandbox.close()
    if cleanup_errors:
        result.update(status='failed', cleanup_errors=cleanup_errors, socket_dir=str(sandbox.socket_dir))
    (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(artifacts))))
    return 0 if result['status'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())

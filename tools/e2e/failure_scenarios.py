#!/usr/bin/env python3
"""Disposable terminal checks for partial settings writes and install failure/retry."""
import argparse
import json
import sqlite3
import shlex
import shutil
import time
from pathlib import Path

from smoke import Sandbox
from scenarios import (READY_MARKER,
                       agent_panes, config_fixture_text, ensure_binary,
                       fake_cli_script, capture, key, pane_text, profile_dir,
                       seed_store, start_manager, store_sessions, write_fixture)


def write_install_fixture(sandbox):
    """Fixture PATH holds a fake npm that 'installs' the command-code CLI.

    The real `cmd` is absent until the fake npm runs, so the first spawn is
    refused with a missing-tool hint; the installer command the dialog offers
    is the fixture npm, which puts `cmd` on PATH exactly like an installer
    would.
    """
    bin_dir = sandbox.home / 'bin'
    bin_dir.mkdir()
    cmd_script = fake_cli_script(READY_MARKER)
    npm = bin_dir / 'npm'
    npm.write_text('#!/bin/sh\n'
                   '# disposable fixture npm; simulates only the command-code install\n'
                   'case "$*" in\n'
                   '  *install*command-code*)\n'
                   f"    cat > {shlex.quote(str(bin_dir / 'cmd'))} <<'__CMD__'\n"
                   f'{cmd_script}__CMD__\n'
                   f"    chmod 755 {shlex.quote(str(bin_dir / 'cmd'))}\n"
                   '    ;;\n'
                   'esac\n'
                   'exit 0\n')
    npm.chmod(0o755)
    config_dir = profile_dir(sandbox)
    config_dir.mkdir(parents=True)
    (config_dir / 'config.toml').write_text(config_fixture_text())
    # Only runtime helpers are discoverable before the fixture installs cmd.
    for name in ('tmux', 'git', 'sh', 'bash', 'env', 'mkdir', 'cat', 'chmod',
                 'dirname', 'basename', 'sed', 'tr', 'awk', 'grep', 'sleep',
                 'rm', 'which', 'uname', 'ps', 'touch', 'date', 'head', 'tail'):
        helper = shutil.which(name)
        if helper:
            (bin_dir / name).symlink_to(helper)
    sandbox.env['PATH'] = str(bin_dir)


def setting_value(sandbox, key):
    """Read one settings row from the store; empty when absent."""
    db = profile_dir(sandbox) / 'state.db'
    sandbox.wait('settings-read', lambda: str(db) if db.exists() else '',
                 lambda value: value != '')
    conn = sqlite3.connect(str(db), timeout=5)
    try:
        row = conn.execute('SELECT value FROM settings WHERE key = ?', (key,)).fetchone()
    finally:
        conn.close()
    return row[0] if row else ''


def session_rows(sandbox, name):
    return [row for row in store_sessions(sandbox) if row['name'] == name]


def frame(sandbox, name, present, absent='', timeout=12):
    return sandbox.wait(name, lambda: capture(sandbox),
                        lambda value: present in value and (not absent or absent not in value),
                        timeout=timeout)


def spawn_form_refused(sandbox, name):
    """Submit the new-session form; the caller asserts how the spawn is answered."""
    key(sandbox, 'n')
    frame(sandbox, 'form-open', 'New Session')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', name)
    for _ in range(4):  # name → tool → dir → worktree → prompt
        key(sandbox, 'Tab')
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', name)
    frame(sandbox, 'form-prompt-typed', name)
    key(sandbox, 'Enter')


def settings_partial_save(sandbox, binary):
    write_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    db = profile_dir(sandbox) / 'state.db'
    with sqlite3.connect(db) as conn:
        conn.execute("CREATE TRIGGER fail_layout BEFORE INSERT ON settings WHEN NEW.key='layout' BEGIN SELECT RAISE(ABORT, 'fixture layout failure'); END")
    key(sandbox, 's')
    frame(sandbox, 'settings-open', 'compact')
    # default tool, theme, theme follows OS and background sit above density.
    for _ in range(4):
        key(sandbox, 'Down')
    key(sandbox, 'Right')
    frame(sandbox, 'settings-density-cycled', 'comfortable')
    key(sandbox, 'Down')
    key(sandbox, 'Right')
    for _ in range(5):
        key(sandbox, 'Down')
    key(sandbox, 'Right')
    key(sandbox, 'Enter')
    frame(sandbox, 'save-failed', 'committed 8 of')
    assert setting_value(sandbox, 'list_density') == 'comfortable', 'earlier density write was lost'
    assert setting_value(sandbox, 'focus_key') == 'attach', 'earlier focus preference was lost'
    assert setting_value(sandbox, 'layout') == '', 'failed layout write unexpectedly committed'
    key(sandbox, 's')
    restored = frame(sandbox, 'settings-reopened', 'comfortable')
    layout_line = next(line for line in restored.splitlines() if 'sessions layout' in line)
    focus_line = next(line for line in restored.splitlines() if 'session keys' in line)
    assert 'split' in layout_line, 'failed optimistic layout was not restored'
    assert '↵ attach' in focus_line, 'committed focus preference was not reconciled'
    with sqlite3.connect(db) as conn:
        conn.execute('DROP TRIGGER fail_layout')
    key(sandbox, 'Enter')
    sandbox.wait('settings-retry', lambda: setting_value(sandbox, 'layout'), lambda value: value == 'split')


def install_script_write_failure(sandbox, binary):
    write_install_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    spawn_form_refused(sandbox, 'install-agent')
    frame(sandbox, 'launch-hint', 'not installed', 'New Session')
    frame(sandbox, 'hint-offers-install', 'npm install -g command-code')
    hooks = profile_dir(sandbox) / 'hooks'
    assert not hooks.exists(), 'fixture unexpectedly initialized hooks before install'
    hooks.write_text('fixture path obstruction')
    try:
        key(sandbox, 'i')
        failed = frame(sandbox, 'install-script-error', '⚠ mkdir')
        assert 'Session needs a setup step' in failed, 'failure closed the setup dialog'
        assert not session_rows(sandbox, 'install-cmd'), 'refused install left a durable row'
    finally:
        hooks.unlink()
    key(sandbox, 'Escape')
    frame(sandbox, 'hint-closed', 'A G E N T', 'Session needs a setup step')


def install_retry_creates_one_session(sandbox, binary):
    write_install_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    spawn_form_refused(sandbox, 'install-agent')
    frame(sandbox, 'launch-hint', 'not installed', 'New Session')
    assert not session_rows(sandbox, 'install-agent'), \
        'a refused spawn must not leave an agent row behind'
    key(sandbox, 'i')
    sandbox.wait('install-row', lambda: json.dumps(session_rows(sandbox, 'install-cmd')),
                 lambda value: json.loads(value))
    sandbox.wait('retry-row', lambda: json.dumps(session_rows(sandbox, 'install-agent')),
                 lambda value: json.loads(value))
    panes = agent_panes(sandbox, 'sleep')
    assert len(panes) == 1, f'one retried agent pane expected, got {panes}'
    sandbox.wait('retry-pane-ready', lambda: pane_text(sandbox, panes[0]),
                 lambda text: READY_MARKER in text)
    assert len(session_rows(sandbox, 'install-agent')) == 1, \
        'the retried spawn must not add a second agent row'
    assert len(session_rows(sandbox, 'install-cmd')) == 1, \
        'the install row must survive the retry exactly once'
    frame(sandbox, 'no-hint-left', 'A G E N T', 'Session needs a setup step')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, help='existing binary; otherwise build once before isolating HOME')
    parser.add_argument('--artifacts', type=Path, help='new output directory (always retained)')
    parser.add_argument('--scenario', choices=('settings-partial-save', 'install-script-write-failure', 'install-retry'))
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    artifacts = (args.artifacts or Path('/tmp') / f'am-failures-{time.time_ns()}').resolve()
    started = time.monotonic()
    results = {}
    for name, run in [('settings-partial-save', settings_partial_save),
                      ('install-script-write-failure', install_script_write_failure),
                      ('install-retry', install_retry_creates_one_session)]:
        if args.scenario and args.scenario != name:
            continue
        sandbox = Sandbox(artifacts / name)
        try:
            binary = ensure_binary(root, artifacts, args.binary)
            run(sandbox, binary)
            frame(sandbox, 'ready-to-quit', 'A G E N T', 'Settings')
            key(sandbox, 'q')
            sandbox.wait_manager_exit('scen:0.0')
            results[name] = 'passed'
        except (Exception, KeyboardInterrupt) as error:
            results[name] = f'failed: {str(error) or type(error).__name__}'
            exit_state = sandbox.tmux('display-message', '-p', '-t', 'scen:0.0',
                                     '#{pane_dead} #{pane_dead_status} #{pane_dead_signal}', check=False)
            (sandbox.artifacts / 'manager-exit-state.txt').write_text(exit_state.stdout + exit_state.stderr)
            (sandbox.artifacts / 'scen-last-frame.txt').write_text(capture(sandbox))
            sandbox.failure_frames()
        finally:
            cleanup_errors = sandbox.close()
            if cleanup_errors:
                results[name] += f' (cleanup: {cleanup_errors})'
    status = 'passed' if all(value == 'passed' for value in results.values()) else 'failed'
    result = dict(status=status, scenarios=results,
                  seconds=round(time.monotonic() - started, 2))
    (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(artifacts))))
    return 0 if status == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())

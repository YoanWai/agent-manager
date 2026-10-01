#!/usr/bin/env python3
"""Blocked-worker scenario suite against the real binary; Python standard library only.

Each case accepts a durable rename or move, then holds a
SQLite BEGIN IMMEDIATE transaction so the accepted write blocks while the UI
keeps accepting input. The dialog is dismissed and the same target's dialog
reopened (a newer dialog/foreground) before the lock releases, and the case
checks durable accepted writes and usability of the reopened dialog.
Focused unit tests explicitly deliver stale completions; a durable write
observation alone does not prove Update has processed its completion. Assertions read durable store rows
and dialog presence, never cursor glyphs.

The manager store connection carries busy_timeout(5000); lock-held UI
observations have a shorter deadline so a blocked foreground fails the case.
Shares one binary build per invocation; each case gets its own Sandbox.
"""
import argparse
import contextlib
import json
import pathlib
import re
import sqlite3
import tempfile
import time

from smoke import Sandbox
from scenarios import (READY_MARKER, agent_pane, capture,
                       ensure_binary, key,
                       pane_text, profile_dir, select_row, seed_store,
                       spawn_form, start_manager, store_sessions,
                       write_fixture)


def frame(sandbox, name, present, absent=''):
    return sandbox.wait(name, lambda: capture(sandbox),
                        lambda value: present in value and (not absent or absent not in value),
                        timeout=2)


@contextlib.contextmanager
def store_lock(sandbox):
    """Hold the SQLite write lock so accepted durable writes block.

    BEGIN IMMEDIATE takes the write lock the moment it lands; the manager's
    blocked worker busy-waits on it (up to its 5s timeout). Always released
    by rollback even on failure, so a stuck case cannot strand the profile.
    """
    db = profile_dir(sandbox) / 'state.db'
    conn = sqlite3.connect(str(db), timeout=5)
    try:
        conn.execute('BEGIN IMMEDIATE')
        started = time.monotonic()
        try:
            yield conn
            if time.monotonic() - started >= 4:
                raise TimeoutError('foreground exceeded the four-second lock budget')
        finally:
            conn.rollback()
    finally:
        conn.close()


def type_text(sandbox, text):
    sandbox.tmux('send-keys', '-l', '-t', 'scen:0.0', text)


def names(sandbox):
    return [row['name'] for row in store_sessions(sandbox)]


def group_of(sandbox, name):
    rows = [row for row in store_sessions(sandbox) if row['name'] == name]
    return rows[0]['group'] if rows else ''


def rename_blocked(sandbox, binary):
    write_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    name = spawn_form(sandbox, 'blocked-rename')
    pane = agent_pane(sandbox, 'sleep')
    sandbox.wait('rename-spawn-ready', lambda: pane_text(sandbox, pane),
                 lambda text: READY_MARKER in text)
    select_row(sandbox, name, 'rename-select')
    key(sandbox, 'r')
    frame(sandbox, 'rename-open', 'Rename')
    with store_lock(sandbox):
        type_text(sandbox, 'X')
        frame(sandbox, 'rename-typed', 'blocked-renameX')
        key(sandbox, 'Enter')
        frame(sandbox, 'rename-accepted', 'blocked-renameX')
        # Accepted and in flight: the durable row is still unchanged.
        assert 'blocked-renameX' not in names(sandbox), 'write committed while locked'
        key(sandbox, 'Escape')
        select_row(sandbox, name, 'rename-reselect')
        key(sandbox, 'r')
        frame(sandbox, 'rename-reopened', 'Rename')
        type_text(sandbox, 'X')
        frame(sandbox, 'rename-reopened-typed', 'blocked-renameX')
    # Released: the accepted rename reconciles durably...
    sandbox.wait('rename-durable-reconciled', lambda: json.dumps(names(sandbox)),
                 lambda value: 'blocked-renameX' in json.loads(value))
    # ...and the stale completion must not have closed the newer dialog.
    frame(sandbox, 'rename-stale-kept-dialog', 'Rename')
    key(sandbox, 'BSpace')
    type_text(sandbox, 'Y')
    frame(sandbox, 'rename-newer-input', 'blocked-renameY')
    key(sandbox, 'Enter')
    sandbox.wait('rename-second-committed', lambda: json.dumps(names(sandbox)),
                 lambda value: 'blocked-renameY' in json.loads(value))
    sandbox.wait('rename-closed', lambda: capture(sandbox), lambda value: 'Rename' not in value)


def move_blocked(sandbox, binary):
    write_fixture(sandbox)
    start_manager(sandbox, binary)
    seed_store(sandbox)
    name = spawn_form(sandbox, 'blocked-move')
    pane = agent_pane(sandbox, 'sleep')
    sandbox.wait('move-spawn-ready', lambda: pane_text(sandbox, pane),
                 lambda text: READY_MARKER in text)
    key(sandbox, 'g')
    frame(sandbox, 'move-group-form', 'New Group')
    type_text(sandbox, 'blocked-group')
    frame(sandbox, 'move-group-name', 'blocked-group')
    key(sandbox, 'Enter')
    frame(sandbox, 'move-group-created', 'blocked-group', 'New Group')
    select_row(sandbox, name, 'move-select')
    key(sandbox, 'm')
    frame(sandbox, 'move-open', '⇄ Move')
    key(sandbox, 'Down')
    sandbox.wait('move-target-selected', lambda: capture(sandbox),
                 lambda value: re.search(r'❯\s+blocked-group', value) is not None, timeout=2)
    with store_lock(sandbox):
        key(sandbox, 'Enter')
        frame(sandbox, 'move-accepted', '⇄ Move')
        # Accepted and in flight: the durable row is still unchanged.
        assert group_of(sandbox, name) != 'blocked-group', 'move committed while locked'
        key(sandbox, 'Escape')
        select_row(sandbox, name, 'move-reselect')
        key(sandbox, 'm')
        frame(sandbox, 'move-reopened', '⇄ Move')
        key(sandbox, 'Down')
        sandbox.wait('move-reopened-target', lambda: capture(sandbox),
                     lambda value: re.search(r'❯\s+blocked-group', value) is not None, timeout=2)
    sandbox.wait('move-durable-reconciled', lambda: json.dumps(group_of(sandbox, name)),
                 lambda value: json.loads(value) == 'blocked-group')
    frame(sandbox, 'move-reopened-still-visible', '⇄ Move')
    key(sandbox, 'Enter')
    # The newer dialog can confirm its now-current placement.
    sandbox.wait('move-closed', lambda: capture(sandbox), lambda value: '⇄ Move' not in value)
    sandbox.wait('move-final-durable', lambda: json.dumps(group_of(sandbox, name)),
                 lambda value: json.loads(value) == 'blocked-group')


CASES = [
    ('rename', rename_blocked),
    ('move', move_blocked),
]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path,
                        help='existing binary; otherwise build once before isolating HOME')
    parser.add_argument('--artifacts', type=pathlib.Path,
                        help='new output directory (always retained)')
    parser.add_argument('--scenario', choices=[name for name, _ in CASES])
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parents[2]
    artifacts = (args.artifacts
                 or pathlib.Path(tempfile.gettempdir()) / f'am-blocked-{time.time_ns()}').resolve()
    artifacts.mkdir(parents=True, exist_ok=False)
    started = time.monotonic()
    results = {}
    status = 'passed'
    try:
        binary = ensure_binary(root, artifacts, args.binary)
        for case, runner in CASES:
            if args.scenario and args.scenario != case:
                continue
            sandbox = Sandbox(artifacts / case)
            case_started = time.monotonic()
            try:
                runner(sandbox, binary)
                key(sandbox, 'C-c')
                sandbox.wait('manager-exit', lambda: sandbox.tmux('display-message', '-p', '-t', 'scen:0.0', '#{pane_dead} #{pane_dead_status}').stdout.strip(), lambda value: value == '1 0')
                results[case] = dict(status='passed',
                                     seconds=round(time.monotonic() - case_started, 2))
            except Exception as error:
                (sandbox.artifacts / 'scen-last-frame.txt').write_text(capture(sandbox))
                results[case] = dict(status='failed',
                                     error=str(error) or type(error).__name__,
                                     capture_errors=sandbox.failure_frames(),
                                     seconds=round(time.monotonic() - case_started, 2))
                status = 'failed'
            finally:
                errors = sandbox.close()
                if errors:
                    results[case]['cleanup_errors'] = errors
                    status = 'failed'
    except (Exception, KeyboardInterrupt) as error:
        results = dict(results, harness=dict(status='failed',
                                             error=str(error) or type(error).__name__))
        status = 'failed'
    result = dict(status=status, cases=results, seconds=round(time.monotonic() - started, 2))
    (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(artifacts))))
    return 0 if status == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())

#!/usr/bin/env python3
"""Contract tests for the failure-scenario harness; Python standard library only.

These pin the fixture and sandbox contracts the failure scenarios rely on:
the Sandbox refuses sockets outside its owned pair, cleanup removes only the
owned socket directory, the fake npm fixture installs only the command-code
CLI, and the store readers answer what the store actually holds. No manager
binary is launched here.
"""
import os
import shutil
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path

from failure_scenarios import (setting_value, session_rows,
                                write_install_fixture)
from scenarios import store_sessions
from smoke import Sandbox


class ContractTests(unittest.TestCase):
    def setUp(self):
        self.dir = Path(tempfile.mkdtemp(prefix='am-failure-contract-'))
        self.sandbox = Sandbox(self.dir / 'sandbox')

    def tearDown(self):
        self.sandbox.close()
        shutil.rmtree(self.dir, ignore_errors=True)

    def test_sandbox_refuses_foreign_sockets(self):
        for socket in Sandbox.sockets:
            self.assertTrue((self.sandbox.socket_dir / f'tmux-{os.getuid()}' / socket).parent
                            .exists(), f'owned socket directory missing for {socket}')
        with self.assertRaises(ValueError):
            self.sandbox.tmux('ls', socket='not-owned')

    def test_sandbox_cleanup_removes_only_its_own_directory(self):
        sibling = Sandbox(self.dir / 'sibling')
        try:
            own = self.sandbox.socket_dir
            self.sandbox.close()
            self.assertFalse(own.exists(), 'close must remove the owned socket directory')
            self.assertTrue(sibling.socket_dir.exists(),
                            'close must not touch a sibling sandbox socket directory')
        finally:
            sibling.close()

    def test_fake_npm_installs_only_command_code(self):
        write_install_fixture(self.sandbox)
        bin_dir = self.sandbox.home / 'bin'
        cmd = bin_dir / 'cmd'
        self.assertFalse(cmd.exists(), 'the CLI must be absent before the install runs')
        self.assertEqual(subprocess.run([str(bin_dir / 'npm'), 'install', '-g', 'grok'],
                                        capture_output=True).returncode, 0)
        self.assertFalse(cmd.exists(), 'an unrelated install must not create the CLI')
        self.assertEqual(subprocess.run([str(bin_dir / 'npm'), 'install', '-g',
                                         'command-code'], capture_output=True).returncode, 0)
        self.assertTrue(cmd.exists() and os.access(cmd, os.X_OK),
                        'the fixture install must leave an executable CLI on the fixture PATH')
        self.assertEqual(subprocess.run([str(cmd), 'mcp', 'add', 'fixture'], capture_output=True, timeout=1).returncode, 0)

    def test_host_cmd_is_not_discoverable_before_fixture_install(self):
        host_bin = self.dir / 'host-bin'
        host_bin.mkdir()
        host_cmd = host_bin / 'cmd'
        host_cmd.write_text('#!/bin/sh\nexit 91\n')
        host_cmd.chmod(0o755)
        self.sandbox.env['PATH'] = str(host_bin) + ':' + self.sandbox.env['PATH']
        write_install_fixture(self.sandbox)
        self.assertIsNone(shutil.which('cmd', path=self.sandbox.env['PATH']))
        self.sandbox.run(['npm', 'install', '-g', 'command-code'])
        self.assertEqual(shutil.which('cmd', path=self.sandbox.env['PATH']),
                         str(self.sandbox.home / 'bin' / 'cmd'))

    def test_setting_value_answers_store_contents(self):
        from scenarios import profile_dir
        profile = profile_dir(self.sandbox)
        profile.mkdir(parents=True)
        db = profile / 'state.db'
        conn = sqlite3.connect(str(db))
        try:
            conn.execute('CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)')
            conn.execute("INSERT INTO settings VALUES ('list_density', 'comfortable')")
            conn.commit()
        finally:
            conn.close()
        self.assertEqual(setting_value(self.sandbox, 'list_density'), 'comfortable')
        self.assertEqual(setting_value(self.sandbox, 'mouse'), '')

    def test_session_rows_filters_by_name(self):
        from scenarios import profile_dir
        profile = profile_dir(self.sandbox)
        profile.mkdir(parents=True)
        db = profile / 'state.db'
        conn = sqlite3.connect(str(db))
        try:
            conn.execute('CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT NOT NULL, '
                         'tool TEXT NOT NULL, group_name TEXT NOT NULL)')
            conn.executemany('INSERT INTO sessions VALUES (?, ?, ?, ?)',
                             [('a', 'install-agent', 'command-code', ''),
                              ('b', 'install-cmd', 'terminal', ''),
                              ('c', 'install-agent', 'command-code', '')])
            conn.commit()
        finally:
            conn.close()
        self.assertEqual([row['id'] for row in session_rows(self.sandbox, 'install-agent')],
                         ['a', 'c'])
        self.assertEqual(len(session_rows(self.sandbox, 'install-cmd')), 1)
        self.assertEqual(session_rows(self.sandbox, 'absent'), [])
        self.assertEqual(store_sessions(self.sandbox), [
            dict(id='a', name='install-agent', tool='command-code', group=''),
            dict(id='b', name='install-cmd', tool='terminal', group=''),
            dict(id='c', name='install-agent', tool='command-code', group=''),
        ])


if __name__ == '__main__':
    unittest.main()

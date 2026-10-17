"""Behavioral contracts for the disposable SQLite barrier."""
import sqlite3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from blocked_scenarios import store_lock
from scenarios import profile_dir


class StoreLockTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.sandbox = SimpleNamespace(home=Path(self.temp.name))
        self.db = profile_dir(self.sandbox) / 'state.db'
        self.db.parent.mkdir(parents=True)
        with sqlite3.connect(self.db) as conn:
            conn.execute('CREATE TABLE fixture(value TEXT)')

    def test_blocks_another_writer_then_releases(self):
        with store_lock(self.sandbox):
            with sqlite3.connect(self.db, timeout=0) as peer:
                with self.assertRaisesRegex(sqlite3.OperationalError, 'locked'):
                    peer.execute("INSERT INTO fixture VALUES ('blocked')")
        with sqlite3.connect(self.db, timeout=0) as peer:
            peer.execute("INSERT INTO fixture VALUES ('released')")
            self.assertEqual(peer.execute('SELECT value FROM fixture').fetchall(), [('released',)])

    def test_exception_releases_and_rolls_back(self):
        with self.assertRaisesRegex(RuntimeError, 'fixture failure'):
            with store_lock(self.sandbox) as conn:
                conn.execute("INSERT INTO fixture VALUES ('uncommitted')")
                raise RuntimeError('fixture failure')
        with sqlite3.connect(self.db, timeout=0) as peer:
            self.assertEqual(peer.execute('SELECT value FROM fixture').fetchall(), [])
            peer.execute("INSERT INTO fixture VALUES ('recovered')")


if __name__ == '__main__':
    unittest.main()

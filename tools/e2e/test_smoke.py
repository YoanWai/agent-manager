import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from smoke import Sandbox


class SandboxTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.sandbox = Sandbox(Path(self.temp.name) / 'evidence')

    def tearDown(self):
        with patch.object(self.sandbox, 'tmux'):
            self.sandbox.close()
        self.temp.cleanup()

    def test_inherited_tmux_cannot_select_live_server(self):
        self.assertNotIn('TMUX', self.sandbox.env)
        self.assertLess(len(str(self.sandbox.socket_dir / 'tmux-99999/agentmgr')), 104)
        self.assertTrue(self.sandbox.socket_dir.is_dir())
        self.assertEqual(self.sandbox.env['HOME'], str(self.sandbox.home))

    def test_timeout_retains_last_observation(self):
        with self.assertRaises(TimeoutError):
            self.sandbox.wait('readiness', lambda: 'still starting', lambda _: False, timeout=0)
        self.assertEqual((self.sandbox.artifacts / 'readiness-failure.txt').read_text(), 'still starting')

    def test_cleanup_names_only_owned_servers(self):
        with patch.object(self.sandbox, 'run') as run:
            self.sandbox.close()
        self.assertEqual([call.args[0] for call in run.call_args_list], [
            ['tmux', '-L', 'e2e-outer', 'kill-server'],
            ['tmux', '-L', 'agentmgr', 'kill-server']])
        self.assertTrue(self.sandbox.home.exists())
        self.assertFalse(self.sandbox.socket_dir.exists())
        # tearDown is intentionally harmless after cleanup.
        self.sandbox.socket_dir.mkdir()

    def test_unknown_socket_rejected_before_dispatch(self):
        with patch.object(self.sandbox, 'run') as run:
            with self.assertRaises(ValueError):
                self.sandbox.tmux('kill-server', socket='default')
        run.assert_not_called()


if __name__ == '__main__':
    unittest.main()

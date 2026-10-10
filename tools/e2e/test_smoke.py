import os
import subprocess
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
        with patch.object(self.sandbox, 'tmux', return_value=subprocess.CompletedProcess([], 0, '', '')):
            self.sandbox.close()
        self.temp.cleanup()

    def test_inherited_tmux_and_git_cannot_select_live_resources(self):
        with patch.dict(os.environ, {'TMUX': '/live/socket,1,0', 'GIT_DIR': '/live/repo',
                                     'GIT_CONFIG_COUNT': '1', 'AGENT_MANAGER_SESSION_ID': 'live-session'}):
            isolated = Sandbox(Path(self.temp.name) / 'inherited-evidence')
        try:
            self.assertNotIn('TMUX', isolated.env)
            self.assertNotIn('GIT_DIR', isolated.env)
            self.assertNotIn('GIT_CONFIG_COUNT', isolated.env)
            self.assertNotIn('AGENT_MANAGER_SESSION_ID', isolated.env)
            self.assertLess(len(str(isolated.socket_dir / 'tmux-99999/agentmgr')), 104)
            self.assertTrue(isolated.socket_dir.is_dir())
            self.assertEqual(isolated.env['HOME'], str(isolated.home))
        finally:
            with patch.object(isolated, 'tmux', return_value=subprocess.CompletedProcess([], 0, '', '')):
                isolated.close()

    def test_fixture_shell_delegates_command_execution(self):
        result = subprocess.run([self.sandbox.env['SHELL'], '-c', 'printf command-ready'],
                                env=self.sandbox.env, text=True, capture_output=True, timeout=2)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, 'command-ready')

    def test_timeout_retains_last_observation(self):
        with self.assertRaises(TimeoutError):
            self.sandbox.wait('readiness', lambda: 'still starting', lambda _: False, timeout=0)
        self.assertEqual((self.sandbox.artifacts / 'readiness-failure.txt').read_text(), 'still starting')

    def test_manager_exit_reads_the_wrappers_exit_code(self):
        def wait_on(pane_state, code):
            if code is not None:
                (self.sandbox.artifacts / 'manager-exit-code.txt').write_text(code + '\n')
            result = subprocess.CompletedProcess([], 0, pane_state, '')
            with patch.object(self.sandbox, 'tmux', return_value=result):
                return self.sandbox.wait_manager_exit('smoke:0.0')

        self.assertEqual(wait_on('1 0\n', '0'), '1 0 code=0')
        # tmux marked the pane dead without recording a status.
        self.assertEqual(wait_on('1 \n', '0'), '1 code=0')
        with patch('smoke.time.monotonic', side_effect=[0, 0, 13]):
            with self.assertRaises(TimeoutError):
                wait_on('1 2\n', '2')

    def test_cleanup_names_only_owned_servers(self):
        with patch.object(self.sandbox, 'run', return_value=subprocess.CompletedProcess([], 0, '', '')) as run:
            self.sandbox.close()
        self.assertEqual([call.args[0] for call in run.call_args_list], [
            ['tmux', '-S', str(self.sandbox.socket_dir / f'tmux-{os.getuid()}' / 'e2e-outer'), 'kill-server'],
            ['tmux', '-S', str(self.sandbox.socket_dir / f'tmux-{os.getuid()}' / 'agentmgr'), 'kill-server']])
        self.assertTrue(self.sandbox.home.exists())
        self.assertFalse(self.sandbox.socket_dir.exists())
        # tearDown is intentionally harmless after cleanup.
        self.sandbox.socket_dir.mkdir()

    def test_cleanup_continues_after_first_server_failure(self):
        with patch.object(self.sandbox, 'tmux', side_effect=[
                TimeoutError('cleanup deadline'), subprocess.CompletedProcess([], 0, '', '')]) as tmux:
            errors = self.sandbox.close()
        self.assertEqual(tmux.call_count, 2)
        self.assertIn('cleanup deadline', errors[0])
        self.assertTrue(self.sandbox.socket_dir.exists())

    def test_failure_capture_continues_without_raising(self):
        with patch.object(self.sandbox, 'tmux', side_effect=[
                TimeoutError('capture deadline'), subprocess.CompletedProcess([], 0, 'shell frame', '')]):
            errors = self.sandbox.failure_frames()
        self.assertEqual(len(errors), 1)
        self.assertEqual((self.sandbox.artifacts / 'agentmgr-last-frame.txt').read_text(), 'shell frame')

    def test_unknown_socket_rejected_before_dispatch(self):
        with patch.object(self.sandbox, 'run') as run:
            with self.assertRaises(ValueError):
                self.sandbox.tmux('kill-server', socket='default')
        run.assert_not_called()


if __name__ == '__main__':
    unittest.main()

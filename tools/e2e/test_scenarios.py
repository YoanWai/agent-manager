"""Selection helpers preserve the requested row and its evidence name."""
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import scenarios
import ssh_scenarios


def preview(text):
    return ' ' * scenarios.RAIL_WIDTH + text


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.sandbox = SimpleNamespace(artifacts=Path(self.temp.name), wait=self.wait)

    def wait(self, name, observe, predicate, timeout):
        for _ in range(5):
            value = observe()
            if predicate(value):
                return value
        raise TimeoutError(name)

    def test_profile_uses_the_explicit_xdg_directory_on_macos(self):
        sandbox = SimpleNamespace(home=Path('/fixture/home'))
        with patch('sys.platform', 'darwin'):
            self.assertEqual(scenarios.profile_dir(sandbox),
                             Path('/fixture/home/.config/agent-manager'))

    def test_selected_local_row_does_not_receive_a_navigation_key(self):
        selected = preview('wanted\n') + '\n' + preview(scenarios.READY_MARKER)
        with patch.object(scenarios, 'capture', return_value=selected), \
                patch.object(scenarios, 'key') as key:
            scenarios.select_row(self.sandbox, 'wanted', 'quick-send')
        key.assert_not_called()
        self.assertEqual((self.sandbox.artifacts / 'quick-send-selected.txt').read_text(), selected)

    def test_local_selection_waits_for_redraw_and_preserves_step_name(self):
        old = preview('other\n') + '\n' + preview(scenarios.READY_MARKER)
        selected = preview('wanted\n') + '\n' + preview(scenarios.READY_MARKER)
        with patch.object(scenarios, 'capture', side_effect=[old, old, selected, selected, selected, selected]), \
                patch.object(scenarios, 'key') as key:
            scenarios.select_row(self.sandbox, 'wanted', 'moving')
        key.assert_called_once_with(self.sandbox, 'j')
        self.assertEqual((self.sandbox.artifacts / 'moving-selected.txt').read_text(), selected)
        self.assertFalse((self.sandbox.artifacts / 'j-selected.txt').exists())

    def test_selected_remote_preview_does_not_receive_a_navigation_key(self):
        selected = preview('remote-agent on box')
        with patch.object(ssh_scenarios, 'capture', return_value=selected), \
                patch.object(ssh_scenarios, 'key') as key:
            ssh_scenarios.select(self.sandbox, 'remote-agent', 'remote-agent on box')
        key.assert_not_called()
        self.assertEqual((self.sandbox.artifacts / 'remote-agent-selected.txt').read_text(), selected)

    def test_remote_selection_waits_through_stale_non_target_frames(self):
        old = preview('other agent')
        selected = preview('remote-agent on box')
        with patch.object(ssh_scenarios, 'capture', side_effect=[old, old, selected, selected]), \
                patch.object(ssh_scenarios, 'key') as key:
            ssh_scenarios.select(self.sandbox, 'remote-agent', 'remote-agent on box')
        key.assert_called_once_with(self.sandbox, 'j')
        self.assertEqual((self.sandbox.artifacts / 'remote-agent-selected.txt').read_text(), selected)

    def test_rail_visibility_does_not_prove_selected_row(self):
        frame = 'wanted'.ljust(scenarios.RAIL_WIDTH) + 'other agent'
        self.assertFalse(scenarios.row_selected(frame, 'wanted'))


if __name__ == '__main__':
    unittest.main()

import ast
from pathlib import Path
import unittest

SCRIPTS = ('smoke.py', 'scenarios.py', 'failure_scenarios.py', 'blocked_scenarios.py', 'ssh_scenarios.py')

# The poll cadence inside a bounded observation loop is the only pause a
# scenario may take. Two of them exist: Sandbox.wait, whose argument widens
# with the time already spent so a condition that never holds costs a few
# observations rather than a pane capture every 50ms for the whole timeout,
# and end_pids, which polls for a process to exit. Both are written as a
# literal or computed argument at a known call site; anything else is a pause.
# Everywhere else a transition is awaited by observing it: a fixed sleep holds
# only as long as the machine that chose it, so it passes locally and fails on
# a slower CI runner, or the reverse. Set to a number instead when a delay is
# genuinely part of the product behaviour being checked.
POLL_CADENCE = 0.05


def is_poll_cadence(node):
    """True for the sleep at the end of Sandbox.wait's loop.

    Its argument is computed, not written, because the interval widens with the
    time already spent: a condition that never holds costs a few observations
    instead of a pane capture every 50ms for the whole timeout.
    """
    return node.args and not isinstance(node.args[0], ast.Constant)


def sleeps(path):
    """Every time.sleep call in a script, as (line, argument value or None)."""
    tree = ast.parse(path.read_text())
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        if not (isinstance(func, ast.Attribute) and func.attr == 'sleep'):
            continue
        if not (isinstance(func.value, ast.Name) and func.value.id == 'time'):
            continue
        if is_poll_cadence(node):
            found.append((node.lineno, 'cadence'))
            continue
        value = None
        if node.args and isinstance(node.args[0], ast.Constant):
            value = node.args[0].value
        found.append((node.lineno, value))
    return found


class SleepTests(unittest.TestCase):
    def test_scenarios_wait_for_conditions_instead_of_sleeping(self):
        root = Path(__file__).resolve().parent
        offenders = []
        for name in SCRIPTS:
            for line, value in sleeps(root / name):
                if value not in ('cadence', POLL_CADENCE):
                    offenders.append(f'{name}:{line} sleeps {value!r}')
        self.assertEqual(offenders, [],
                         'transitions are awaited by observing them; only the poll cadence may sleep: '
                         + ', '.join(offenders))

    def test_the_poll_cadence_itself_is_still_the_only_pause(self):
        root = Path(__file__).resolve().parent
        cadences = [(name, line) for name in SCRIPTS for line, value in sleeps(root / name)
                    if value in ('cadence', POLL_CADENCE)]
        self.assertEqual(len(cadences), 2,
                         'expected the two observation cadences, found ' + repr(cadences))


if __name__ == '__main__':
    unittest.main()

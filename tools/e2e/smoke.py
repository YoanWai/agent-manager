#!/usr/bin/env python3
"""Isolated real-terminal smoke; Python standard library only."""
import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time


class Sandbox:
    sockets = ('e2e-outer', 'agentmgr')

    def __init__(self, artifacts):
        self.artifacts = Path(artifacts)
        self.artifacts.mkdir(parents=True, exist_ok=False)
        self.socket_dir = Path(tempfile.mkdtemp(prefix='ame-', dir='/tmp'))
        self.home = self.artifacts / 'home'
        self.home.mkdir()
        self.env = dict(os.environ, HOME=str(self.home), ZDOTDIR=str(self.home),
                        XDG_CONFIG_HOME=str(self.home / '.config'),
                        XDG_DATA_HOME=str(self.home / '.local/share'),
                        XDG_STATE_HOME=str(self.home / '.local/state'),
                        TMUX_TMPDIR=str(self.socket_dir), TERM='xterm-256color',
                        COLORTERM='truecolor', SHELL='/bin/sh', NO_COLOR='1')
        for key in ('TMUX', 'TMUX_PANE', 'AGENT_MANAGER_PROFILE', 'COLORFGBG'):
            self.env.pop(key, None)
        self.log = self.artifacts / 'commands.jsonl'

    def run(self, args, check=True):
        result = subprocess.run(args, env=self.env, text=True, capture_output=True, timeout=20)
        with self.log.open('a') as stream:
            stream.write(json.dumps(dict(args=args, code=result.returncode,
                                         stdout=result.stdout, stderr=result.stderr)) + '\n')
        if check and result.returncode:
            raise RuntimeError(f'{args}: exit {result.returncode}: {result.stderr}')
        return result

    def tmux(self, *args, socket='e2e-outer', check=True):
        if socket not in self.sockets:
            raise ValueError('socket is outside this sandbox')
        return self.run(['tmux', '-L', socket, *args], check=check)

    def wait(self, name, observe, predicate, timeout=12):
        started = time.monotonic()
        deadline = started + timeout
        last = ''
        while True:
            last = observe()
            if predicate(last):
                with (self.artifacts / 'timings.jsonl').open('a') as stream:
                    stream.write(json.dumps(dict(step=name, seconds=round(time.monotonic()-started, 3))) + '\n')
                (self.artifacts / (name + '.txt')).write_text(last)
                return last
            if time.monotonic() >= deadline:
                (self.artifacts / (name + '-failure.txt')).write_text(last)
                raise TimeoutError(f'{name}: condition not observed within {timeout}s')
            # Poll cadence only: transitions always require observable conditions.
            time.sleep(0.05)

    def capture(self):
        return self.tmux('capture-pane', '-p', '-t', 'smoke:0.0').stdout

    def frame(self, name, present, absent=''):
        return self.wait(name, self.capture,
                         lambda frame: present in frame and (not absent or absent not in frame))

    def key(self, key):
        self.tmux('send-keys', '-t', 'smoke:0.0', key)

    def close(self):
        for socket in self.sockets:
            self.tmux('kill-server', socket=socket, check=False)
        shutil.rmtree(self.socket_dir)


def smoke(sandbox, binary):
    # reman-on-exit permits exit-code inspection instead of mistaking a vanished pane for success.
    sandbox.tmux('new-session', '-d', '-s', 'smoke', '-x', '110', '-y', '30',
                 f'exec env TERM=xterm-256color COLORTERM=truecolor NO_COLOR=1 {shlex.quote(str(binary))}')
    sandbox.tmux('set-option', '-w', '-t', 'smoke', 'remain-on-exit', 'on')
    sandbox.frame('startup', 'A G E N T')
    sandbox.key('Escape')
    sandbox.key('?')
    sandbox.frame('help-open', '? Keys')
    sandbox.key('/')
    sandbox.frame('search-open', 'done')
    sandbox.tmux('send-keys', '-l', '-t', 'smoke:0.0', 'fork')
    sandbox.frame('search-typed', 'search fork')
    sandbox.key('Enter')
    sandbox.frame('search-committed', 'clear search')
    sandbox.key('Escape')
    sandbox.frame('search-cleared', 'move the cursor up', 'search fork')
    # A wheel outside the dialog must be consumed, never interpreted by the rail.
    wheel = b'\x1b[<65;2;15M'
    sandbox.tmux('send-keys', '-H', '-t', 'smoke:0.0', *[f'{byte:02x}' for byte in wheel])
    # Ordered key response is the acknowledgement that the preceding mouse event was handled.
    sandbox.key('PageDown')
    sandbox.frame('wheel-consumed-keyboard-scroll', 'more above', 'move the cursor up')
    sandbox.key('Escape')
    sandbox.frame('help-closed', 'A G E N T', '? Keys')
    sandbox.key('T')
    sandbox.frame('terminal-row', 'terminal-')
    panes = sandbox.wait('terminal-panes',
                        lambda: sandbox.tmux('list-panes', '-a', '-F',
                            '#{pane_id} #{pane_current_command}', socket='agentmgr', check=False).stdout,
                        lambda value: len(value.splitlines()) == 1 and len(value.split()) == 2)
    pane = panes.split()[0]
    # A unique file proves the actual shell accepted a command, not a footer label or prompt.
    acknowledgement = sandbox.home / 'shell-ack'
    sandbox.tmux('send-keys', '-l', '-t', pane,
                 f'printf shell-ready > {shlex.quote(str(acknowledgement))}', socket='agentmgr')
    sandbox.tmux('send-keys', '-t', pane, 'Enter', socket='agentmgr')
    sandbox.wait('shell-ready', lambda: acknowledgement.read_text() if acknowledgement.exists() else '',
                 lambda value: value == 'shell-ready')
    sandbox.key('C-c')
    sandbox.wait('manager-exit', lambda: sandbox.tmux('display-message', '-p', '-t', 'smoke:0.0',
                  '#{pane_dead} #{pane_dead_status}').stdout.strip(), lambda value: value == '1 0')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, help='existing binary; otherwise build once before isolating HOME')
    parser.add_argument('--artifacts', type=Path, help='new output directory (always retained)')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    artifacts = (args.artifacts or Path(tempfile.gettempdir()) / f'am-e2e-{time.time_ns()}').resolve()
    started = time.monotonic()
    sandbox = Sandbox(artifacts)
    try:
        binary = args.binary.resolve() if args.binary else artifacts / 'agent-manager'
        if not args.binary:
            with (artifacts / 'build.log').open('w') as log:
                subprocess.run(['go', 'build', '-o', str(binary), '.'], cwd=root,
                               stdout=log, stderr=subprocess.STDOUT, check=True)
        smoke(sandbox, binary)
        result = dict(status='passed', seconds=round(time.monotonic()-started, 2))
    except Exception as error:
        for socket, target in [('e2e-outer', 'smoke:0.0'), ('agentmgr', '%0')]:
            capture = sandbox.tmux('capture-pane', '-p', '-t', target, socket=socket, check=False)
            (artifacts / (socket + '-last-frame.txt')).write_text(capture.stdout + capture.stderr)
        result = dict(status='failed', error=str(error), seconds=round(time.monotonic()-started, 2))
    finally:
        sandbox.close()
    (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(result, artifacts=str(artifacts))))
    return 0 if result['status'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())

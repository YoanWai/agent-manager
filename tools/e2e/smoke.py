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
        (self.socket_dir / f'tmux-{os.getuid()}').mkdir(mode=0o700)
        self.home = self.artifacts / 'home'
        self.home.mkdir()
        self.prompt = f'am-shell-{time.time_ns()}> '
        shell = self.home / 'fixture-shell'
        shell.write_text('#!/bin/sh\nunset ENV BASH_ENV\n' +
                         'if [ "$#" -gt 0 ]; then exec /bin/sh "$@"; fi\n' +
                         'PS1=' + shlex.quote(self.prompt) + '\nexport PS1\nexec /bin/sh -i\n')
        shell.chmod(0o700)
        self.env = dict(os.environ, HOME=str(self.home), ZDOTDIR=str(self.home),
                        XDG_CONFIG_HOME=str(self.home / '.config'),
                        XDG_DATA_HOME=str(self.home / '.local/share'),
                        XDG_STATE_HOME=str(self.home / '.local/state'),
                        TMUX_TMPDIR=str(self.socket_dir), TERM='xterm-256color',
                        COLORTERM='truecolor', SHELL=str(shell), NO_COLOR='1')
        for key in ('TMUX', 'TMUX_PANE', 'AGENT_MANAGER_PROFILE', 'AGENT_MANAGER_SESSION_ID', 'COLORFGBG',
                    'ENV', 'BASH_ENV', 'PROMPT_COMMAND'):
            self.env.pop(key, None)
        for key in tuple(self.env):
            if key.startswith('GIT_'):
                self.env.pop(key)
        self.env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null')
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
        path = self.socket_dir / f'tmux-{os.getuid()}' / socket
        return self.run(['tmux', '-S', str(path), *args], check=check)

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
        errors = []
        for socket in self.sockets:
            try:
                result = self.tmux('kill-server', socket=socket, check=False)
                if result.returncode and not any(message in result.stderr for message in
                        ('no server running', 'No such file or directory')):
                    errors.append(f'{socket}: {result.stderr.strip()}')
            except Exception as error:
                errors.append(f'{socket}: {error}')
        # Keep sockets reachable if cleanup failed; never strand an owned daemon.
        if not errors and self.socket_dir.exists():
            try:
                shutil.rmtree(self.socket_dir)
            except OSError as error:
                errors.append(f'socket directory: {error}')
        return errors

    def failure_frames(self):
        errors = []
        for socket, target in [('e2e-outer', 'smoke:0.0'), ('agentmgr', '%0')]:
            try:
                capture = self.tmux('capture-pane', '-p', '-t', target, socket=socket, check=False)
                (self.artifacts / (socket + '-last-frame.txt')).write_text(capture.stdout + capture.stderr)
            except Exception as error:
                errors.append(f'{socket}: {error}')
        return errors


def smoke(sandbox, binary):
    repo = sandbox.artifacts / 'fixture-repo'
    repo.mkdir()
    sandbox.run(['git', 'init', str(repo)])
    changed = repo / 'review-fixture.txt'
    changed.write_text('before_review_e2e\n')
    sandbox.run(['git', '-C', str(repo), 'add', changed.name])
    sandbox.run(['git', '-C', str(repo), '-c', 'user.name=E2E',
                 '-c', 'user.email=e2e@example.invalid', '-c', 'commit.gpgsign=false',
                 'commit', '-m', 'fixture baseline'])
    changed.write_text('after_review_e2e\n')
    # remain-on-exit permits exit-code inspection instead of mistaking a vanished pane for success.
    sandbox.tmux('new-session', '-d', '-s', 'smoke', '-x', '110', '-y', '30', '-c', str(repo),
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
    sandbox.key('PageDown')
    baseline = sandbox.frame('keyboard-scroll', 'more above', 'move the cursor up')
    sandbox.key('g')
    sandbox.frame('keyboard-scroll-reset', 'move the cursor up')
    # A wheel outside the dialog must be consumed, never interpreted by the rail.
    wheel = b'\x1b[<65;2;15M'
    sandbox.tmux('send-keys', '-H', '-t', 'smoke:0.0', *[f'{byte:02x}' for byte in wheel])
    # Ordered key response is the acknowledgement that the preceding mouse event was handled.
    sandbox.key('PageDown')
    sandbox.wait('wheel-consumed-keyboard-scroll', sandbox.capture, lambda frame: frame == baseline)
    sandbox.key('Escape')
    sandbox.frame('help-closed', 'A G E N T', '? Keys')
    sandbox.key('g')
    sandbox.frame('group-form', 'New Group')
    sandbox.tmux('send-keys', '-l', '-t', 'smoke:0.0', 'e2e-group')
    sandbox.frame('group-form-name', 'e2e-group')
    sandbox.key('Enter')
    sandbox.frame('group-created', 'e2e-group', 'New Group')
    sandbox.key('T')
    sandbox.frame('terminal-row', 'terminal-')
    panes = sandbox.wait('terminal-panes',
                        lambda: sandbox.tmux('list-panes', '-a', '-F',
                            '#{pane_id} #{pane_current_command}', socket='agentmgr', check=False).stdout,
                        lambda value: len(value.splitlines()) == 1 and len(value.split()) == 2)
    pane = panes.split()[0]
    sandbox.wait('shell-prompt',
                 lambda: sandbox.tmux('capture-pane', '-p', '-t', pane, socket='agentmgr').stdout,
                 lambda frame: sandbox.prompt.rstrip() in frame)
    # A unique file proves the actual shell accepted a command, not a footer label or prompt.
    acknowledgement = sandbox.home / 'shell-ack'
    sandbox.tmux('send-keys', '-l', '-t', pane,
                 f'printf shell-ready > {shlex.quote(str(acknowledgement))}', socket='agentmgr')
    sandbox.tmux('send-keys', '-t', pane, 'Enter', socket='agentmgr')
    sandbox.wait('shell-ready', lambda: acknowledgement.read_text() if acknowledgement.exists() else '',
                 lambda value: value == 'shell-ready')
    feature_flows(sandbox, pane)
    sandbox.key('C-c')
    sandbox.wait('manager-exit', lambda: sandbox.tmux('display-message', '-p', '-t', 'smoke:0.0',
                  '#{pane_dead} #{pane_dead_status}').stdout.strip(), lambda value: value == '1 0')


def feature_flows(sandbox, pane):
    sandbox.key('C-r')
    sandbox.frame('review-diff', 'after_review_e2e')
    sandbox.key('?')
    sandbox.frame('review-help', '? Review keys')
    sandbox.key('Escape')
    sandbox.frame('review-help-closed', 'after_review_e2e', '? Review keys')
    sandbox.key('Escape')
    sandbox.frame('review-closed', 'A G E N T', 'after_review_e2e')
    sandbox.key('Right')
    sandbox.frame('focus-open', 'focused · ctrl+q')
    acknowledgement = sandbox.home / 'focus-ack'
    sandbox.tmux('send-keys', '-l', '-t', 'smoke:0.0',
                 f'printf focus-ready > {shlex.quote(str(acknowledgement))}')
    sandbox.key('Enter')
    sandbox.wait('focused-command', lambda: acknowledgement.read_text() if acknowledgement.exists() else '',
                 lambda value: value == 'focus-ready')
    sandbox.wait('focused-pane-command',
                 lambda: sandbox.tmux('capture-pane', '-p', '-t', pane, socket='agentmgr').stdout,
                 lambda frame: 'printf focus-ready' in frame)
    sandbox.key('C-q')
    sandbox.frame('focus-closed', 'Shell', 'focused · ctrl+q')
    sandbox.key('/')
    sandbox.frame('rail-search-open', 'type to filter')
    # The rail accepts individual key events; acknowledge each before sending the next.
    for query in ('z', 'zz'):
        sandbox.tmux('send-keys', '-l', '-t', 'smoke:0.0', 'z')
        sandbox.frame('rail-search-' + query, '⌕ ' + query)
    sandbox.wait('rail-search-filtered', sandbox.capture,
                 lambda frame: 'terminal-' not in rail_text(frame))
    sandbox.key('Escape')
    sandbox.wait('rail-search-cleared', sandbox.capture,
                 lambda frame: 'terminal-' in rail_text(frame) and 'Group' in frame and '⌕ zz' not in frame)
    sandbox.key('Down')
    sandbox.wait('rail-group-selected', sandbox.capture,
                 lambda frame: 'e2e-group' in '\n'.join(line[35:] for line in frame.splitlines()))
    sandbox.key('Left')
    sandbox.wait('rail-folded', sandbox.capture,
                 lambda frame: 'e2e-group' in rail_text(frame) and 'terminal-' not in rail_text(frame))
    sandbox.key('Right')
    sandbox.wait('rail-unfolded', sandbox.capture,
                 lambda frame: 'terminal-' in rail_text(frame))


def rail_text(frame):
    return '\n'.join(line[:35] for line in frame.splitlines())


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
    except (Exception, KeyboardInterrupt) as error:
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

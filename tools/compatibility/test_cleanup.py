import os
import pathlib
import subprocess
import tempfile
import shutil
import unittest

SCRIPT = pathlib.Path(__file__).with_name("two_manager.sh")

class CleanupSafetyTest(unittest.TestCase):
    def test_missing_socket_directories_never_use_named_default(self):
        source = SCRIPT.read_text()
        cleanup = source[source.index("cleanup() {"):source.index("trap cleanup EXIT")]
        with tempfile.TemporaryDirectory(prefix="am-cleanup-test-") as tmp:
            root = pathlib.Path(tmp)
            log = root / "calls"
            tools = root / "bin"
            tools.mkdir()
            for name in ("tmux", "pkill"):
                wrapper = tools / name
                wrapper.write_text("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALL_LOG\"\n")
                wrapper.chmod(0o755)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"], CALL_LOG=str(log))
            context = 'work="$1"; SLOW_SWITCH="$work/slow.switch"; socketA="$work/tmxa/tmux-0/agentmgr"; socketB="$work/tmxb/tmux-0/agentmgr"; REAL_TMUX=tmux; observer_pid=""; a_pid=""; b_pid=""; '
            subprocess.run(["bash", "-c", context + cleanup + "\ncleanup", "test", str(root)], env=env, check=True)
            calls = log.read_text().splitlines()
            self.assertEqual(calls, ["-S " + str(root / "tmxa/tmux-0/agentmgr") + " kill-server", "-S " + str(root / "tmxb/tmux-0/agentmgr") + " kill-server"])

    def test_cleanup_stops_only_captured_manager_pid(self):
        source = SCRIPT.read_text()
        cleanup = source[source.index("cleanup() {"):source.index("trap cleanup EXIT")]
        with tempfile.TemporaryDirectory(prefix="am-pid-test-") as tmp:
            root = pathlib.Path(tmp)
            owned = subprocess.Popen(["sleep", "60"])
            unrelated = subprocess.Popen(["sleep", "60"])
            try:
                (root / "manager-a.pid").write_text(str(owned.pid) + "\n")
                context = 'work="$1"; SLOW_SWITCH="$work/slow"; socketA="$work/missing-a/socket"; socketB="$work/missing-b/socket"; REAL_TMUX=true; '
                subprocess.run(["bash", "-c", context + cleanup + "\ncleanup", "test", str(root)], check=True)
                owned.wait(timeout=5)
                self.assertIsNone(unrelated.poll())
            finally:
                for process in (owned, unrelated):
                    if process.poll() is None:
                        process.terminate()
                    process.wait(timeout=5)

    @unittest.skipUnless(shutil.which("tmux"), "tmux unavailable")
    def test_missing_fixture_paths_preserve_unrelated_real_server(self):
        source = SCRIPT.read_text()
        cleanup = source[source.index("cleanup() {"):source.index("trap cleanup EXIT")]
        with tempfile.TemporaryDirectory(prefix="am-safe-", dir="/tmp") as tmp:
            root = pathlib.Path(tmp)
            sentinel = str(root / "sentinel")
            env = dict(os.environ)
            env.pop("TMUX", None)
            subprocess.run(["tmux", "-f", "/dev/null", "-S", sentinel, "new-session", "-d", "-s", "sentinel", "sleep 60"], env=env, check=True)
            try:
                env["TMUX"] = sentinel + ",0,0"
                context = 'work="$1"; SLOW_SWITCH="$work/slow"; socketA="$work/missing-a/socket"; socketB="$work/missing-b/socket"; REAL_TMUX=tmux; '
                subprocess.run(["bash", "-c", context + cleanup + "\ncleanup", "test", str(root)], env=env, check=True)
                result = subprocess.run(["tmux", "-S", sentinel, "has-session", "-t", "sentinel"], capture_output=True)
                self.assertEqual(result.returncode, 0)
            finally:
                subprocess.run(["tmux", "-S", sentinel, "kill-server"], capture_output=True)

    def test_script_has_no_broad_process_name_cleanup(self):
        self.assertNotIn("pkill", SCRIPT.read_text())

if __name__ == "__main__":
    unittest.main()

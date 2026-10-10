# Disposable compatibility checks

Build the candidate once, then run the separate matrix and slower process gate:

```sh
go build -o /tmp/agent-manager-candidate .
python3 -m unittest discover -s tools/compatibility -p 'test_*.py'
python3 tools/compatibility/matrix.py --binary /tmp/agent-manager-candidate
python3 tools/compatibility/matrix.py --binary /tmp/agent-manager-candidate --release v0.38.0
bash tools/compatibility/two_manager.sh /tmp/agent-manager-candidate
```

The matrix downloads v0.39.0 for the current macOS/Linux architecture, verifies
its release checksum before extracting the binary, and records the version,
archive hash, results and gaps. CLI/MCP runs have isolated profiles; the sequential mixed-version task checks
share only their own disposable profile. Concurrent checks create 16 tasks
through both binaries and race six claims, asserting one winner and matching
stored ownership through both readers. They cover task writes, not every
mutation family. The runner
seeds a disposable caller identity, since caller discovery through process
ancestry must not authorize the developer's real session inside a test profile.

The process gate takes about 90 seconds, including real heartbeat aging and
reclamation. It starts the managers on PTYs as owned subprocesses so pause and
cleanup target exact PIDs. All tmux calls use absolute fixture socket paths.
Results and logs remain at the printed artifact directory on failure.

Passing these checks proves only the named local contracts. Installed client
extensions, mixed-version concurrent writes beyond tasks, in-flight delivery fencing and
real SSH remain separate rollout requirements. No production authority or schema
change is introduced by this harness.

To compare an explicitly selected development build, pass
`--peer-binary /absolute/path/agent-manager`. The result records its binary hash
instead of claiming a released archive checksum. Copy the executable first if
its installed path could be replaced during the run; all profile state remains
inside the artifact directory.

The [in-flight delivery probe](../../docs/architecture/in-flight-delivery.md)
reproduces a paused sender past claim retirement. Its passing terminal-state
test is not an exclusive-authority gate.

Automatic delivery now uses the [profile guard and token receipts](../../docs/architecture/delivery-ownership.md).
The task matrix does not authorize old delivery writers/readers to coexist after
migration. Offline cutover must stop their already-admitted transport first;
legacy automatic claim SQL is refused after the new schema fences install.

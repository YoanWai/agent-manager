# In-flight delivery and claim retirement

The recovered architecture does not fence a paused transport write after another
manager retires its claim. This blocks a pilot with multiple managers on one
profile. Released CLI task compatibility does not remove this limitation.

## Reproduce the boundary

```sh
mkdir -p /tmp/am-inflight-probe
env -u TMUX TMUX_TMPDIR=/tmp/am-inflight-probe SHELL=/bin/sh \
  go test -race ./internal/execution \
  -run TestInboxRetirementDuringInFlightSendPreservesTerminalState -count=1 -v
```

The test uses two Runner objects, two independent SQLite connections and a real
managed fixture pane. A fixture tmux wrapper pauses only `load-buffer`, after
Runner A has durably claimed the inbox row. The test ages that row, lets Runner B
retire it through the production delivery method, then releases A. No production
clock or transport seam is introduced. This is an execution-boundary probe, not
a mixed-release two-process rollout test.

The 2026-10-01 run reported:

```text
retirement probe: late_paste=true runner_reported_sent=true send_error=<nil>
```

The row retained its original dropped and terminal timestamps. The test asserts
that durable terminal state cannot be overwritten by a late completion; it does
not assert exclusive delivery authority. Its passing result is therefore not
acceptance of concurrent execution owners. The log reports the separate actual
transport outcome.

## Why heartbeat and claim tests are insufficient

`ClaimMessage` admits one writer atomically. Retirement marks an old claim
terminal and removes it from the queue. Neither action revokes a transport write
already in progress. `MarkDelivered` matches only rows without a terminal stamp,
but zero affected rows is returned as success. A paused original writer can
therefore resume after retirement, type the message and report success while its
sender observes a dropped row.

The same claim/send/terminal-record sequence exists in upstream `dc471a9`, in
`internal/ui/poller.go` lines 1017-1041. The architecture migration moved it into
`internal/execution/polldelivery.go`; source inspection establishes inheritance,
not a released-process reproduction.

## Required follow-up

The delivery contract needs to distinguish confirmed, refused and uncertain
outcomes. A final database read before paste narrows the race but cannot close
it; retirement can happen after that read. A process generation alone also
cannot revoke a tmux command that has already passed its check.

A production design must coordinate claim retirement with the actual transport
commit boundary, preserve bounded shutdown and define how historical writers
are excluded. Review pending-input delivery against the same contract. Prove
paused-write retirement, owner replacement, send failure, response loss and
old-writer coexistence before claiming exclusive authority. Keep this change
separate from UI ownership and file organization.

For an initial real-session pilot, use one manager process per profile, stop it
before switching binaries, and retain a rollback copy of profile state. Wider
platform, installed-client and provider acceptance remains on the roadmap.

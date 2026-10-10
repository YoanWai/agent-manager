#!/bin/sh

set -eu

repo_root=$(CDPATH='' cd "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

for tool in go tmux; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'fast gate requires %s on PATH\n' "$tool" >&2
		exit 1
	fi
done

fixture_root=$(mktemp -d /tmp/am-fast.XXXXXX)
passed=false
cleanup() {
	if [ "$passed" = true ]; then
		rm -rf "$fixture_root"
	else
		printf 'fast gate failed; retained fixture: %s\n' "$fixture_root" >&2
	fi
}
trap cleanup EXIT

mkdir -p "$fixture_root/zsh"
export TMUX_TMPDIR="$fixture_root"
export ZDOTDIR="$fixture_root/zsh"
export SHELL=/bin/sh
unset TMUX TMUX_PANE

printf '%s\n' '[1/2] pure policy, extracted UI feature, SQLite, and architecture suites'
go test -race -count=1 \
	./internal/agentsession \
	./internal/config \
	./internal/diff/model \
	./internal/status \
	./internal/store \
	./internal/ui/help \
	./internal/ui/review \
	./internal/ui/focus \
	./internal/ui/rail \
	./internal/ui/presentation \
	./tools/architecture/check-ui-boundaries \
	./tools/architecture/check-ui-moves

store_session_tests='^(TestTasksAreClaimedByExactlyOneSession|TestDependenciesGateAClaimUntilTheyAreDone|TestOnlyUnfinishedDependenciesAreNamedAsBlocking|TestReleasedAndDeletedTasksLeaveTheList|TestDeletingASessionHandsItsClaimsBack|TestRacingClaimsOnOneTaskLeaveASingleWinner|TestRacingSessionsSplitTheListWithoutSharingATask|TestReservationsSurfaceOverlapWithoutBlockingIt|TestSharedLeasesOnlyClashWithExclusiveOnes|TestALapsedLeaseStopsBlockingAndReleaseClearsTheRest|TestReleasingBlankPathsDoesNotDropEveryLease|TestDeleteGroupTakesItsSubtreeAndKeepsNesting)$'

printf '%s\n' '[2/2] store-only session command policies and transactional claim races'
session_log="$fixture_root/sessioncmd.log"
if go test -race -count=1 -v ./internal/sessioncmd -run "$store_session_tests" >"$session_log" 2>&1; then
	status=0
else
	status=$?
fi
cat "$session_log"
if [ "$status" -ne 0 ]; then
	exit "$status"
fi
selected=$(awk '$1 == "===" && $2 == "RUN" && $3 !~ /\// { count++ } END { print count + 0 }' "$session_log")
if [ "$selected" -ne 12 ]; then
	printf 'session command selection ran %s tests; expected 12\n' "$selected" >&2
	exit 1
fi

passed=true
printf '%s\n' 'fast gate passed; run tools/testing/process-race.sh before the full suite when process boundaries change'

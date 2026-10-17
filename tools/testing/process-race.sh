#!/bin/sh

set -eu

repo_root=$(CDPATH='' cd "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

for tool in go tmux; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'process/race matrix requires %s on PATH\n' "$tool" >&2
		exit 1
	fi
done

run_phase() {
	slug=$1
	label=$2
	package=$3
	pattern=$4
	expected=$5
	fixture_root=$(mktemp -d "/tmp/am-${slug}.XXXXXX")
	mkdir -p "$fixture_root/zsh"
	phase_log="$fixture_root/go-test.log"

	printf '\n[%s]\n' "$label"
	if env -u TMUX -u TMUX_PANE \
		TMUX_TMPDIR="$fixture_root" \
		ZDOTDIR="$fixture_root/zsh" \
		SHELL=/bin/sh \
		go test -race -count=1 -v "$package" -run "$pattern" >"$phase_log" 2>&1; then
		status=0
	else
		status=$?
	fi
	cat "$phase_log"
	if [ "$status" -eq 0 ]; then
		selected=$(awk '$1 == "===" && $2 == "RUN" && $3 !~ /\// { count++ } END { print count + 0 }' "$phase_log")
		if [ "$selected" -ne "$expected" ]; then
			printf 'phase ran %s tests; expected %s\n' "$selected" "$expected" >&2
			status=1
		fi
	fi
	if [ "$status" -eq 0 ]; then
		rm -rf "$fixture_root"
		return 0
	fi

	printf 'phase failed; retained fixture: %s\n' "$fixture_root" >&2
	return "$status"
}

sessioncmd_tests='^(TestSessionsCreateCarriesNamePromptAndTargetWithRealTmux|TestSessionsKillKeepsTheScreenAndReviveBringsItBack|TestSessionsSendAndReadReachTheTargetPane|TestTerminalsCreateListSendAndReadWithRealTmux|TestDeleteGroupMovesItsSessionsToTheRoot)$'
execution_tests='^(TestSecondManagerTakesOverOnlyWhenTheFirstAges|TestBlockedSubscriberDoesNotBlockCancellation|TestDeliveryGuardKeepsPeerFromRetiringAnInFlightSend|TestInboxRecordsAPostPasteFailureAsUncertain|TestPendingDeliverySkipsWhileAnotherOwnerHoldsTheGuard|TestOwnerlessClaimIsMarkedUncertainNotRedelivered)$'
ui_tests='^(TestSpawnWorkerDefersFilesystemReadAndCapturesDraft|TestAcceptedSpawnSurvivesChangedDraftWithoutClosingIt|TestAcceptedGroupDrainsBlockedValidationOnQuit|TestAcceptedTerminalDrainsBlockedDirectoryOnQuit|TestSpawnUpdateReturnsWhileWriterIsQueued|TestForkUpdateReturnsWhileWorkerIsBlocked|TestSettingsSavePartialFailureIsDurableAndHonest|TestStaleNormalizeDoesNotOverwriteNewerSave|TestEffectLifetimeWaitsForRunningWork|TestBlockedInputWorkerKeepsUpdateResponsiveAndDrainsOnQuit|TestInputPasteCannotBeOvertakenByEnter|TestInputKeysUseServingControlWithoutDriverFallback|TestControlKeyAcknowledgementPrecedesPasteTransport|TestFocusWatchForwardDistinguishesUnavailableFromAdmitted|TestWheelReachesMouseTrackingApp|TestInstallStartDefersWorkOffUpdate|TestInstallStartKeepsUncertainSendWithoutBlindRetry|TestInstallBlocksQuitUntilPendingRetrySettles|TestInstallSettleDefersStatusAndBinaryChecks|TestInstallSettleKeepsTrackerOnTransientStatusReadFailure|TestInstallSettleRetriesTransientInstalledCheck|TestInstallSettleKeepsTrackerWhenShellLivenessIsUnknown|TestQuickSendWorkerClassifiesTransportAndRecordsOnlyConfirmed|TestBlockedQuickSendsKeepUpdateResponsiveRunFIFOAndDrainOnQuit|TestReviewPreferencesCannotRetargetReopenedReview|TestReviewPreferencesFinishBehindHelpWithoutStealingIt|TestReviewBaseSavePersistsButDoesNotReloadStaleOrQuittingReview|TestReviewSetBaseWorkerUsesCapturedValues|TestDismissNoticeFailureDoesNotReopenAfterNewerInput|TestDismissNoticeFailureDuringQuitRestoresWithoutReopening|TestNewerSplitSaveSuccessClearsOnlySplitFailure|TestCtrlCFromResizePersistsRatioAfterAcceptedQuit|TestInstallerRefusedQuitKeepsMouseDrag|TestReviewEditorCompletionCannotCrossHelpOrQuit|TestEditorReturnCompletionCannotCrossHelpOrQuit|TestPreparedAttachDoesNotReplaceNewerHelp|TestAttachReportsTransportFailureWithoutCallingPaneDead)$'
tmux_tests='^(TestControlAttachSurvivesConcurrentPastes|TestAttachGateHoldsAnotherProcess|TestControlCloseTimeoutDoesNotBypassAnActiveHandshake|TestControlCommandWaitsForAnotherProcessHandshake|TestRepeatedControlCloseWaitsForAnotherProcessHandshake|TestPrepareAttachRestoresAutoSize|TestSendText|TestLifecycle|TestSendTextContextKillsAndReapsTransportBeforeReturning|TestSendTextContextClassifiesLoadFailureAsRefused|TestSessionExistsContextSeparatesAbsenceFromTransportFailure)$'

run_phase process-sessioncmd '1/4 session commands: real panes, lifecycle, messages, terminals, live group member' ./internal/sessioncmd "$sessioncmd_tests" 5
run_phase process-execution '2/4 execution: cancellation, takeover, guarded delivery, and uncertain outcomes' ./internal/execution "$execution_tests" 6
run_phase process-ui '3/4 UI: accepted preflights, blocked adapters, acknowledged input/install/quick effects, async editor and persistence boundaries, quit policy, partial writes, and stale fences' ./internal/ui "$ui_tests" 37
run_phase process-tmux '4/4 tmux: control clients, cross-process handshake gates, typed liveness, cancellation/reaping, send, resize, and lifecycle' ./internal/tmux "$tmux_tests" 11

printf '\n%s\n' 'process/race matrix passed; the full race suite and release-specific gates remain required'

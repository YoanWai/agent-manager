#!/bin/bash
# Interface tests for the Linux delivery module (linux/cmux-notify).
# Sandboxed: each call runs with HOME=<tmp>, a fake relay, desktop disabled,
# and CMUX_NOTIFY_WORKSPACE="" (empty pin = explicitly untargeted, and it
# short-circuits the live agent-manager probe so tests never leak the real
# workspace id).
set -u
SCRIPT="$HOME/.local/bin/cmux-notify"
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf 'ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf 'FAIL %s\n' "$1"; }
check(){ # name expected actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1"; printf '  expected: %s\n  actual:   %s\n' "$2" "$3"; fi
}

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
STATE="$T/.local/state/cmux-notify"
mkdir -p "$T/.cmux/bin"
cat > "$T/.cmux/bin/cmux" <<EOF
#!/bin/bash
ec=\${CMUX_FAKE_EXIT:-0}
case " \$* " in
  *" FAILME "*) ec=1 ;;
  *"--workspace W-DEAD"*) ec=1 ;;
esac
if [ "\$ec" = 0 ]; then
  printf 'SOCK=%s %s\n' "\${CMUX_SOCKET_PATH:-none}" "\$*" >> "$T/relay.log"
fi
exit "\$ec"
EOF
chmod +x "$T/.cmux/bin/cmux"

run() { # args... -> cmux-notify with sandbox env (CMUX_FAKE_EXIT pass-through)
  HOME=$T CMUX_NOTIFY_DESKTOP=0 CMUX_NOTIFY_WORKSPACE="" \
  CMUX_FAKE_EXIT="${CMUX_FAKE_EXIT:-0}" CMUX_NOTIFY_MAX_SPOOL="${CMUX_NOTIFY_MAX_SPOOL:-200}" \
  bash "$SCRIPT" "$@" 2>/dev/null
}
reset() { rm -f "$T/relay.log"; rm -rf "$STATE"; }

# 1. basic send, untargeted (empty pin)
reset
run "t1" "b1"
check "basic untargeted send" "SOCK=none notify --title t1 --body b1" "$(cat "$T/relay.log")"

# 2. env-pin targeting
reset
HOME=$T CMUX_NOTIFY_DESKTOP=0 CMUX_NOTIFY_WORKSPACE=W-42 bash "$SCRIPT" "t2" "b2" 2>/dev/null
check "env-pin adds --workspace" "SOCK=none notify --workspace W-42 --title t2 --body b2" "$(cat "$T/relay.log")"

# 3. flag targeting
reset
run --workspace W-99 "t3" "b3"
check "flag adds --workspace" "SOCK=none notify --workspace W-99 --title t3 --body b3" "$(cat "$T/relay.log")"

# 4. empty pin = explicitly untargeted (no probe leakage)
reset
run "t4" "b4"
case "$(cat "$T/relay.log")" in *--workspace*) bad "empty pin stays untargeted" ;; *) ok "empty pin stays untargeted" ;; esac

# 5. total failure spools 4-field TSV (empty route, empty ws)
reset
CMUX_FAKE_EXIT=1 run "first" "one"
check "spool 4-field untargeted" "first	one		" "$(cat "$STATE/spool")"

# 6. spooled event keeps its workspace
reset
HOME=$T CMUX_NOTIFY_DESKTOP=0 CMUX_NOTIFY_WORKSPACE=W-42 CMUX_FAKE_EXIT=1 \
  bash "$SCRIPT" "r2" "b2" 2>/dev/null
check "spool stores workspace" "r2	b2		W-42" "$(cat "$STATE/spool")"

# 7. ws-replay: flush re-sends with the stored workspace (tab-collapse regression)
reset
mkdir -p "$STATE"; printf 'r2\tb2\t\tW-42\n' > "$STATE/spool"
run --flush
check "ws-replay uses stored workspace" "SOCK=none notify --workspace W-42 --title r2 --body b2" "$(cat "$T/relay.log")"

# 8. flush drains in FIFO order
reset
mkdir -p "$STATE"; printf 'e1\tb1\t\t\ne2\tb2\t\t\n' > "$STATE/spool"
run --flush
check "flush FIFO order" "SOCK=none notify --title e1 --body b1
SOCK=none notify --title e2 --body b2" "$(cat "$T/relay.log")"
check "spool empty after flush" "" "$(cat "$STATE/spool" 2>/dev/null)"

# 9. stale workspace falls back to untargeted once
reset
mkdir -p "$STATE"; printf 's\tb\t\tW-DEAD\n' > "$STATE/spool"
run --flush
check "stale ws retried untargeted" "SOCK=none notify --title s --body b" "$(cat "$T/relay.log")"
check "stale ws not respooled" "" "$(cat "$STATE/spool" 2>/dev/null)"

# 10. mid-flush failure respools remainder in order, unattempted
reset
mkdir -p "$STATE"; printf 'g1\tb1\t\t\nFAILME\tb2\t\t\ng2\tb3\t\t\n' > "$STATE/spool"
run --flush
check "mid-flush: head delivered" "SOCK=none notify --title g1 --body b1" "$(cat "$T/relay.log")"
check "mid-flush: remainder respooled in order" "FAILME	b2		
g2	b3		" "$(cat "$STATE/spool")"

# 11. codex JSON body reduced to last-assistant-message
reset
run "codex" '{"type":"agent-turn-complete","last-assistant-message":"hello world"}'
check "JSON body normalized" "SOCK=none notify --title codex --body hello world" "$(cat "$T/relay.log")"

# 12. body collapsed to one line and capped at 160 (+"...")
reset
LONG=$(printf 'a%.0s' $(seq 1 200))
run "cap" "$LONG"
body=$(sed 's/.*--body //' "$T/relay.log")
check "body capped at 160+ellipsis" "163" "${#body}"
reset
run "multi" "$(printf 'line1\nline2')"
check "multiline collapsed" "SOCK=none notify --title multi --body line1 line2" "$(cat "$T/relay.log")"

# 13. spool cap keeps newest MAX_SPOOL entries
reset
CMUX_NOTIFY_MAX_SPOOL=3; export CMUX_NOTIFY_MAX_SPOOL
for i in 1 2 3 4 5; do CMUX_FAKE_EXIT=1 run "n$i" "x"; done
check "spool capped at 3" "n4	x		
n5	x		" "$(tail -2 "$STATE/spool")"
check "spool has exactly 3 lines" "3" "$(wc -l < "$STATE/spool")"

echo
echo "pass=$PASS fail=$FAIL"
[ "$FAIL" -eq 0 ]

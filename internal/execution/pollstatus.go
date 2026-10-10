package execution

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"

	"github.com/charmbracelet/x/ansi"
	"hash/fnv"
)

// derivePaneStatus turns one captured pane into a session status. The
// capture carries ANSI escapes for the preview; rules match against the
// stripped text. Streaming output often renders without any spinner, so
// when no rule matches but the content region above the input box changed
// since the previous poll, the session counts as working. The reverse
// transition closes marker-less turns: a session that was mid-turn whose
// region stopped changing has ended its turn even when the tool printed
// no turn_end line, so the region's last content line decides finished
// versus waiting. A matched working verdict gets the same treatment after
// a longer stability window, since a spinner row can outlive its turn
// when the turn died before printing any end marker. Finished is an
// alert: entering the session acknowledges it (acked), and the pane keeps
// deriving finished until the next turn, so acked maps it back to idle.
//
// A missing prior hash (first observation, or post-resize rebaseline)
// never invents working and never collapses finished/waiting to the tool
// default: the stored status holds until the next poll has a baseline.
func (p *Runner) derivePaneStatus(sess store.Session, pane string, agentAlive bool, paneHashes map[string]uint64) (string, error) {
	text := ansi.Strip(pane)
	region, hasRegion := p.engine.ActivityRegion(sess.Tool, text)
	var regionHash uint64
	if hasRegion {
		regionHash = hashString(p.engine.RegionContent(sess.Tool, region))
		paneHashes[sess.ID] = regionHash
	}
	if p.statusSources[sess.Tool] == hooks.StatusSourceClaude {
		if !agentAlive {
			// The agent died without its SessionEnd cleanup hook
			// (crash, SIGKILL); a stale file must not mask the pane.
			if err := p.hooks.Remove(sess.ID); err != nil {
				return "", err
			}
		} else if hookStatus, ok := p.hooks.Read(sess.ID); ok {
			return p.applyHookStatus(sess, text, hookStatus), nil
		}
	}
	newStatus, matched := p.engine.Match(sess.Tool, text)
	stuck := matched && newStatus == status.Working
	if hasRegion && (!matched || stuck) {
		if previous, seen := p.paneHashes[sess.ID]; seen {
			if previous != regionHash {
				if !matched {
					newStatus = status.Working
				}
				delete(p.quietSince, sess.ID)
			} else if turnInFlight(sess.Status) {
				if sess.Status != status.Working {
					// Already resting: re-infer finished vs waiting without
					// delay, unless the rules now read the pane as working.
					if !stuck {
						newStatus = p.engine.TurnEndedState(sess.Tool, region)
					}
				} else {
					// Mid-turn pauses (thinking, between tools) look quiet for
					// a poll or two; wait before treating that as turn end.
					grace := quietEndGrace
					var paneHash uint64
					if stuck {
						grace = stuckEndGrace
						paneHash = hashString(text)
					}
					now := time.Now()
					timer, ok := p.quietSince[sess.ID]
					if !ok || timer.stuck != stuck || timer.pane != paneHash {
						timer = quietTimer{since: now, stuck: stuck, pane: paneHash}
						p.quietSince[sess.ID] = timer
					}
					if now.Sub(timer.since) >= grace {
						newStatus = p.engine.TurnEndedState(sess.Tool, region)
						if newStatus != status.Working {
							delete(p.quietSince, sess.ID)
						}
					} else {
						newStatus = status.Working
					}
				}
			}
		} else if turnInFlight(sess.Status) && !stuck {
			newStatus = sess.Status
		}
	} else {
		delete(p.quietSince, sess.ID)
	}
	if newStatus == status.Finished && sess.Acked {
		newStatus = status.Idle
	}
	return newStatus, nil
}

// turnInFlight reports whether a status means a turn is running or resting
// unacknowledged. Only then can a quiet region mean the turn just ended;
// finished and waiting stay in the set so the inferred status persists
// across polls instead of collapsing to idle on the next pass.
func turnInFlight(current string) bool {
	return current == status.Working || current == status.Finished || current == status.Waiting
}

// applyHookStatus trusts the hook-reported status over pane heuristics
// for the states hooks can see. They cannot see a plain-text question,
// an interrupt banner, an error line, or work that outlives the turn that
// started it, so a matched pane verdict upgrades finished to waiting,
// errored or working (Stop fires when the main agent stops responding,
// which leaves background agents reported as finished while they run),
// and working to waiting (an Esc interrupt fires no Stop event). A
// working hook also reconciles to
// the pane verdict when the pane shows the turn already ended: background
// subagents write working via PreToolUse/PostToolUse but fire no Stop
// when they finish, so the file would otherwise stay pinned at working
// forever. The pane only reports finished/waiting/errored once the newest
// turn is quiet, so this never fires while the agent is still streaming.
func (p *Runner) applyHookStatus(sess store.Session, text, hookStatus string) string {
	paneStatus, matched := p.engine.Match(sess.Tool, text)
	switch hookStatus {
	case status.Finished:
		if matched && (paneStatus == status.Waiting || paneStatus == status.Errored || paneStatus == status.Working) {
			return paneStatus
		}
		if sess.Acked {
			return status.Idle
		}
	case status.Working:
		if matched && (paneStatus == status.Waiting || paneStatus == status.Finished || paneStatus == status.Errored) {
			if paneStatus == status.Finished && sess.Acked {
				return status.Idle
			}
			return paneStatus
		}
	case status.Errored:
		if matched && (paneStatus == status.Waiting || paneStatus == status.Finished || paneStatus == status.Working) {
			if paneStatus == status.Finished && sess.Acked {
				return status.Idle
			}
			return paneStatus
		}
	}
	return hookStatus
}

func hashString(s string) uint64 { h := fnv.New64a(); h.Write([]byte(s)); return h.Sum64() }

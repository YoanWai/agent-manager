// Package quick is the docked prompt bar: its tool and choice, the list
// open above its prompt, its keys, clicks and view, and the answer or spawn
// enter asks for. The root opens it, owns its prompt box, moves its target
// and runs what its Request asks for.
package quick

import (
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Target is the selected row the bar answers or spawns into.
type Target struct {
	Group   string
	IsGroup bool
}

// Host is what the bar reads from the root: the choice facts, the status
// bar, the prompt box it owns, and the selected row.
type Host interface {
	spawn.ChoiceHost
	ClearErr()
	Prompt() spawn.Prompt
	Target() (Target, bool)
}

// Request is root work a bar key or click asks for once the bar has taken
// its own share. At most one is set.
type Request struct {
	// Move steps the list cursor, which retargets the bar.
	Move int
	// Toggle flips the worktree choice once the target is known to host one.
	Toggle bool
	// Catalog names the CLI the bar moved to, whose choices are new.
	Catalog string
	// Close dismisses the bar; its prompt has let go of its images.
	Close bool
	// Send answers the selected session with Text.
	Send bool
	Text string
	// Spawn starts a session in the selected group.
	Spawn *spawn.Request
	// LoadSaved asks for the catalog the saved prompts list opened on;
	// InsertSaved puts the picked one at the caret as a chip.
	LoadSaved   bool
	InsertSaved *spawn.Saved
}

// The list open above the prompt, if any.
const (
	PickNone = iota
	PickModel
	PickEffort
)

// Control keys, since alt never arrives from many terminals, and ones the
// prompt's editor, the manager and the common multiplexers leave free.
const (
	ModelKey   = "ctrl+l"
	EffortKey  = "ctrl+x"
	ProfileKey = "ctrl+y"
)

// Bar is the quick bar: active across cursor moves, so the target follows
// the selection. The tool is the spawn CLI for group targets, cycled with
// tab.
type Bar struct {
	toolNames []string
	toolIndex int
	worktree  bool
	// worktreeTouched marks an explicit toggle this run; until then the
	// hint and spawn follow the target group's default.
	worktreeTouched bool
	// defaultsTouched protects an explicit tool or worktree choice from the
	// external settings refresh queued when this quick bar opened.
	defaultsTouched bool
	choice          spawn.Choice
	// picking is the list open above the prompt, which takes the typing.
	picking int
	// savedList is the saved prompts list ctrl+o opens above the prompt.
	savedList spawn.SavedList
	// hits are relative to the bar's first painted line.
	hits []Hit
}

// NewBar opens on names[index], its worktree toggle at the target's default.
func NewBar(h spawn.ChoiceHost, names []string, index int, worktree bool) Bar {
	return Bar{
		toolNames: names,
		toolIndex: index,
		worktree:  worktree,
		choice:    spawn.NewChoice(h, names[index]),
	}
}

// Tool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (q *Bar) Tool() string {
	if len(q.toolNames) == 0 {
		return ""
	}
	return q.toolNames[q.toolIndex]
}

// Tools are the CLIs tab steps through, and the picked one's index.
func (q *Bar) Tools() ([]string, int) { return q.toolNames, q.toolIndex }

// SetTools takes a newer tool list and pick, leaving the choice be.
func (q *Bar) SetTools(names []string, index int) {
	q.toolNames, q.toolIndex = names, index
}

// RestartChoice starts the choice over on the current CLI.
func (q *Bar) RestartChoice(h spawn.ChoiceHost) { q.choice = spawn.NewChoice(h, q.Tool()) }

// Choice is the model, effort and profile a spawn launches with.
func (q *Bar) Choice() *spawn.Choice { return &q.choice }

// Worktree is the worktree toggle.
func (q *Bar) Worktree() bool { return q.worktree }

// WorktreeTouched says the toggle is an explicit choice this run.
func (q *Bar) WorktreeTouched() bool { return q.worktreeTouched }

// DefaultWorktree moves a toggle the user has not touched.
func (q *Bar) DefaultWorktree(on bool) {
	if !q.worktreeTouched {
		q.worktree = on
	}
}

// SetWorktree is an explicit worktree choice for this run, which the target
// group's default and later settings refreshes leave be.
func (q *Bar) SetWorktree(on bool) {
	q.worktree = on
	q.worktreeTouched = true
	q.defaultsTouched = true
}

// DefaultsTouched says the user chose a tool or worktree since opening.
func (q *Bar) DefaultsTouched() bool { return q.defaultsTouched }

// Picking is the list open above the prompt.
func (q *Bar) Picking() int { return q.picking }

// HandleKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor) unless the
// caret has a prompt row to move to, enter submits against whatever is
// selected, and every other key is typed text. The root has made a
// pasted key typed text already.
func (q *Bar) HandleKey(h Host, msg tea.KeyMsg) (tea.Cmd, Request) {
	if q.savedList.Open() {
		cmd, pick := q.savedList.Key(msg)
		return cmd, savedRequest(pick)
	}
	if q.picking != PickNone {
		// A click can move the target off the group the list was opened for.
		if q.Spawning(h) {
			return q.handlePickKey(h, msg), Request{}
		}
		q.closePick(h)
	}
	prompt := h.Prompt()
	switch msg.String() {
	case "esc":
		// Reopening the bar starts a fresh prompt, so the images this one
		// was holding have nowhere left to be referenced from.
		prompt.Release()
		return nil, Request{Close: true}
	case "up":
		if cmd, stepped := prompt.StepRow(msg); stepped {
			return cmd, Request{}
		}
		return nil, Request{Move: -1}
	case "down":
		if cmd, stepped := prompt.StepRow(msg); stepped {
			return cmd, Request{}
		}
		return nil, Request{Move: 1}
	case "tab", "alt+m":
		return nil, q.cycleTool(h, 1)
	case "shift+tab":
		return nil, q.cycleTool(h, -1)
	case ModelKey:
		q.openPick(h, PickModel)
		return nil, Request{}
	case EffortKey:
		q.stepEffort(h)
		return nil, Request{}
	case ProfileKey:
		q.stepProfile(h)
		return nil, Request{}
	case "ctrl+t", "alt+w":
		return nil, Request{Toggle: true}
	case "ctrl+o":
		q.savedList = spawn.OpenSavedList(nil, true, false)
		return nil, Request{LoadSaved: true}
	case "enter":
		return nil, q.Submit(h)
	}
	if cmd, handled := prompt.ChipKey(msg); handled {
		return cmd, Request{}
	}
	return prompt.Type(msg), Request{}
}

// LoadSaved fills the saved prompts list with the catalog read.
func (q *Bar) LoadSaved(rows []spawn.Saved, err error) {
	if q.savedList.Open() {
		q.savedList.Load(rows, err)
	}
}

// SavedOpen says the saved prompts list is showing, which a catalog read
// that answers late finds closed.
func (q *Bar) SavedOpen() bool { return q.savedList.Open() }

func savedRequest(pick spawn.SavedPick) Request {
	if !pick.Picked {
		return Request{}
	}
	return Request{InsertSaved: &pick.Saved}
}

// Submit answers the selected session, or spawns a new session with the
// prompt embedded when a group is selected.
func (q *Bar) Submit(h Host) Request {
	target, ok := h.Target()
	if !ok {
		h.ReportErr("nothing selected")
		return Request{}
	}
	prompt := h.Prompt()
	if prompt.Pasting() {
		h.ReportErr("still reading the pasted image - try again in a moment")
		return Request{}
	}
	text := prompt.Message()
	if text == "" {
		h.ReportErr("prompt cannot be empty")
		return Request{}
	}
	if target.IsGroup {
		spawn, ok := q.SpawnRequest(h, target.Group, text)
		if !ok {
			return Request{}
		}
		return Request{Spawn: &spawn}
	}
	return Request{Send: true, Text: text}
}

// SpawnRequest validates a spawn of prompt into group and builds it; the
// root names the session, fills in the group's launch inputs and checks
// the directory on the effect lane.
func (q *Bar) SpawnRequest(h spawn.ChoiceHost, group, prompt string) (spawn.Request, bool) {
	toolName := q.Tool()
	if toolName == "" {
		h.ReportErr("no tools configured")
		return spawn.Request{}, false
	}
	picked, err := q.choice.Launch(h, toolName, "")
	if err != nil {
		h.ReportErr(err.Error())
		return spawn.Request{}, false
	}
	return spawn.Request{
		Tool:        toolName,
		Group:       group,
		Prompt:      prompt,
		Pick:        picked,
		Worktree:    q.worktree,
		WorktreeSet: q.worktreeTouched,
	}, true
}

// cycleTool steps the spawn CLI by delta and starts its choices over; the
// root asks the new CLI what it offers.
func (q *Bar) cycleTool(h spawn.ChoiceHost, delta int) Request {
	count := len(q.toolNames)
	if count == 0 {
		return Request{}
	}
	q.toolIndex = (q.toolIndex + delta + count) % count
	q.defaultsTouched = true
	toolName := q.Tool()
	q.choice = spawn.NewChoice(h, toolName)
	return Request{Catalog: toolName}
}

// Spawning says the target is a group. Choices apply to a spawn only,
// never to an answer.
func (q *Bar) Spawning(h Host) bool {
	target, ok := h.Target()
	return ok && target.IsGroup
}

// ChoiceHint is what a choice key says while a session is the target.
const ChoiceHint = "model, effort and profile apply to a new agent: select a group to spawn one"

func (q *Bar) requireSpawn(h Host) bool {
	if q.Spawning(h) {
		return true
	}
	h.ReportErr(ChoiceHint)
	return false
}

func (q *Bar) openPick(h Host, pick int) {
	if !q.requireSpawn(h) {
		return
	}
	toolName, ch := q.Tool(), &q.choice
	if note, listed := spawn.ModelNote(h, toolName, spawn.Styles{}); !listed {
		h.ReportErr("model: " + ansi.Strip(note))
		return
	}
	h.ClearErr()
	q.savedList.Close()
	q.picking = pick
	switch pick {
	case PickModel:
		ch.OpenFilter(h, toolName)
	case PickEffort:
		ch.FocusEffort()
	}
	h.Prompt().Blur()
}

func (q *Bar) closePick(h Host) {
	q.picking = PickNone
	q.choice.Blur()
	h.Prompt().Focus()
}

// stepEffort opens the typed field for a CLI that lists no levels.
func (q *Bar) stepEffort(h Host) {
	if !q.requireSpawn(h) {
		return
	}
	toolName, ch := q.Tool(), &q.choice
	if ch.EffortTyped(h, toolName) {
		q.openPick(h, PickEffort)
		return
	}
	if value, shown, active := ch.EffortRow(h, toolName, spawn.Styles{}); !active {
		if !shown {
			value = "no levels for this model"
		}
		h.ReportErr("effort: " + ansi.Strip(value))
		return
	}
	h.ClearErr()
	ch.CycleEffort(h, toolName, 1)
}

func (q *Bar) stepProfile(h Host) {
	if !q.requireSpawn(h) {
		return
	}
	toolName := q.Tool()
	if !spawn.HasProfiles(h, toolName) {
		h.ReportErr("profile: " + toolName + " has none")
		return
	}
	h.ClearErr()
	q.choice.CycleProfile(h, toolName, 1)
}

func (q *Bar) handlePickKey(h Host, msg tea.KeyMsg) tea.Cmd {
	toolName, ch := q.Tool(), &q.choice
	if q.picking == PickEffort {
		switch msg.String() {
		case "esc", "enter", "tab":
			q.closePick(h)
			return nil
		}
		return ch.TypeEffort(h, toolName, msg)
	}
	switch msg.String() {
	case "esc", ModelKey:
		q.closePick(h)
		return nil
	case EffortKey:
		q.stepEffort(h)
		return nil
	case ProfileKey:
		q.stepProfile(h)
		return nil
	case "enter", "tab":
		ch.PickHighlighted(h, toolName)
		q.closePick(h)
		return nil
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		ch.StepList(h, toolName, delta)
		return nil
	}
	return ch.TypeFilter(msg)
}

// Hit is a clickable stretch of the bar, relative to its first line.
type Hit struct {
	Line, X0, X1 int
	Action       int
	// Entry is the model list row a ClickEntry picks.
	Entry int
}

// What a click on a stretch does.
const (
	ClickTool = iota
	ClickModel
	ClickEffort
	ClickProfile
	ClickWorktree
	ClickEntry
	// ClickSaved picks the saved prompt on list line Entry.
	ClickSaved
)

// Hits are the bar's clickable stretches as last painted.
func (q *Bar) Hits() []Hit { return q.hits }

// HitAt is the stretch at a column and line of the bar.
func (q *Bar) HitAt(col, line int) (Hit, bool) {
	for _, hit := range q.hits {
		if hit.Line == line && col >= hit.X0 && col < hit.X1 {
			return hit, true
		}
	}
	return Hit{}, false
}

// Click steps what a stretch shows the way its key does.
func (q *Bar) Click(h Host, hit Hit) Request {
	switch hit.Action {
	case ClickTool:
		q.closePick(h)
		return q.cycleTool(h, 1)
	case ClickModel:
		if q.picking == PickModel {
			q.closePick(h)
		} else {
			q.openPick(h, PickModel)
		}
	case ClickEffort:
		q.stepEffort(h)
	case ClickProfile:
		q.stepProfile(h)
	case ClickWorktree:
		return Request{Toggle: true}
	case ClickEntry:
		q.choice.PickEntry(h, q.Tool(), hit.Entry)
		q.closePick(h)
	case ClickSaved:
		return savedRequest(q.savedList.Click(hit.Entry))
	}
	return Request{}
}

// Legend is the bar's key hint.
func (q *Bar) Legend(h Host) [][2]string {
	toolName, ch := q.Tool(), &q.choice
	_, effortActive := ch.EffortShown(h, toolName)
	hasProfiles := spawn.HasProfiles(h, toolName)
	switch q.picking {
	case PickModel:
		pairs := [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵/tab", "choose"}}
		if effortActive {
			pairs = append(pairs, [2]string{EffortKey, "effort"})
		}
		if hasProfiles {
			pairs = append(pairs, [2]string{ProfileKey, "profile"})
		}
		return append(pairs, [2]string{"esc", "back to the prompt"})
	case PickEffort:
		return [][2]string{{"type", "effort"}, {"↵", "done"}, {"esc", "back to the prompt"}}
	}
	if q.savedList.Open() {
		return q.savedList.Hint("insert", "back to the prompt")
	}
	pairs := [][2]string{{"↵", "send"}, {"↑↓", "target or caret"}, {"tab", "tool"}}
	if len(q.toolNames) > 1 {
		pairs = append(pairs, [2]string{"shift+tab", "previous tool"})
	}
	pairs = append(pairs, [2]string{"ctrl+o", "saved prompts"})
	if q.Spawning(h) {
		if spawn.ModelListed(h, toolName) {
			pairs = append(pairs, [2]string{ModelKey, "model"})
		}
		if effortActive {
			pairs = append(pairs, [2]string{EffortKey, "effort"})
		}
		if hasProfiles {
			pairs = append(pairs, [2]string{ProfileKey, "profile"})
		}
	}
	return append(pairs, [2]string{"ctrl+t", "worktree"}, [2]string{"esc", "close"})
}

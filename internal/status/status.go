package status

import (
	"regexp"

	"github.com/YoanWai/agent-manager/internal/config"
)

const (
	Working  = "working"
	Waiting  = "waiting"
	Finished = "finished"
	Errored  = "errored"
	Idle     = "idle"
	Dead     = "dead"
	// Starting is the transient state a session shows from launch until its
	// agent first draws to the pane, so a new row appears immediately instead
	// of after the next poll.
	Starting = "starting"
)

type rule struct {
	state string
	re    *regexp.Regexp
}

type Engine struct {
	tools map[string]toolRules
}

type toolRules struct {
	defaultStatus  string
	activityCutoff *regexp.Regexp
	inputPrefix    *regexp.Regexp
	turnEnd        *regexp.Regexp
	chromeLine     *regexp.Regexp
	chromeBlock    *regexp.Regexp
	blockedLine    *regexp.Regexp
	trailingNote   *regexp.Regexp
	busyLine       *regexp.Regexp
	limitLine      *regexp.Regexp
	messageStart   *regexp.Regexp
	toolResult     *regexp.Regexp
	placeholder    *regexp.Regexp
	userEcho       *regexp.Regexp
	dialogFooter   *regexp.Regexp
	busyFooter     *regexp.Regexp
	// composerPlaceholder is the literal text a tool paints inside its
	// empty composer; a draft replaces it. Searched in a stripped row.
	composerPlaceholder string
	blinkingMarker      string
	rules               []rule
}

func NewEngine(cfg config.Config) (*Engine, error) {
	engine := &Engine{tools: map[string]toolRules{}}
	for name, tool := range cfg.Tools {
		compiled := make([]rule, 0, len(tool.Rules))
		for _, raw := range tool.Rules {
			re, err := regexp.Compile(raw.Pattern)
			if err != nil {
				return nil, err
			}
			compiled = append(compiled, rule{state: raw.State, re: re})
		}
		def := tool.DefaultStatus
		if def == "" {
			def = Idle
		}
		tr := toolRules{defaultStatus: def, composerPlaceholder: tool.ComposerPlaceholder, blinkingMarker: tool.BlinkingMarker, rules: compiled}
		optional := []struct {
			pattern string
			target  **regexp.Regexp
		}{
			{tool.ActivityCutoff, &tr.activityCutoff},
			{tool.InputPrefix, &tr.inputPrefix},
			{tool.TurnEnd, &tr.turnEnd},
			{tool.ChromeLine, &tr.chromeLine},
			{tool.ChromeBlock, &tr.chromeBlock},
			{tool.BlockedLine, &tr.blockedLine},
			{tool.TrailingNote, &tr.trailingNote},
			{tool.BusyLine, &tr.busyLine},
			{tool.LimitLine, &tr.limitLine},
			{tool.MessageStart, &tr.messageStart},
			{tool.ToolResult, &tr.toolResult},
			{tool.InputPlaceholder, &tr.placeholder},
			{tool.UserEcho, &tr.userEcho},
			{tool.DialogFooter, &tr.dialogFooter},
			{tool.BusyFooter, &tr.busyFooter},
		}
		for _, opt := range optional {
			if opt.pattern == "" {
				continue
			}
			re, err := regexp.Compile(opt.pattern)
			if err != nil {
				return nil, err
			}
			*opt.target = re
		}
		engine.tools[name] = tr
	}
	return engine, nil
}

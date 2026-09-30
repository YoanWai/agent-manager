package execution

import (
	"github.com/YoanWai/agent-manager/internal/launch"
	"strings"

	"unicode"

	"os"

	"github.com/YoanWai/agent-manager/internal/agentsession"

	"github.com/YoanWai/agent-manager/internal/store"

	"github.com/charmbracelet/x/ansi"
)

// paneBooted reports whether the agent has painted anything to its pane yet,
// which marks the end of the launch state.
func paneBooted(pane string) bool {
	return strings.TrimSpace(ansi.Strip(pane)) != ""
}

func isManagerEcho(line string) bool {
	for _, opening := range managerEchoOpenings {
		if strings.HasPrefix(line, opening) {
			return true
		}
	}
	return false
}

// rowLines is what a full screen row shows for a session: the agent's
// last message flattened to one line from its beginning, and the last
// prompt sent to it. The visible pane is the first source; when an
// anchor scrolled off it, the recovery is the tool's own transcript for
// Claude Code (which repaints in place, so tmux holds no history for
// it), and a deeper pane capture for everything else.
func (p *Runner) rowLines(sess store.Session, pane string) (quote, prompt string) {
	clean := p.engine.Plain(sess.Tool, pane)
	quote, anchored, ok := p.engine.LastMessage(sess.Tool, clean)
	if !ok {
		quote = lastMeaningfulPaneLine(clean)
	}
	prompt, echoOK := p.engine.LastUserEcho(sess.Tool, clean)
	if isManagerEcho(prompt) {
		prompt = ""
	}
	quoteAdrift := ok && !anchored && p.engine.HasMessageStart(sess.Tool)
	promptAdrift := echoOK && prompt == ""
	if (quoteAdrift || promptAdrift) && p.mcpStyles[sess.Tool] == "claude" && sess.AgentSessionID != "" {
		tailPrompt, tailReply := p.claudeTail(sess)
		if quoteAdrift && tailReply != "" {
			quote = tailReply
		}
		if promptAdrift && tailPrompt != "" {
			prompt = tailPrompt
		}
	} else if quoteAdrift || promptAdrift {
		if deep, err := p.tmux.CapturePaneHistory(sess.ID, quoteHistoryLines); err == nil {
			cleanDeep := p.engine.Plain(sess.Tool, deep)
			if line, anchored, ok := p.engine.LastMessage(sess.Tool, cleanDeep); ok && anchored {
				quote = line
			}
			if echoed, ok := p.engine.LastUserEcho(sess.Tool, cleanDeep); ok && echoed != "" && !isManagerEcho(echoed) {
				prompt = echoed
			}
		}
	}
	return capRunes(quote, rowQuoteCap), capRunes(prompt, rowQuoteCap)
}

// claudeTail is the newest prompt and reply in a Claude Code session's
// own transcript, flattened to single lines.
func (p *Runner) claudeTail(sess store.Session) (prompt, reply string) {
	path, err := agentsession.ClaudeTranscriptPath(sess.Cwd, sess.AgentSessionID)
	if err != nil {
		return "", ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", ""
	}
	if cached, ok := p.claudeTails[sess.ID]; ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.prompt, cached.reply
	}
	cleanUser := func(text string) string {
		cleaned := launch.DeliveredPrompt(text)
		if cleaned == "" || isManagerEcho(cleaned) {
			return ""
		}
		return cleaned
	}
	tailPrompt, tailReply, ok := agentsession.ClaudeTranscriptTail(sess.Cwd, sess.AgentSessionID, cleanUser)
	if !ok {
		return "", ""
	}
	prompt = oneLine(tailPrompt)
	reply = oneLine(tailReply)
	p.claudeTails[sess.ID] = claudeTailCache{size: info.Size(), modTime: info.ModTime(), prompt: prompt, reply: reply}
	return prompt, reply
}

func capRunes(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

// lastMeaningfulPaneLine is the newest pane line with a word on it. The
// fallback for tools without box rules, so pure chrome — blank rows,
// borders, bare spinners — is skipped until a line carrying a letter or
// digit turns up.
func lastMeaningfulPaneLine(pane string) string {
	lines := strings.Split(ansi.Strip(pane), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.ContainsFunc(line, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r)
		}) {
			return line
		}
	}
	return ""
}

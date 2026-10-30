package execution

import (
	"context"
	"errors"
	"github.com/YoanWai/agent-manager/internal/launch"
	"strings"

	"unicode"

	"os"

	"github.com/YoanWai/agent-manager/internal/agentsession"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"

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
// it), and a deeper pane capture for everything else. pi and omp draw a
// prompt with no marker at all, so theirs is read from their session file
// every time.
func (p *Runner) rowLines(ctx context.Context, sess store.Session, pane, dir string) (quote, prompt string) {
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
	promptUnreadable := !echoOK
	if (quoteAdrift || promptAdrift || promptUnreadable) && agentsession.KeepsTranscript(p.reportStyles[sess.Tool]) && sess.AgentSessionID != "" {
		tailPrompt, tailReply := p.transcriptTail(sess, dir)
		if quoteAdrift && tailReply != "" {
			quote = tailReply
		}
		if (promptAdrift || promptUnreadable) && tailPrompt != "" {
			prompt = tailPrompt
		}
	} else if quoteAdrift || promptAdrift {
		captureCtx, cancel := context.WithTimeout(ctx, tmux.CommandTimeout)
		deep, err := p.tmux.CapturePaneHistoryContext(captureCtx, sess.ID, quoteHistoryLines)
		cancel()
		if err == nil {
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

// transcriptTail is the newest prompt and reply in a session's own
// transcript, flattened to single lines. Claude moves the transcript with
// a session that changes directory, so dir, where the agent sits now, is
// tried before the launch directory.
func (p *Runner) transcriptTail(sess store.Session, dir string) (prompt, reply string) {
	style := p.reportStyles[sess.Tool]
	source, err := resolveTranscript(style, dir, sess.Cwd, sess.AgentSessionID)
	if err != nil {
		return "", ""
	}
	path, info := source.path, source.info
	if cached, ok := p.transcripts[sess.ID]; ok && cached.path == path && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.prompt, cached.reply
	}
	cleanUser := func(text string) string {
		cleaned := launch.DeliveredPrompt(text)
		if cleaned == "" || isManagerEcho(cleaned) {
			return ""
		}
		return cleaned
	}
	tailPrompt, tailReply, ok := agentsession.TranscriptTail(style, path, cleanUser)
	if !ok {
		return "", ""
	}
	prompt = transcriptLine(tailPrompt)
	reply = transcriptLine(tailReply)
	p.transcripts[sess.ID] = transcriptTailCache{path: path, size: info.Size(), modTime: info.ModTime(), prompt: prompt, reply: reply}
	return prompt, reply
}

// transcriptLine flattens transcript text to one row. A pane reaches the
// row through capture-pane, which drops escapes and controls on the way;
// a file holds whatever was typed or pasted into it, so the row strips
// them itself.
func transcriptLine(text string) string {
	return oneLine(sanitizeBody(ansi.Strip(text)))
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

type transcriptSource struct {
	path string
	info os.FileInfo
}

func resolveTranscript(style, liveDir, launchDir, sessionID string) (transcriptSource, error) {
	for _, directory := range []string{liveDir, launchDir} {
		if directory == "" {
			continue
		}
		path, err := agentsession.TranscriptPath(style, directory, sessionID)
		if err != nil {
			// A missing file is tried in the other directory. A style this
			// package does not read is not that: rowLines only calls here
			// after KeepsTranscript, so returning the error keeps a caller
			// that skipped the check from looking like an empty transcript.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return transcriptSource{}, err
		}
		info, err := os.Stat(path)
		if err == nil {
			return transcriptSource{path: path, info: info}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return transcriptSource{}, err
		}
	}
	return transcriptSource{}, os.ErrNotExist
}

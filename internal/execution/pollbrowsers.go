package execution

import (
	"context"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// browserEngineMaxAge spaces terminal-browser's ls, a process per run,
// however often pokes make the poll pass.
const browserEngineMaxAge = 2 * time.Second

type BrowserLister interface {
	ListCached(ctx context.Context, maxAge time.Duration) ([]termbrowser.Browser, error)
}

// BrowserEngine is the active tab terminal-browser reports for the engine in
// one browser row's pane, and whether an agent is driving it.
type BrowserEngine struct {
	URL, Title  string
	AgentDriven bool
}

type browserRead struct {
	engines map[string]BrowserEngine
	ok      bool
	warning string
}

// readBrowserEngines maps the engines ls lists onto the live browser rows at
// rows, and stores each active tab's URL where it moved. A failed ls leaves
// the read not ok and warns once per distinct failure.
func (p *Runner) readBrowserEngines(ctx context.Context, sessions []store.Session, rows []int) (browserRead, error) {
	if p.browsers == nil {
		return browserRead{}, nil
	}
	if len(rows) == 0 {
		return browserRead{engines: map[string]BrowserEngine{}, ok: true}, nil
	}
	listCtx, cancel := context.WithTimeout(ctx, tmux.CommandTimeout)
	defer cancel()
	browsers, err := p.browsers.ListCached(listCtx, browserEngineMaxAge)
	if err != nil {
		failure := "reading the browser engines: " + err.Error()
		if failure == p.browserFailure {
			return browserRead{}, nil
		}
		p.browserFailure = failure
		return browserRead{warning: failure}, nil
	}
	p.browserFailure = ""
	bySession := make(map[string]termbrowser.Browser, len(browsers))
	for _, browser := range browsers {
		if id := browser.SessionID(); id != "" {
			bySession[id] = browser
		}
	}
	engines := make(map[string]BrowserEngine, len(rows))
	for _, index := range rows {
		sess := sessions[index]
		browser, listed := bySession[sess.ID]
		if !listed {
			continue
		}
		tab, _ := browser.ActiveTab()
		engines[sess.ID] = BrowserEngine{URL: tab.URL, Title: tab.Title, AgentDriven: tab.AgentControlled}
		if tab.URL == "" || tab.URL == sess.BrowserURL {
			continue
		}
		if err := ignoreDeletedSession(p.store.SetBrowserURL(sess.ID, tab.URL)); err != nil {
			return browserRead{}, err
		}
		sessions[index].BrowserURL = tab.URL
	}
	return browserRead{engines: engines, ok: true}, nil
}

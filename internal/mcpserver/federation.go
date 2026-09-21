package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/federation"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// Wrappers keep the native services authoritative for this host. Only explicitly
// host-qualified targets cross SSH; remote commands use the host's CLI directly.
type fleetSessions struct {
	sessionCommands
	fleet   *federation.Client
	local   string
	loadErr error
}
type fleetTerminals struct {
	terminalCommands
	fleet   *federation.Client
	local   string
	loadErr error
}

func federate(dir string, s sessionCommands, t terminalCommands) (sessionCommands, terminalCommands) {
	f, err := federation.Load(dir)
	if os.IsNotExist(err) {
		return s, t
	}
	local := ""
	if f != nil {
		for _, h := range f.Hosts() {
			if h.SSH == "" {
				local = h.Name
				break
			}
		}
	}
	return &fleetSessions{s, f, local, err}, &fleetTerminals{t, f, local, err}
}
func qualified(host, id string) string {
	if host == "" {
		return id
	}
	return (federation.Ref{Host: host, ID: id}).String()
}
func routedGroup(host string, group *string) (*string, error) {
	if host == "" {
		return group, nil
	}
	value := ""
	if group != nil {
		value = *group
	}
	if r, ok := federation.ParseRef(value); ok {
		if r.Host != host {
			return nil, fmt.Errorf("host and group host disagree")
		}
		return group, nil
	}
	value = qualified(host, value)
	return &value, nil
}
func route(f *federation.Client, loadErr error, id string) (federation.Ref, bool, error) {
	if loadErr != nil {
		return federation.Ref{}, false, loadErr
	}
	ref, ok := federation.ParseRef(id)
	if !ok {
		return federation.Ref{ID: id}, false, nil
	}
	for _, h := range f.Hosts() {
		if h.Name == ref.Host {
			return ref, h.SSH != "", nil
		}
	}
	return ref, false, fmt.Errorf("unknown host %q", ref.Host)
}
func remoteResult[T any](f *federation.Client, ctx context.Context, host string, args ...string) (T, error) {
	var result T
	b, err := f.Command(ctx, host, args...)
	if err == nil {
		err = json.Unmarshal(b, &result)
	}
	return result, err
}
func sessionHost(host string, s sessioncmd.Session) sessioncmd.Session {
	s.ID = qualified(host, s.ID)
	s.Group = qualified(host, s.Group)
	return s
}
func terminalHost(host string, t sessioncmd.Terminal) sessioncmd.Terminal {
	t.ID = qualified(host, t.ID)
	t.Group = qualified(host, t.Group)
	if t.ParentID != "" {
		t.ParentID = qualified(host, t.ParentID)
	}
	return t
}
func (s *fleetSessions) List(caller string) ([]sessioncmd.Session, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	listed, err := s.sessionCommands.List(caller)
	if err != nil {
		return nil, err
	}
	listed = append([]sessioncmd.Session{}, listed...)
	var unavailable []error
	for i := range listed {
		listed[i] = sessionHost(s.local, listed[i])
	}
	for _, h := range s.fleet.Hosts() {
		if h.SSH == "" {
			continue
		}
		rows, err := remoteResult[[]sessioncmd.Session](s.fleet, context.Background(), h.Name, "sessions", "--json")
		if err != nil {
			unavailable = append(unavailable, fmt.Errorf("host %s unavailable: %w", h.Name, err))
			continue
		}
		for _, r := range rows {
			r.Self = false
			listed = append(listed, sessionHost(h.Name, r))
		}
	}
	return listed, errors.Join(unavailable...)
}
func (s *fleetSessions) Create(caller string, o sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	group := ""
	if o.Group != nil {
		group = *o.Group
	}
	r, remote, err := route(s.fleet, s.loadErr, group)
	if err != nil {
		return sessioncmd.Session{}, err
	}
	if o.Group != nil {
		o.Group = &r.ID
	}
	if remote {
		v, e := s.fleet.Create(context.Background(), r.Host, o)
		return sessionHost(r.Host, v), e
	}
	v, e := s.sessionCommands.Create(caller, o)
	return sessionHost(s.local, v), e
}
func (s *fleetSessions) Read(caller, id string) (sessioncmd.SessionScreen, error) {
	r, remote, err := route(s.fleet, s.loadErr, id)
	if err != nil {
		return sessioncmd.SessionScreen{}, err
	}
	if remote {
		v, e := remoteResult[sessioncmd.SessionScreen](s.fleet, context.Background(), r.Host, "read", r.ID, "--json")
		v.Session = sessionHost(r.Host, v.Session)
		return v, e
	}
	v, e := s.sessionCommands.Read(caller, r.ID)
	v.Session = sessionHost(s.local, v.Session)
	return v, e
}
func (s *fleetSessions) Send(caller, id, text string) (sessioncmd.SendResult, error) {
	r, remote, err := route(s.fleet, s.loadErr, id)
	if err != nil {
		return sessioncmd.SendResult{}, err
	}
	if remote {
		return remoteResult[sessioncmd.SendResult](s.fleet, context.Background(), r.Host, "send", r.ID, text, "--json")
	}
	return s.sessionCommands.Send(caller, r.ID, text)
}
func (s *fleetSessions) Wait(ctx context.Context, caller, id string, until []string, timeout time.Duration) (sessioncmd.WaitResult, error) {
	r, remote, err := route(s.fleet, s.loadErr, id)
	if err != nil {
		return sessioncmd.WaitResult{}, err
	}
	if !remote {
		v, e := s.sessionCommands.Wait(ctx, caller, r.ID, until, timeout)
		v.Session = sessionHost(s.local, v.Session)
		return v, e
	}
	if timeout <= 0 {
		timeout = 50 * time.Second
	}
	args := []string{"wait", r.ID, "--json", "--timeout", timeout.String()}
	if len(until) > 0 {
		args = append(args, "--until", strings.Join(until, ","))
	}
	v, e := remoteResult[sessioncmd.WaitResult](s.fleet, ctx, r.Host, args...)
	v.Session = sessionHost(r.Host, v.Session)
	return v, e
}
func (s *fleetSessions) lifecycle(caller, id, action string, archived bool) (sessioncmd.Session, error) {
	r, remote, err := route(s.fleet, s.loadErr, id)
	if err != nil {
		return sessioncmd.Session{}, err
	}
	var v sessioncmd.Session
	if remote {
		v, err = s.fleet.Lifecycle(context.Background(), r, action, !archived)
		return sessionHost(r.Host, v), err
	}
	switch action {
	case "kill":
		v, err = s.sessionCommands.Kill(caller, r.ID)
	case "revive":
		v, err = s.sessionCommands.Revive(caller, r.ID)
	case "archive":
		v, err = s.sessionCommands.Archive(caller, r.ID, archived)
	}
	return sessionHost(s.local, v), err
}
func (s *fleetSessions) Kill(caller, id string) (sessioncmd.Session, error) {
	return s.lifecycle(caller, id, "kill", false)
}
func (s *fleetSessions) Revive(caller, id string) (sessioncmd.Session, error) {
	return s.lifecycle(caller, id, "revive", false)
}
func (s *fleetSessions) Archive(caller, id string, archived bool) (sessioncmd.Session, error) {
	return s.lifecycle(caller, id, "archive", archived)
}
func (s *fleetSessions) MessageStatusHost(caller, host string, id int64) (sessioncmd.MessageState, error) {
	r, remote, err := route(s.fleet, s.loadErr, qualified(host, ""))
	if err != nil {
		return sessioncmd.MessageState{}, err
	}
	if remote {
		return remoteResult[sessioncmd.MessageState](s.fleet, context.Background(), r.Host, "message-status", strconv.FormatInt(id, 10), "--json")
	}
	return s.sessionCommands.MessageStatus(caller, id)
}
func (s *fleetSessions) Groups(caller string) ([]sessioncmd.Group, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	groups, err := s.sessionCommands.Groups(caller)
	if err != nil {
		return nil, err
	}
	groups = append([]sessioncmd.Group{}, groups...)
	var unavailable []error
	for i := range groups {
		groups[i].Path = qualified(s.local, groups[i].Path)
	}
	for _, h := range s.fleet.Hosts() {
		if h.SSH == "" {
			continue
		}
		more, err := remoteResult[[]sessioncmd.Group](s.fleet, context.Background(), h.Name, "groups", "--json")
		if err != nil {
			unavailable = append(unavailable, fmt.Errorf("host %s unavailable: %w", h.Name, err))
			continue
		}
		for _, g := range more {
			g.Path = qualified(h.Name, g.Path)
			groups = append(groups, g)
		}
	}
	return groups, errors.Join(unavailable...)
}
func (s *fleetSessions) CreateGroup(caller, path, dir string) (sessioncmd.Group, error) {
	r, remote, err := route(s.fleet, s.loadErr, path)
	if err != nil {
		return sessioncmd.Group{}, err
	}
	if remote {
		args := []string{"create-group", r.ID, "--json"}
		if dir != "" {
			args = append(args, "--directory", dir)
		}
		g, e := remoteResult[sessioncmd.Group](s.fleet, context.Background(), r.Host, args...)
		g.Path = qualified(r.Host, g.Path)
		return g, e
	}
	g, e := s.sessionCommands.CreateGroup(caller, r.ID, dir)
	g.Path = qualified(s.local, g.Path)
	return g, e
}
func (s *fleetSessions) DeleteGroup(caller, path string) (sessioncmd.GroupRemoval, error) {
	r, remote, err := route(s.fleet, s.loadErr, path)
	if err != nil {
		return sessioncmd.GroupRemoval{}, err
	}
	if remote {
		return remoteResult[sessioncmd.GroupRemoval](s.fleet, context.Background(), r.Host, "delete-group", r.ID, "--json")
	}
	return s.sessionCommands.DeleteGroup(caller, r.ID)
}
func (t *fleetTerminals) List(caller string) ([]sessioncmd.Terminal, error) {
	if t.loadErr != nil {
		return nil, t.loadErr
	}
	listed, err := t.terminalCommands.List(caller)
	if err != nil {
		return nil, err
	}
	listed = append([]sessioncmd.Terminal{}, listed...)
	var unavailable []error
	for i := range listed {
		listed[i] = terminalHost(t.local, listed[i])
	}
	for _, h := range t.fleet.Hosts() {
		if h.SSH == "" {
			continue
		}
		more, err := remoteResult[[]sessioncmd.Terminal](t.fleet, context.Background(), h.Name, "terminal", "list", "--json")
		if err != nil {
			unavailable = append(unavailable, fmt.Errorf("host %s unavailable: %w", h.Name, err))
			continue
		}
		for _, v := range more {
			listed = append(listed, terminalHost(h.Name, v))
		}
	}
	return listed, errors.Join(unavailable...)
}
func (t *fleetTerminals) Create(caller string, o sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	group := ""
	if o.Group != nil {
		group = *o.Group
	}
	r, remote, err := route(t.fleet, t.loadErr, group)
	if err != nil {
		return sessioncmd.Terminal{}, err
	}
	if o.Group != nil {
		o.Group = &r.ID
	}
	if remote {
		v, e := t.fleet.TerminalCreate(context.Background(), r.Host, o)
		return terminalHost(r.Host, v), e
	}
	v, e := t.terminalCommands.Create(caller, o)
	return terminalHost(t.local, v), e
}
func (t *fleetTerminals) Send(caller, id, command string, keys []string) (sessioncmd.TerminalInput, error) {
	r, remote, err := route(t.fleet, t.loadErr, id)
	if err != nil {
		return sessioncmd.TerminalInput{}, err
	}
	if remote {
		v, e := t.fleet.TerminalSend(context.Background(), r, command, keys)
		v.TerminalID = qualified(r.Host, v.TerminalID)
		return v, e
	}
	v, e := t.terminalCommands.Send(caller, r.ID, command, keys)
	v.TerminalID = qualified(t.local, v.TerminalID)
	return v, e
}
func (t *fleetTerminals) Read(caller, id string) (sessioncmd.TerminalScreen, error) {
	r, remote, err := route(t.fleet, t.loadErr, id)
	if err != nil {
		return sessioncmd.TerminalScreen{}, err
	}
	if remote {
		v, e := remoteResult[sessioncmd.TerminalScreen](t.fleet, context.Background(), r.Host, "terminal", "read", r.ID, "--json")
		v.Terminal = terminalHost(r.Host, v.Terminal)
		return v, e
	}
	v, e := t.terminalCommands.Read(caller, r.ID)
	v.Terminal = terminalHost(t.local, v.Terminal)
	return v, e
}
func (t *fleetTerminals) Close(caller, id string) error {
	r, remote, err := route(t.fleet, t.loadErr, id)
	if err != nil {
		return err
	}
	if remote {
		return t.fleet.TerminalClose(context.Background(), r)
	}
	return t.terminalCommands.Close(caller, r.ID)
}

package sessioncmd

import "context"

type Mailbox struct{ configDir string }

func NewMailbox(configDir string) *Mailbox { return &Mailbox{configDir: configDir} }
func (m *Mailbox) Rename(ctx context.Context, id, name string) (string, error) {
	return Rename(ctx, m.configDir, id, name)
}
func (m *Mailbox) ReviewRepo(id, target string) (string, error) {
	return ReviewRepo(m.configDir, id, target)
}
func (m *Mailbox) ReviewBase(id, cwd, ref string) (string, error) {
	return ReviewBase(m.configDir, id, cwd, ref)
}
func (m *Mailbox) ReviewScope(id, scope string) (string, error) {
	return ReviewScope(m.configDir, id, scope)
}
func (m *Mailbox) ReviewComment(id, comment string, handled bool) (string, error) {
	return ReviewComment(m.configDir, id, comment, handled)
}

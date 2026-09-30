package ui

func (m *Model) viewMove() string {
	return m.card("⇄ Move", m.viewGroupPicker(),
		[][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"esc", "cancel"}})
}

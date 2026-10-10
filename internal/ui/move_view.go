package ui

func (d *moveDialog) view(h moveHost) string {
	return h.card("⇄ Move", h.viewGroupPicker(),
		[][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"esc", "cancel"}})
}

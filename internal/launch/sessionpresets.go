package launch

func WithInstructions(instructions, task string) string {
	if instructions == "" {
		return task
	}
	if task == "" {
		return instructions
	}
	return instructions + "\n\n" + task
}

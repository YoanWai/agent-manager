package onboarding

// sceneBeats is how many ticks each scene of an animated guide step holds,
// in order. Scene i acts out the step's guide line i, unless sceneLines
// says otherwise, and the step loops.
var sceneBeats = map[Step][]int{
	Welcome: {70},
	// The quick prompt types and starts three agents, one after another.
	QuickPrompt: {8, 12, 6, 10, 6, 10, 20},
	Sessions:    {36, 40, 18},
	Review:      {18, 40, 30},
	Orchestrate: {54, 60, 80},
}

// sceneLines is the guide line each scene acts out, for a step that acts a
// line out more than once.
var sceneLines = map[Step][]int{
	QuickPrompt: {0, 1, 2, 1, 2, 1, 2},
}

// line is the guide line the scene on screen acts out.
func (m Model) line() int {
	scene, _ := m.scene()
	if lines, ok := sceneLines[m.Step()]; ok {
		return lines[scene]
	}
	return scene
}

// Animating reports a guide step that acts itself out, which needs ticks.
func (m Model) Animating() bool {
	_, ok := sceneBeats[m.Step()]
	return !m.done && ok
}

func (m *Model) Tick() { m.beat++ }

// scene is the scene on screen and how many beats into it the animation is.
func (m Model) scene() (index, frame int) {
	beats := sceneBeats[m.Step()]
	total := 0
	for _, length := range beats {
		total += length
	}
	if total == 0 {
		return 0, m.beat
	}
	frame = m.beat % total
	for index < len(beats)-1 && frame >= beats[index] {
		frame -= beats[index]
		index++
	}
	return index, frame
}

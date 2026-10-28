package onboarding

// sceneBeats is how many ticks each scene of an animated guide step holds,
// in order. Scene i acts out the step's guide line i, and the step loops.
var sceneBeats = map[Step][]int{
	Welcome:     {70},
	QuickPrompt: {14, 34, 30},
	Sessions:    {36, 40, 18},
	Review:      {18, 40, 30},
	Orchestrate: {54, 60, 80},
}

// Animating reports a guide step that acts itself out, which needs ticks.
func (m Model) Animating() bool {
	_, ok := sceneBeats[m.Step()]
	return !m.done && ok
}

// Tick advances the open step's animation by one beat.
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

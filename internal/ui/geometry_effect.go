package ui

type paneResize struct {
	id   string
	size [2]int
}
type geometryRequest struct {
	targets []paneResize
	publish [2]int
}

func (geometryRequest) effectRequest() {}

type geometryEffectResult struct {
	resized []paneResize
	publish [2]int
}

func (geometryEffectResult) effectResult() {}

func (m *Model) queueGeometry(request geometryRequest) {
	// Ignore an already accepted identical target, while allowing a newer size.
	desired := map[string][2]int{}
	published := [2]int{}
	jobs := append([]*effectJob(nil), m.effects.main.pending...)
	if m.effects.main.active != nil {
		jobs = append([]*effectJob{m.effects.main.active}, jobs...)
	}
	for _, job := range jobs {
		if existing, ok := job.request.(geometryRequest); ok {
			for _, target := range existing.targets {
				desired[target.id] = target.size
			}
			if existing.publish != [2]int{} {
				published = existing.publish
			}
		}
	}
	targets := make([]paneResize, 0, len(request.targets))
	for _, target := range request.targets {
		if desired[target.id] != target.size {
			targets = append(targets, target)
		}
	}
	request.targets = targets
	if published == request.publish {
		request.publish = [2]int{}
	}
	if len(request.targets) == 0 && request.publish == [2]int{} {
		return
	}
	// Coalesce only the adjacent geometry job, never across an accepted mutation.
	if n := len(m.effects.main.pending); n > 0 {
		if previous, ok := m.effects.main.pending[n-1].request.(geometryRequest); ok {
			byID := map[string]int{}
			for i, target := range previous.targets {
				byID[target.id] = i
			}
			for _, target := range request.targets {
				if i, ok := byID[target.id]; ok {
					previous.targets[i] = target
				} else {
					previous.targets = append(previous.targets, target)
				}
			}
			if request.publish != [2]int{} {
				previous.publish = request.publish
			}
			request = previous
			m.effects.main.pending = m.effects.main.pending[:n-1]
		}
	}
	request.targets = append([]paneResize(nil), request.targets...)
	m.enqueueEffect(request, 0, false)
}
func (s effectServices) runGeometry(request geometryRequest) (effectResult, error) {
	result := geometryEffectResult{}
	if request.publish != [2]int{} {
		if err := s.store.SetPaneSize(request.publish[0], request.publish[1]); err != nil {
			return result, err
		}
		result.publish = request.publish
	}
	if len(request.targets) == 0 {
		return result, nil
	}
	ids := make([]string, len(request.targets))
	for i, target := range request.targets {
		ids[i] = target.id
	}
	err := s.reflow(ids, func() {
		results := make(chan paneResize, len(request.targets))
		for _, target := range request.targets {
			go func(target paneResize) {
				if s.driver.Resize(target.id, target.size[0], target.size[1]) != nil {
					target.id = ""
				}
				results <- target
			}(target)
		}
		for range request.targets {
			if target := <-results; target.id != "" {
				result.resized = append(result.resized, target)
			}
		}
	})
	return result, err
}
func (m *Model) applyGeometryEffect(result geometryEffectResult, err error) {
	if m.focus.runtime.lastPaneSizes == nil {
		m.focus.runtime.lastPaneSizes = map[string][2]int{}
	}
	for _, target := range result.resized {
		m.focus.runtime.lastPaneSizes[target.id] = target.size
	}
	if result.publish != [2]int{} {
		m.focus.runtime.lastPublishedSize = result.publish
	}
	if err != nil {
		m.reportErr(err.Error())
	}
}

//go:build windows

package conversation

// FromAgent asks whether process is the agent a launch started under agent.
// Windows has no process group to bound a walk below the launched process,
// so only the agent itself counts, spawned or not.
func FromAgent(agent, process int, _ bool) (bool, error) {
	return process == agent, nil
}

package equip

import (
	"encoding/json"
	"slices"
)

// fixedCost is the tokens of the blocks agent puts into a session once: the
// skills intro when it lists skills, and Codex's plugins block when a plugin
// is on.
func fixedCost(agent Agent, listed, plugins bool) int {
	cost := 0
	if listed {
		cost += agent.listing().introTokens
	}

	if plugins && agent == Codex {
		cost += tokens(Codex, codexPluginsBlockBytes)
	}

	return cost
}

// total adds up the costs in agent of what is on, with agent's fixed cost.
func (s *Session) total(agent Agent) Total {
	total, listingTokens, unknown := 0, 0, false
	listed, plugins := false, false // a skill or plugin is on, so agent lists skills; a plugin is on

	for _, ext := range s.pending.exts {
		// A plugin's MCP server counts in its plugin's cost.
		if ext.plugin != "" {
			continue
		}

		cost := s.costIn(agent, ext)
		total += cost

		if ext.Kind != MCPServer && cost > 0 {
			listingTokens += ext.cost[agent] // of its skills alone, without a plugin's MCP servers
			listed = true
		}

		plugins = plugins || ext.Kind == Plugin && ext.has(agent) && s.pending.stateIn(agent, ext) == On
		unknown = unknown || s.unknownIn(agent, ext)
	}

	budget := agent.listing().budget

	return Total{
		Tokens: total + fixedCost(agent, listed, plugins), Unknown: unknown,
		OverBudget: budget > 0 && listingTokens > budget,
	}
}

// unknown reports whether the cost of ext is unknown, in part for a plugin:
// an MCP server's, until it is measured.
func (s *Session) unknown(ext Extension) bool {
	_, measured := s.measurement(ext.Key)

	return ext.Kind == MCPServer && !measured ||
		slices.ContainsFunc(Agents(), func(agent Agent) bool { return s.unknownIn(agent, ext) })
}

// unknownIn reports whether agent's cost of ext leaves out an MCP server
// that is on in agent but not measured yet: ext, or one in plugin ext.
func (s *Session) unknownIn(agent Agent, ext Extension) bool {
	if !ext.has(agent) || s.pending.stateIn(agent, ext) != On {
		return false
	}

	if ext.Kind == MCPServer {
		_, measured := s.measurement(ext.Key)

		return !measured
	}

	return slices.ContainsFunc(s.pending.exts, func(server Extension) bool {
		return server.plugin == ext.Key && s.unknownIn(agent, server)
	})
}

// measurement returns the measurement of the MCP server with key, reporting
// whether it has one.
func (s *Session) measurement(key string) (measurement, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	measured, ok := s.measured[key]

	return measured, ok
}

// cost estimates the tokens of ext: the higher of the agents' costs.
func (s *Session) cost(ext Extension) int {
	cost := 0

	for _, agent := range Agents() {
		cost = max(cost, s.costIn(agent, ext))
	}

	return cost
}

// costIn estimates the tokens ext puts into agent's sessions.
func (s *Session) costIn(agent Agent, ext Extension) int {
	if !ext.has(agent) || s.pending.stateIn(agent, ext) != On {
		return 0
	}

	if ext.Kind == MCPServer {
		var cfg serverConfig

		measured, ok := s.measurement(ext.Key)
		if !ok {
			return 0
		}

		_ = json.Unmarshal(ext.config[ClaudeCode], &cfg)

		return measured.cost(agent, ext.name(), cfg.AlwaysLoad || !claudeToolSearch(s.machine))
	}

	cost := ext.cost[agent]
	// A plugin costs its MCP servers too.
	for _, server := range s.pending.exts {
		if server.plugin == ext.Key {
			cost += s.costIn(agent, server)
		}
	}

	return cost
}

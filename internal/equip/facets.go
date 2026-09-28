package equip

// Facet is a way to narrow the list.
type Facet struct {
	keys     map[string]bool // of the rows it keeps
	Name     string
	NewGroup bool // the first of the agents' facets, of the states' or of the changes'
}

// Has reports whether the facet keeps row.
func (f Facet) Has(row Row) bool { return f.keys[row.Key] }

// Count is the number of rows the facet keeps.
func (f Facet) Count() int { return len(f.keys) }

// facet is the name of a Facet and the test of the rows it keeps.
type facet struct {
	has  func(ext Extension, row Row) bool
	name string
}

// kindFacet is the facet of the rows of kind k.
func kindFacet(name string, k Kind) facet {
	return facet{name: name, has: func(_ Extension, row Row) bool { return row.Kind == k }}
}

// agentFacet is the facet of the rows agent has, in any state.
func agentFacet(agent Agent) facet {
	return facet{name: agent.String(), has: func(ext Extension, _ Row) bool { return ext.has(agent) }}
}

// stateFacet is the facet of the rows in state st.
func stateFacet(name string, st State) facet {
	return facet{name: name, has: func(_ Extension, row Row) bool { return row.State == st }}
}

// facets are the facets of rows, in the order the sidebar shows them.
func (s *Session) facets(rows []Row) []Facet {
	groups := [][]facet{
		{
			{name: "All", has: func(Extension, Row) bool { return true }},
			kindFacet("Skills", Skill), kindFacet("Plugins", Plugin), kindFacet("MCP servers", MCPServer),
			{name: "By name", has: func(_ Extension, row Row) bool { return row.ByName }},
		},
		{agentFacet(ClaudeCode), agentFacet(Codex)},
		{stateFacet("On", On), stateFacet("Manual-only", ManualOnly), stateFacet("Off", Off)},
		{
			{name: "Overrides", has: func(ext Extension, _ Row) bool {
				return s.inRow(ext, s.overridden)
			}},
			{name: "Not applied", has: func(_ Extension, row Row) bool { return row.NotApplied }},
			{name: "Unsaved changes", has: func(_ Extension, row Row) bool { return row.Unsaved }},
		},
	}

	var out []Facet

	for groupIndex, group := range groups {
		for index, def := range group {
			keys := map[string]bool{}

			for _, row := range rows {
				if ext, _ := s.pending.ext(row.Key); def.has(ext, row) {
					keys[row.Key] = true
				}
			}

			out = append(out, Facet{Name: def.name, keys: keys, NewGroup: groupIndex > 0 && index == 0})
		}
	}

	return out
}

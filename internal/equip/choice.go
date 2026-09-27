package equip

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// choice is a Project's active presets and Overrides, with the library the
// presets come from. It resolves the state of each extension.
type choice struct {
	overrides map[string]State      // by extension key
	applied   map[Agent][]Extension // the exts whose states equip writes for each agent
	// library is the presets by name, as written, and a new one. Every choice
	// of a Session starts from the same library, as a preset write goes to
	// disk at once.
	library []Preset
	active  []string    // the ids of the active presets, sorted
	exts    []Extension // installed, to find a plugin's
}

// state returns the state of ext and whether an Override set it.
func (c choice) state(ext Extension) (State, bool) {
	state, override := c.overrides[ext.Key]
	// A plugin's MCP server loads only while its plugin is on.
	if plugin, ok := c.ext(ext.plugin); ok {
		if pluginState, _ := c.state(plugin); pluginState != On {
			return pluginState, override
		}
	}

	if !override {
		state = c.base(ext.primary(), ext)
	}

	return state, override
}

// stateIn is the state ext has in agent's sessions: its Override or the
// presets' state where equip writes it for agent, else agent's own default.
// So Codex keeps a skill on.
func (c choice) stateIn(agent Agent, ext Extension) State {
	if !slices.ContainsFunc(c.applied[agent], func(e Extension) bool { return e.Key == ext.Key }) {
		return ext.fallback[agent]
	}

	if st, ok := c.overrides[ext.Key]; ok {
		return st
	}

	return c.base(agent, ext)
}

// base is the state ext has in agent without an Override. With active
// presets, it is on for a member of one of them and off for everything else;
// with none, and for an MCP server that follows its plugin, it is agent's
// default.
func (c choice) base(agent Agent, ext Extension) State {
	if len(c.active) == 0 || ext.plugin != "" {
		return ext.fallback[agent]
	}

	for _, preset := range c.library {
		if slices.Contains(c.active, preset.ID) &&
			slices.ContainsFunc(preset.Members, func(m Member) bool { return m.Key == ext.Key }) {
			return On
		}
	}

	return Off
}

// entries are the entries a save of c leaves in agent's config. With active
// presets, every extension equip writes for agent gets one.
func (c choice) entries(agent Agent) map[string]State {
	entries := map[string]State{}

	for _, ext := range c.applied[agent] {
		state, ok := c.overrides[ext.Key]
		if !ok && len(c.active) > 0 {
			state, ok = c.base(agent, ext), true
		}

		if ok && ext.entry(agent, state) {
			entries[ext.Key] = state
		}
	}

	return entries
}

// record is the active presets as a record keeps them, each with a hash of
// its members. A missing preset keeps its name from last, the record's.
func (c choice) record(last []recordPreset) []recordPreset {
	out := make([]recordPreset, 0, len(c.active))

	for _, active := range c.active {
		sum := sha256.New()
		name := presetName(last, active)

		for _, preset := range c.library {
			if preset.ID != active {
				continue
			}

			name = preset.Name

			for _, member := range preset.Members {
				_, _ = sum.Write([]byte(member.Key + "\n")) // a hash never fails to write
			}
		}

		out = append(out, recordPreset{ID: active, Name: name, Hash: hex.EncodeToString(sum.Sum(nil)[:8])})
	}

	return out
}

// presetName is the name presets, a record's, keep for the preset with id. It
// is empty when they keep none.
func presetName(presets []recordPreset, id string) string {
	for _, preset := range presets {
		if preset.ID == id {
			return preset.Name
		}
	}

	return ""
}

// ext returns the extension with key, reporting whether it is installed.
func (c choice) ext(key string) (Extension, bool) {
	var ext Extension

	i := slices.IndexFunc(c.exts, func(e Extension) bool { return e.Key == key })
	if i >= 0 {
		ext = c.exts[i]
	}

	return ext, i >= 0
}

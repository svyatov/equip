package equip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Preset is a named set of extensions for one kind of project.
type Preset struct {
	ID       string // set at creation, never changes
	Name     string
	Members  []Member // by kind, then by name
	Projects int      // the projects on this machine whose records have it active
	Active   bool     // active in this Project, pending
}

// Member is one extension a Preset names, which may not be installed.
type Member struct {
	Key       string
	Name      string
	Kind      Kind
	Installed bool
}

// presetFile is a preset as its file keeps it: members by kind, as record
// tables name the kinds.
type presetFile struct {
	ID         string   `toml:"id"`
	Skills     []string `toml:"skills"`
	Plugins    []string `toml:"plugins"`
	MCPServers []string `toml:"mcp_servers"`
}

// errNoID is the error of a preset file with no id, which a Project cannot
// keep active.
var errNoID = errors.New("preset has no id")

// presetsDir is the dir of the presets, one file each, named after the preset.
func presetsDir(machine Machine) string { return filepath.Join(machine.ConfigHome, "equip", "presets") }

// readPresets reads every preset, by name.
func readPresets(machine Machine) ([]Preset, error) {
	// Glob sorts by file name, which is the preset's name.
	files, _ := filepath.Glob(filepath.Join(presetsDir(machine), "*.toml"))
	presets := make([]Preset, 0, len(files))

	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // equip builds the path
		if err != nil {
			return nil, fmt.Errorf("read preset: %w", err)
		}

		var preset presetFile

		err = toml.Unmarshal(data, &preset)
		if err == nil && preset.ID == "" {
			err = errNoID
		}

		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}

		var members []Member

		for kind, names := range [][]string{Skill: preset.Skills, Plugin: preset.Plugins, MCPServer: preset.MCPServers} {
			for _, name := range slices.Sorted(slices.Values(names)) {
				members = append(members, Member{Key: Kind(kind).keyOf(name), Name: name, Kind: Kind(kind), Installed: false})
			}
		}

		presets = append(presets, Preset{
			ID: preset.ID, Name: strings.TrimSuffix(filepath.Base(file), ".toml"), Members: members,
			Projects: 0, Active: false,
		})
	}

	return presets, nil
}

// Presets returns the library: every preset, by name.
func (s *Session) Presets() []Preset {
	// ponytail: reads every record on each call; keep the counts if a
	// machine ever holds enough records to notice.
	recs := records(s.machine)
	out := make([]Preset, 0, len(s.library))

	for _, preset := range s.library {
		preset.Active = slices.Contains(s.active, preset.ID)
		preset.Members = slices.Clone(preset.Members)

		for i, member := range preset.Members {
			_, preset.Members[i].Installed = s.ext(member.Key)
		}

		for _, rec := range recs {
			if slices.Contains(ids(rec.Presets), preset.ID) {
				preset.Projects++
			}
		}

		out = append(out, preset)
	}

	return out
}

// SetPresets makes the presets with the ids active the active ones in the
// Project.
func (s *Session) SetPresets(active []string) {
	// Sorted, so the same presets in another order are no change.
	s.active = slices.Compact(slices.Sorted(slices.Values(active)))
}

// TurnedSince counts the rows that turned into each state since the view
// before: with the totals of both, the effect of the changes in between.
func (v View) TurnedSince(before View) map[State]int {
	counts := map[State]int{}

	for _, row := range v.Rows {
		i := slices.IndexFunc(before.Rows, func(was Row) bool { return was.Key == row.Key })
		if i >= 0 && before.Rows[i].State != row.State {
			counts[row.State]++
		}
	}

	return counts
}

// ids are the ids of presets.
func ids(presets []recordPreset) []string {
	out := make([]string, 0, len(presets))
	for _, preset := range presets {
		out = append(out, preset.ID)
	}

	return out
}

// recordPresets are the active presets as a save records them, each with a
// hash of its members.
func (s *Session) recordPresets() []recordPreset {
	out := make([]recordPreset, 0, len(s.active))

	for _, active := range s.active {
		sum := sha256.New()

		for _, preset := range s.library {
			if preset.ID != active {
				continue
			}

			for _, member := range preset.Members {
				_, _ = sum.Write([]byte(member.Key + "\n")) // a hash never fails to write
			}
		}

		out = append(out, recordPreset{ID: active, Hash: hex.EncodeToString(sum.Sum(nil)[:8])})
	}

	return out
}

// base is the state ext has in agent without an Override. With active
// presets, it is on for a member of one of them and off for everything else;
// with none, and for an MCP server that follows its plugin, it is agent's
// default.
func (s *Session) base(agent Agent, ext Extension, active []string) State {
	if len(active) == 0 || ext.plugin != "" {
		return ext.fallback[agent]
	}

	for _, preset := range s.library {
		if slices.Contains(active, preset.ID) &&
			slices.ContainsFunc(preset.Members, func(m Member) bool { return m.Key == ext.Key }) {
			return On
		}
	}

	return Off
}

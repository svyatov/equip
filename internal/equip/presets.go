package equip

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
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
	Projects []string // the paths of the projects on this machine whose records have it active
	Active   bool     // active in this Project, pending
	New      bool     // created in this Session and not written yet
	// Unwritten reports unwritten edits, which Members show: the ones added
	// and the ones removed.
	Unwritten bool
}

// ErrUnwrittenEdits is the error of an edit of a preset while another one
// has unwritten edits.
var ErrUnwrittenEdits = errors.New("another preset has unwritten edits")

// ErrPresetName is the error of a preset name that is empty, taken, or
// cannot name a file.
var ErrPresetName = errors.New("a preset needs a free name that can name a file")

// errNoPreset is the error of an edit of a preset the library does not have.
var errNoPreset = errors.New("no such preset")

// Member is one extension a Preset names, which may not be installed. An
// installed one has its row here: its state, cost and Override mark.
type Member struct {
	Row

	Installed bool
	Added     bool // an unwritten edit adds it
	Removed   bool // an unwritten edit removes it
}

// member is the member with key, as a preset file names it.
func member(key string) Member {
	// A named Row: go fix flattens a nested Row literal into promoted
	// fields, and exhaustruct v5.0.3 panics on those.
	row := Row{
		Key: key, Name: keyName(key), Kind: keyKind(key), Cost: 0, State: On, Fallback: On, CostUnknown: false,
		Override: false, Unsaved: false, ChangedOutside: false,
	}

	return Member{Row: row, Installed: false, Added: false, Removed: false}
}

// presetFile is a preset as its file keeps it: members by kind, as record
// tables name the kinds.
type presetFile struct {
	ID         string   `toml:"id"`
	Skills     []string `toml:"skills,omitempty"`
	Plugins    []string `toml:"plugins,omitempty"`
	MCPServers []string `toml:"mcp_servers,omitempty"`
}

// errNoID is the error of a preset file with no id, which a Project cannot
// keep active.
var errNoID = errors.New("preset has no id")

// errTakenID is the error of a preset file whose id another preset file
// has, as a copied file does.
var errTakenID = errors.New("shares the id")

// presetsDir is the dir of the presets, one file each, named after the preset.
func presetsDir(machine Machine) string { return filepath.Join(machine.ConfigHome, "equip", "presets") }

// readPresets reads every preset, by name.
func readPresets(machine Machine) ([]Preset, error) {
	// Glob sorts by file name, which is the preset's name.
	files, _ := filepath.Glob(filepath.Join(presetsDir(machine), "*.toml"))
	presets := make([]Preset, 0, len(files))
	byID := map[string]string{} // the file of each id

	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // equip builds the path
		if err != nil {
			return nil, fmt.Errorf("read preset: %w", err)
		}

		var preset presetFile

		// Strict, so a misspelled kind fails here and does not turn its
		// members off.
		err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&preset)

		switch {
		case err != nil:
		case preset.ID == "":
			err = errNoID
		case byID[preset.ID] != "":
			err = fmt.Errorf("%w %q with %s", errTakenID, preset.ID, byID[preset.ID])
		}

		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}

		byID[preset.ID] = file

		var members []Member

		for kind, names := range [][]string{Skill: preset.Skills, Plugin: preset.Plugins, MCPServer: preset.MCPServers} {
			for _, name := range slices.Sorted(slices.Values(names)) {
				members = append(members, member(Kind(kind).keyOf(name)))
			}
		}

		presets = append(presets, Preset{
			ID: preset.ID, Name: strings.TrimSuffix(filepath.Base(file), ".toml"), Members: members,
			Projects: nil, Active: false, New: false, Unwritten: false,
		})
	}

	return presets, nil
}

// Presets returns the library: every preset, by name.
func (s *Session) Presets() []Preset {
	// ponytail: reads every record on each call; keep the counts if a
	// machine ever holds enough records to notice.
	// An Orphan's project is gone, so it uses no preset.
	recs := slices.DeleteFunc(records(s.machine), func(rec record) bool {
		_, err := os.Stat(rec.Path)

		return errors.Is(err, fs.ErrNotExist)
	})
	out := make([]Preset, 0, len(s.library))

	for _, preset := range s.library {
		preset.Active = slices.Contains(s.active, preset.ID)
		preset.Members = slices.Clone(preset.Members)

		if s.draft != nil && s.draft.ID == preset.ID {
			preset.Members, preset.Unwritten = edits(preset.Members, s.draft.Members), true
		}

		for i := range preset.Members {
			m := &preset.Members[i]

			var ext Extension
			if ext, m.Installed = s.ext(m.Key); m.Installed {
				m.Row = s.row(ext)
			}
		}

		for _, rec := range recs {
			if slices.Contains(ids(rec.Presets), preset.ID) {
				preset.Projects = append(preset.Projects, rec.Path)
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

// TogglePreset makes the preset with id active in the Project, or no longer
// active. The other active presets stay, a missing one too.
func (s *Session) TogglePreset(id string) {
	active := slices.Clone(s.active)
	if i := slices.Index(active, id); i >= 0 {
		s.SetPresets(slices.Delete(active, i, i+1))
	} else {
		s.SetPresets(append(active, id))
	}
}

// TurnedSince returns the rows whose state changed since the view before:
// with the totals of both, the effect of the changes in between.
func (v View) TurnedSince(before View) []Row {
	var turned []Row

	for _, row := range v.Rows {
		i := slices.IndexFunc(before.Rows, func(was Row) bool { return was.Key == row.Key })
		if i >= 0 && before.Rows[i].State != row.State {
			turned = append(turned, row)
		}
	}

	return turned
}

// ids are the ids of presets.
func ids(presets []recordPreset) []string {
	out := make([]string, 0, len(presets))
	for _, preset := range presets {
		out = append(out, preset.ID)
	}

	return out
}

// recordPresets are the active presets with ids as a record keeps them, each
// with a hash of its members.
func (s *Session) recordPresets(ids []string) []recordPreset {
	out := make([]recordPreset, 0, len(ids))

	for _, active := range ids {
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

// CreatePreset creates the preset name, with no members and unwritten, and
// returns its id.
func (s *Session) CreatePreset(name string) (string, error) {
	if s.draft != nil {
		return "", ErrUnwrittenEdits
	}

	preset := Preset{
		ID: rand.Text(), Name: name, Members: nil, Projects: nil, Active: false, New: true, Unwritten: false,
	}

	err := s.checkName(preset.ID, name)
	if err != nil {
		return "", err
	}

	s.library = append(s.library, preset)
	slices.SortFunc(s.library, byName)
	s.draft = &preset

	return preset.ID, nil
}

// AddMember adds the extension with key to the preset with id, an unwritten
// edit.
func (s *Session) AddMember(id, key string) error {
	return s.edit(id, func(members []Member) []Member {
		return append(members, member(key))
	})
}

// RemoveMember removes the extension with key from the preset with id, an
// unwritten edit.
func (s *Session) RemoveMember(id, key string) error {
	return s.edit(id, func(members []Member) []Member {
		return slices.DeleteFunc(members, func(m Member) bool { return m.Key == key })
	})
}

// edit makes change to the members of the preset with id, an unwritten edit.
// Only one preset has unwritten edits at a time.
func (s *Session) edit(presetID string, change func([]Member) []Member) error {
	if s.draft != nil && s.draft.ID != presetID {
		return ErrUnwrittenEdits
	}

	at := s.presetIndex(presetID)
	if at < 0 {
		return fmt.Errorf("%w: %s", errNoPreset, presetID)
	}

	written := s.library[at]
	if s.draft == nil {
		draft := written
		draft.Members = slices.Clone(written.Members)
		s.draft = &draft
	}

	s.draft.Members = change(s.draft.Members)
	slices.SortFunc(s.draft.Members, byKind)
	// Undone to the preset as written, which has no unwritten edits left.
	if !written.New && !slices.ContainsFunc(edits(written.Members, s.draft.Members), func(m Member) bool {
		return m.Added || m.Removed
	}) {
		s.draft = nil
	}

	return nil
}

// RenamePreset names the preset with id name. It renames its file at once,
// and every Project that uses the preset still does.
func (s *Session) RenamePreset(presetID, name string) error {
	index := s.presetIndex(presetID)
	if index < 0 {
		return fmt.Errorf("%w: %s", errNoPreset, presetID)
	}

	err := s.checkName(presetID, name)
	if err != nil {
		return err
	}

	if !s.library[index].New {
		err := os.Rename(presetPath(s.machine, s.library[index].Name), presetPath(s.machine, name))
		if err != nil {
			return fmt.Errorf("rename preset: %w", err)
		}
	}

	s.library[index].Name = name
	slices.SortFunc(s.library, byName)

	return nil
}

// checkName reports ErrPresetName unless name can name the preset with id:
// trimmed, not hidden, one file name, and no other preset's in any case, as
// a disk may not tell case apart.
func (s *Session) checkName(id, name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.HasPrefix(name, ".") ||
		strings.ContainsAny(name, `/\`) ||
		slices.ContainsFunc(s.library, func(p Preset) bool { return p.ID != id && strings.EqualFold(p.Name, name) }) {
		return fmt.Errorf("%w: %q", ErrPresetName, name)
	}

	return nil
}

// presetIndex is the index of the preset with id in the library, or -1.
func (s *Session) presetIndex(id string) int {
	return slices.IndexFunc(s.library, func(p Preset) bool { return p.ID == id })
}

// presetPath is the file of the preset name.
func presetPath(machine Machine, name string) string {
	return filepath.Join(presetsDir(machine), name+".toml")
}

// byName orders presets by name, as their files sort.
func byName(a, b Preset) int { return cmp.Compare(a.Name, b.Name) }

// DiscardPreset drops the unwritten edits, and a preset not written yet.
func (s *Session) DiscardPreset() {
	s.library = slices.DeleteFunc(s.library, func(p Preset) bool { return p.New })
	s.draft = nil
}

// byKind orders members by kind, then by name.
func byKind(a, b Member) int { return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name)) }

// edits are the members of written, a preset's as written, and of draft, as
// edited, each added or removed marked so.
func edits(written, draft []Member) []Member {
	has := func(members []Member, m Member) bool {
		return slices.ContainsFunc(members, func(other Member) bool { return other.Key == m.Key })
	}

	var out []Member

	for _, m := range draft {
		m.Added = !has(written, m)
		out = append(out, m)
	}

	for _, m := range written {
		if !has(draft, m) {
			m.Removed = true
			out = append(out, m)
		}
	}

	slices.SortFunc(out, byKind)

	return out
}

// PreviewWrite returns the views of the Project's saved states before and
// after the write of the preset with unwritten edits: what the write changes
// here, pending changes left out. It writes nothing.
func (s *Session) PreviewWrite() (View, View) {
	overrides, active, library := s.overrides, s.active, s.library
	defer func() { s.overrides, s.active, s.library = overrides, active, library }()

	s.overrides, s.active = s.saved, ids(s.recorded)
	before := s.View()

	if s.draft != nil {
		s.library = slices.Clone(library)
		s.library[s.presetIndex(s.draft.ID)].Members = s.draft.Members
	}

	return before, s.View()
}

// WritePreset writes the preset with unwritten edits. If the Project saved it
// active, it rewrites the Project's agent config and record with the states
// the preset changes, and pending changes stay pending. If an agent's entries
// changed since they were read, it writes nothing, imports the changes and
// returns ErrChangedSinceOpen.
func (s *Session) WritePreset() error {
	if s.draft == nil {
		return nil
	}

	active := ids(s.recorded)
	here := slices.Contains(active, s.draft.ID)

	if here {
		changed, err := s.changedOutside()
		if err != nil {
			return err
		}

		if changed {
			return ErrChangedSinceOpen
		}
	}

	err := s.writeDraft()
	if err == nil && here {
		err = s.rewriteHere(active)
	}
	// Kept until every write lands, so a failed one can be written again.
	if err == nil {
		s.draft = nil
	}

	return err
}

// rewriteHere writes the saved states, with the presets as written, into the
// Project's agent config, and the record with the active presets with ids.
func (s *Session) rewriteHere(active []string) error {
	err := s.writeAgents(s.saved, active)
	if err != nil {
		return err
	}

	presets := s.recordPresets(active)

	err = writeRecord(s.machine, s.project, s.saved, presets)
	if err != nil {
		return err
	}

	s.recorded = presets

	return nil
}

// writeDraft writes the preset with unwritten edits into its file, and takes
// it into the library.
func (s *Session) writeDraft() error {
	names := map[Kind][]string{}
	for _, member := range s.draft.Members {
		names[member.Kind] = append(names[member.Kind], member.Name)
	}

	data, err := toml.Marshal(presetFile{
		ID: s.draft.ID, Skills: names[Skill], Plugins: names[Plugin], MCPServers: names[MCPServer],
	})
	if err != nil {
		return fmt.Errorf("encode preset: %w", err)
	}

	written := &s.library[s.presetIndex(s.draft.ID)]

	err = writeFile(presetPath(s.machine, written.Name), data)
	if err != nil {
		return err
	}

	// A clone, as the draft stays until every write lands.
	written.Members, written.New = slices.Clone(s.draft.Members), false

	return nil
}

// Unwritten reports whether a preset has unwritten edits.
func (s *Session) Unwritten() bool { return s.draft != nil }

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

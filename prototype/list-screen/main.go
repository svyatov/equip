// PROTOTYPE, throwaway. Three structurally different variants of equip's
// preset screens (picker, editor, propagation confirm), opened with p from the
// approved main screen. F1-F3 or ctrl+t switch the variant, -variant a|b|c
// picks one at start. State is shared, so a change in one shows in the others.
// Nothing is written to disk.
//
// Run: cd prototype/list-screen && go run . [-variant b]
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
	"github.com/svyatov/equip/prototype/list-screen/mainscreen"
	presetsa "github.com/svyatov/equip/prototype/list-screen/presets/a"
	presetsb "github.com/svyatov/equip/prototype/list-screen/presets/b"
	presetsc "github.com/svyatov/equip/prototype/list-screen/presets/c"
)

// Variant is one candidate for the preset screens. It gets every message
// except the switch keys while open, and returns data.ClosePresets to go back.
type Variant interface {
	Name() string
	Open() // called each time p opens it
	SetSize(w, h int)
	Update(tea.Msg) tea.Cmd
	View() string
}

type model struct {
	store    *data.Store
	main     *mainscreen.Model
	variants []Variant
	cur      int
	open     bool
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.main.SetSize(msg.Width, msg.Height-1)
		for _, v := range m.variants {
			v.SetSize(msg.Width, msg.Height-1)
		}
		return m, nil
	case data.OpenPresets:
		m.open = true
		m.variants[m.cur].Open()
		return m, nil
	case data.ClosePresets:
		m.open = false
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+t", "f1", "f2", "f3":
			if msg.String() == "ctrl+t" {
				m.cur = (m.cur + 1) % len(m.variants)
			} else {
				m.cur = int(msg.String()[1] - '1')
			}
			if m.open {
				m.variants[m.cur].Open()
			}
			return m, nil
		}
	}
	if m.open {
		return m, m.variants[m.cur].Update(msg)
	}
	return m, m.main.Update(msg)
}

var (
	barStyle = lipgloss.NewStyle().Background(lipgloss.Color("#5f00af")).Foreground(lipgloss.Color("#ffffff"))
	barCur   = barStyle.Bold(true).Reverse(true)
)

func (m *model) View() tea.View {
	var tabs []string
	for i, v := range m.variants {
		t := fmt.Sprintf(" F%d %c · %s ", i+1, 'A'+i, v.Name())
		if i == m.cur {
			tabs = append(tabs, barCur.Render(t))
		} else {
			tabs = append(tabs, barStyle.Render(t))
		}
	}
	screen := m.main.View()
	if m.open {
		screen = m.variants[m.cur].View()
	}
	bar := barStyle.Render(" PROTOTYPE presets ") + strings.Join(tabs, "") +
		barStyle.Render(fmt.Sprintf(" ctrl+t next │ %d unsaved │ ctrl+c quit ", m.store.Unsaved()))
	v := tea.NewView(screen + "\n" + bar)
	v.AltScreen = true
	return v
}

func main() {
	start := flag.String("variant", "a", "a, b, or c")
	flag.Parse()
	s := data.Fake()
	m := &model{store: s, main: mainscreen.New(s),
		variants: []Variant{presetsa.New(s), presetsb.New(s), presetsc.New(s)}}
	m.cur = max(0, min(len(m.variants)-1, int((*start)[0]-'a')))
	if _, err := tea.NewProgram(m).Run(); err != nil {
		log.Fatal(err)
	}
}

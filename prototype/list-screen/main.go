// PROTOTYPE, throwaway. Three structurally different variants of equip's main
// extension list screen, over the same fake data, switchable with F1-F3 or
// ctrl+t, or -variant a|b|c at start. State is shared, so a toggle in one
// variant shows in the others. Nothing is written to disk.
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
	"github.com/svyatov/equip/prototype/list-screen/variants/a"
	"github.com/svyatov/equip/prototype/list-screen/variants/b"
	"github.com/svyatov/equip/prototype/list-screen/variants/c"
)

// Variant is one candidate screen. It gets every message except the switch keys.
type Variant interface {
	Name() string
	SetSize(w, h int)
	Update(tea.Msg) tea.Cmd
	View() string
}

type model struct {
	store    *data.Store
	variants []Variant
	cur      int
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		for _, v := range m.variants {
			v.SetSize(msg.Width, msg.Height-1)
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+t":
			m.cur = (m.cur + 1) % len(m.variants)
			return m, nil
		case "f1", "f2", "f3":
			m.cur = int(msg.String()[1] - '1')
			return m, nil
		}
	}
	return m, m.variants[m.cur].Update(msg)
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
	bar := barStyle.Render(" PROTOTYPE ") + strings.Join(tabs, "") +
		barStyle.Render(fmt.Sprintf(" ctrl+t next │ %d unsaved │ ctrl+c quit ", m.store.Unsaved()))
	v := tea.NewView(m.variants[m.cur].View() + "\n" + bar)
	v.AltScreen = true
	return v
}

func main() {
	start := flag.String("variant", "a", "a, b, or c")
	flag.Parse()
	s := data.Fake()
	m := &model{store: s, variants: []Variant{a.New(s), b.New(s), c.New(s)}}
	m.cur = max(0, min(len(m.variants)-1, int((*start)[0]-'a')))
	if _, err := tea.NewProgram(m).Run(); err != nil {
		log.Fatal(err)
	}
}

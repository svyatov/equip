// Package a is a PROTOTYPE stub, throwaway: replaced by a preset-screens variant.
package a

import (
	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
)

type Model struct{ s *data.Store }

func New(s *data.Store) *Model    { return &Model{s: s} }
func (m *Model) Name() string     { return "stub" }
func (m *Model) Open()            {}
func (m *Model) SetSize(w, h int) {}
func (m *Model) View() string     { return "stub: esc goes back" }
func (m *Model) Update(tea.Msg) tea.Cmd {
	return func() tea.Msg { return data.ClosePresets{} }
}

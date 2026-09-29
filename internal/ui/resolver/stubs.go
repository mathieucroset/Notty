package resolver

// Temporary stand-ins for the parts of the resolver added by the following
// commits (text keys and view, the embedded editor, the choice screens).

import (
	"reflect"

	tea "charm.land/bubbletea/v2"
)

type acceptEditMsg struct{}

type leaveEditMsg struct{}

type editStatusMsg struct{ text string }

var cmdType = reflect.TypeFor[tea.Cmd]()

func (m Model) textKey(string) (Model, tea.Cmd) { return m, nil }

func (m Model) jump(int) Model { return m }

func (m Model) scrollToCurrent() Model { return m }

func (m Model) editKey(tea.KeyPressMsg) (Model, tea.Cmd) { return m, nil }

func (m Model) forwardToEditor(tea.Msg) (Model, tea.Cmd) { return m, nil }

func (m Model) acceptEdit() (Model, tea.Cmd) { return m, nil }

func (m Model) leaveEdit() Model { return m }

func (m Model) editorSize() (int, int) { return m.w, m.h }

type choiceState struct{}

func newChoiceState(File) *choiceState { return &choiceState{} }

func (m Model) choiceKey(string) (Model, tea.Cmd) { return m, nil }

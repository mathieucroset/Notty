package resolver

// Temporary stand-ins for the parts of the resolver added by the following
// commits (the embedded editor, the choice screens).

import (
	"reflect"

	tea "charm.land/bubbletea/v2"
)

type acceptEditMsg struct{}

type leaveEditMsg struct{}

type editStatusMsg struct{ text string }

var cmdType = reflect.TypeFor[tea.Cmd]()

const editNote = ""

func (m Model) openEditor() (Model, tea.Cmd) { return m, nil }

func (m Model) editKey(tea.KeyPressMsg) (Model, tea.Cmd) { return m, nil }

func (m Model) forwardToEditor(tea.Msg) (Model, tea.Cmd) { return m, nil }

func (m Model) acceptEdit() (Model, tea.Cmd) { return m, nil }

func (m Model) leaveEdit() Model { return m }

func (m Model) editorSize() (int, int) { return m.w, m.h }

type option struct{}

type choiceState struct{ options []option }

func newChoiceState(File) *choiceState { return &choiceState{} }

func (m Model) choiceKey(string) (Model, tea.Cmd) { return m, nil }

func (m Model) choiceContent(*item, int, int) []string { return nil }

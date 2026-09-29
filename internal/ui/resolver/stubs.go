package resolver

// Temporary stand-ins for the choice screens, added by the next commit.

import (
	tea "charm.land/bubbletea/v2"
)

type option struct{}

type choiceState struct{ options []option }

func newChoiceState(File) *choiceState { return &choiceState{} }

func (m Model) choiceKey(string) (Model, tea.Cmd) { return m, nil }

func (m Model) choiceContent(*item, int, int) []string { return nil }

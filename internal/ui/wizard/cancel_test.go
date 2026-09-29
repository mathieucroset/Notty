package wizard

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/setup"
)

// waitDone fails the test unless ch is closed within a second.
func waitDone(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("%s did not return after Cancel", what)
	}
}

func TestCancelStopsRunningSetup(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo}
	started, returned := make(chan struct{}), make(chan struct{})
	var runErr error
	f.run = func(ctx context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		defer close(returned)
		progress(0, steps[0])
		close(started)
		<-ctx.Done()
		runErr = ctx.Err()
		return false, &setup.StepError{Index: 0, Step: steps[0], Err: ctx.Err()}
	}
	m := New(SetupSync, "/v", "nord", f.env(), testStyles()).SetSize(100, 40)
	m, _ = send(t, m, ghMsg{ok: false})
	m, _ = press(t, m, "down")
	m, cmd := m.Update(key("enter"))
	m, cmd = stepOnce(t, m, cmd) // identity checked
	m, cmd = stepOnce(t, m, cmd) // planned: cmd now executes the steps

	msgs := make(chan []tea.Msg, 1)
	go func() { msgs <- exec(cmd) }()
	<-started
	// esc while running stays ignored.
	m, _ = press(t, m, "esc")
	if m.Stage() != StageRun {
		t.Fatalf("esc while running changed the stage to %v", m.Stage())
	}
	m = m.Cancel()
	waitDone(t, returned, "the runner")
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("runner context error = %v, want context.Canceled", runErr)
	}
	// Drain what the run delivered: a late result must not emit anything.
	for _, msg := range <-msgs {
		var out []tea.Msg
		m, out = send(t, m, msg)
		if len(out) != 0 {
			t.Errorf("a canceled run emitted %v", out)
		}
	}
}

func TestCancelStopsRemoteCheckAndIdentityCheck(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	env := f.env()
	remoteDone := make(chan struct{})
	env.InspectRemote = func(ctx context.Context, _ string) (setup.RemoteState, error) {
		defer close(remoteDone)
		<-ctx.Done()
		return setup.RemoteState{}, ctx.Err()
	}
	m := New(SetupSync, "/v", "nord", env, testStyles()).SetSize(100, 40)
	m, _ = press(t, m, "enter") // gh unknown: cursor on URL
	m = typeText(t, m, "/srv/n.git")
	m, cmd := m.Update(key("enter"))
	go exec(cmd)
	_ = m.Cancel()
	waitDone(t, remoteDone, "InspectRemote")

	identityDone := make(chan struct{})
	env.CheckIdentity = func(ctx context.Context, _ string) error {
		defer close(identityDone)
		<-ctx.Done()
		return ctx.Err()
	}
	m2 := New(SetupSync, "/v", "nord", env, testStyles()).SetSize(100, 40)
	m2, _ = press(t, m2, "down")
	m2, cmd = m2.Update(key("enter"))
	go exec(cmd)
	_ = m2.Cancel()
	waitDone(t, identityDone, "CheckIdentity")
}

func TestCancelIsSafeWhenIdle(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m = m.Cancel().Cancel()
	if m.Stage() != StageVault {
		t.Errorf("stage = %v", m.Stage())
	}
}

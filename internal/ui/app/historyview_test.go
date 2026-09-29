package app

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// historyOptions returns options on a git vault where ideas.md has three
// committed versions.
func historyOptions(t *testing.T) Options {
	t.Helper()
	env := gittest.New(t)
	for _, body := range []string{"first", "second", "third"} {
		gittest.Write(t, env.Laptop, "ideas.md", "# Ideas\n\n"+body+" version\n")
		gittest.CommitAll(t, env.Laptop, "Update ideas.md · laptop")
	}
	v, err := vault.Open(env.Laptop.Dir)
	if err != nil {
		t.Fatal(err)
	}
	opts := testOptions(t)
	opts.Vault = v
	return opts
}

func TestHistoryListingAndRestore(t *testing.T) {
	opts := historyOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenHistoryMsg{Path: "ideas.md"})
	if m.history == nil {
		t.Fatalf("history view not open; toasts = %v", toastTexts(m))
	}
	if got := m.keyContext(); got != keys.History {
		t.Errorf("keyContext = %v, want History", got)
	}
	s := screen(m)
	if strings.Count(s, "laptop") < 3 {
		t.Errorf("want 3 entries with their host:\n%s", s)
	}
	if !strings.Contains(s, "third version") {
		t.Errorf("newest version not shown:\n%s", s)
	}
	assertSize(t, m, 120, 30)

	// Select the middle version, then restore it.
	run(t, m, keyMsg("j"))
	if !strings.Contains(screen(m), "second version") {
		t.Errorf("selected version not loaded:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if m.history != nil {
		t.Error("history view still open after restoring")
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got != "# Ideas\n\nsecond version\n" {
		t.Errorf("restored content = %q", got)
	}
	if !hasToast(m, msgs.ToastInfo, "Restored ideas") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if n, _ := m.ix.Get("ideas.md"); n == nil || !strings.Contains(n.Content, "second version") {
		t.Error("restored content not indexed")
	}
}

func TestHistoryCloses(t *testing.T) {
	m := start(t, historyOptions(t), 120, 30)
	run(t, m, msgs.OpenHistoryMsg{Path: "ideas.md"})
	if m.history == nil {
		t.Fatal("history view not open")
	}
	run(t, m, keyMsg("esc"))
	if m.history != nil {
		t.Error("esc did not close the history view")
	}
	if !strings.Contains(screen(m), "N O T E S") {
		t.Errorf("main screen not back:\n%s", screen(m))
	}
}

func TestHistoryRestoreToastWaitsForTheSave(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	// Saving over a folder fails.
	run(t, m, history.RestoreVersionMsg{Path: "Work", Rev: "0123456789abcdef", Content: "old"})
	if hasToast(m, msgs.ToastInfo, "Restored") {
		t.Errorf("restore toast shown although the save failed: %v", toastTexts(m))
	}
	if !hasToast(m, msgs.ToastError, "Could not save Work") {
		t.Errorf("toasts = %v, want the save error", toastTexts(m))
	}
}

func TestHistoryNeedsGit(t *testing.T) {
	gittest.Isolate(t) // a temp vault, never inside another repository
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.OpenHistoryMsg{Path: "ideas.md"})
	if m.history != nil {
		t.Error("history opened outside a git repository")
	}
	if !hasToast(m, msgs.ToastInfo, "History needs git") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

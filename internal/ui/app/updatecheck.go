package app

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/update"
)

// updateTimeout bounds the release check's network request.
const updateTimeout = 5 * time.Second

// UpdateCheckOptions configures the check for a newer release.
type UpdateCheckOptions struct {
	EnvDisabled bool         // NOTTY_NO_UPDATE_CHECK=1
	StatePath   string       // <StateDir>/update-check.json; "" disables
	APIURL      string       // "" → update.DefaultAPIURL
	Client      *http.Client // nil → http.DefaultClient
	Exe         string       // os.Executable(), symlinks resolved
	GoBin       string       // symlinks resolved
}

// updateResultMsg carries the outcome of the release check.
type updateResultMsg struct{ outcome update.Outcome }

// startUpdateCheck starts the release check, once per session: Init runs
// again when the wizard has opened the vault. It is skipped when disabled
// by the environment or the config (read now, so the settings of a vault
// opened by the wizard count), without a state file, or for a build that is
// not a release.
func (m *Model) startUpdateCheck() tea.Cmd {
	if m.updateStarted {
		return nil
	}
	m.updateStarted = true
	uc := m.opts.UpdateCheck
	if uc.EnvDisabled || !m.opts.Config.UpdateCheck || uc.StatePath == "" || !update.Releasable(m.opts.Version) {
		return nil
	}
	return updateCheckCmd(uc, m.opts.Version, m.now())
}

// updateCheckCmd loads the saved state, fetches the latest release when a
// check is due, and saves the updated state, off the UI goroutine.
// Failures are only logged: the check never shows an error.
func updateCheckCmd(uc UpdateCheckOptions, version string, now time.Time) tea.Cmd {
	return func() tea.Msg {
		s := update.LoadState(uc.StatePath)
		var fetched *update.Release
		if update.Due(s, now) {
			ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
			rel, err := update.Latest(ctx, uc.Client, cmp.Or(uc.APIURL, update.DefaultAPIURL), version)
			cancel()
			if err != nil {
				slog.Debug("update check failed", "err", err)
				s = update.Failed(s, now)
			} else {
				fetched = &rel
			}
		}
		out := update.Apply(s, fetched, version, now)
		if err := out.State.Save(uc.StatePath); err != nil {
			slog.Debug("update check state not saved", "err", err)
		}
		return updateResultMsg{outcome: out}
	}
}

// handleUpdateResult shows the status bar marker for a newer release and,
// the first time that version is seen, a toast saying how to upgrade.
func (m *Model) handleUpdateResult(msg updateResultMsg) tea.Cmd {
	v := msg.outcome.Available.Version
	if v == "" {
		return nil
	}
	m.updateVersion = v
	if !msg.outcome.Toast {
		return nil
	}
	hint := update.UpgradeHint(m.opts.UpdateCheck.Exe, m.opts.UpdateCheck.GoBin, runtime.GOOS)
	return emit(msgs.ToastMsg{Level: msgs.ToastInfo, Text: "Notty " + v + " is available — " + hint})
}

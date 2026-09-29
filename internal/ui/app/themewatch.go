package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// themeDebounce is how long the theme watcher waits after the last event
// before reporting: matugen may truncate a file, then write it.
const themeDebounce = 200 * time.Millisecond

// themeWatcher reports which user theme files changed in the themes
// directory, debounced (user themes spec §3). It is separate from the
// vault watcher.
type themeWatcher struct {
	w   *fsnotify.Watcher
	out chan []string
	// done is closed by Close; exited when the loop has returned.
	done, exited chan struct{}
	closeOnce    sync.Once
}

// newThemeWatcher watches dir, creating it if absent.
func newThemeWatcher(dir string) (*themeWatcher, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("themes: %w", err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("themes: %w", err)
	}
	if err := fw.Add(dir); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("themes: %w", err)
	}
	tw := &themeWatcher{
		w:      fw,
		out:    make(chan []string, 1),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	go tw.loop()
	return tw, nil
}

// loop collects the theme names of file events and sends them, sorted,
// once no event came for themeDebounce.
func (tw *themeWatcher) loop() {
	defer close(tw.exited)
	pending := map[string]bool{}
	timer := time.NewTimer(themeDebounce)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-tw.w.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue // attributes only: the colors did not change
			}
			if n, ok := strings.CutSuffix(filepath.Base(ev.Name), theme.ThemeExt); ok && n != "" {
				pending[n] = true
				timer.Reset(themeDebounce)
			}
		case err, ok := <-tw.w.Errors:
			if !ok {
				return
			}
			slog.Warn("theme watcher", "err", err)
		case <-timer.C:
			names := slices.Sorted(maps.Keys(pending))
			clear(pending)
			select {
			case tw.out <- names:
			case <-tw.done:
				return
			}
		case <-tw.done:
			return
		}
	}
}

// Close stops the watcher and waits for its loop to end. It may be called
// more than once.
func (tw *themeWatcher) Close() error {
	var err error
	tw.closeOnce.Do(func() {
		close(tw.done)
		err = tw.w.Close()
		<-tw.exited
	})
	return err
}

// themeFilesMsg carries the names of the theme files that changed.
type themeFilesMsg struct{ names []string }

// listenThemesCmd waits for the theme watcher's next batch. It returns nil
// (ending the loop) once the watcher is closed.
func listenThemesCmd(tw *themeWatcher) tea.Cmd {
	if tw == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case names := <-tw.out:
			return themeFilesMsg{names: names}
		case <-tw.done:
			return nil
		}
	}
}

// startThemeWatcher starts watching the themes directory, once per app
// (Init runs again when the wizard opens the vault), and returns the
// command listening to it. Without a themes directory, or when the watcher
// cannot start, Notty runs without live reload.
func (m *Model) startThemeWatcher() tea.Cmd {
	if m.themesStarted || m.opts.Catalog.Dir == "" {
		return nil
	}
	m.themesStarted = true
	tw, err := newThemeWatcher(m.opts.Catalog.Dir)
	if err != nil {
		slog.Warn("no live theme reload", "err", err)
		return nil
	}
	m.themes = tw
	return listenThemesCmd(tw)
}

// closeThemeWatcher stops the theme watcher, if any.
func (m *Model) closeThemeWatcher() {
	if m.themes != nil {
		_ = m.themes.Close()
		m.themes = nil
	}
}

// handleThemeFiles applies the theme files that changed (user themes spec
// §3): the open picker's and wizard's lists are refreshed, and each
// changed user theme the app shows or wants is read again.
//
//   - The displayed palette is replaced when its Name is the file's, or,
//     with no picker or wizard open (nothing previewed), when it is the
//     theme the user wants (a startup fallback whose file appeared).
//   - With the picker open, the original it restores is replaced when its
//     Name is the file's or the file is the theme the user wants, so
//     leaving the picker shows the new colors.
//
// A file that does not load keeps the colors, with one warning toast per
// distinct error; a removed file keeps them silently until it comes back.
func (m *Model) handleThemeFiles(names []string) tea.Cmd {
	var cmds []tea.Cmd
	all := m.opts.Catalog.Names()
	pal := m.paletteOverlay()
	if pal != nil {
		pal.palette = pal.palette.SetThemeNames(all)
	}
	if m.wizard != nil {
		w, cmd := m.wizard.SetThemeNames(all)
		m.wizard = &w
		cmds = append(cmds, cmd)
	}
	for _, n := range names {
		if !m.opts.Catalog.IsUser(n) {
			continue
		}
		shown := m.opts.Palette.Name == n
		orig := pal != nil && (pal.paletteOrig.Name == n || m.themeName == n)
		want := pal == nil && m.wizard == nil && m.themeName == n
		if !shown && !orig && !want {
			continue
		}
		p, err := m.opts.Catalog.Resolve(n)
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed: keep the colors until it comes back
		}
		if err != nil {
			if text := themeLoadWarning(err); text != m.lastThemeErr {
				m.lastThemeErr = text
				cmds = append(cmds, m.pushToast(msgs.ToastWarn, text))
			}
			continue
		}
		m.lastThemeErr = ""
		if orig {
			pal.paletteOrig = p
		}
		if (shown || want) && p.Key() != m.opts.Palette.Key() {
			m.applyPalette(p)
		}
	}
	return tea.Batch(cmds...)
}

package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
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
	dir string
	// parent is dir's parent when it is watched too (Windows), else "".
	parent string
	out    chan []string
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
		dir:    filepath.Clean(dir),
		out:    make(chan []string, 1),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	if runtime.GOOS == "windows" {
		// On Windows a directory's own watch never reports that directory
		// being removed or renamed; its parent's watch does. Best effort:
		// without it, only a deleted themes directory is noticed.
		if parent := filepath.Dir(tw.dir); parent != tw.dir && fw.Add(parent) == nil {
			tw.parent = parent
		}
	}
	go tw.loop()
	return tw, nil
}

// loop collects the theme names of file events and sends them, sorted,
// once no event came for themeDebounce.
//
// When the themes directory itself is removed or renamed, its watch is
// gone: once the events settle, the directory at that path is watched
// again (re-created when absent) and every theme file it holds is
// reported, since it may be another directory altogether.
func (tw *themeWatcher) loop() {
	defer close(tw.exited)
	pending := map[string]bool{}
	rewatch := false
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
			name := filepath.Clean(ev.Name)
			if name == tw.dir && ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				slog.Warn("themes directory removed or renamed: watching it again", "dir", tw.dir, "op", ev.Op.String())
				rewatch = true
				timer.Reset(themeDebounce)
				continue
			}
			if filepath.Dir(name) != tw.dir {
				continue // in the parent, or in a stale watch of a renamed directory
			}
			if n, ok := strings.CutSuffix(filepath.Base(name), theme.ThemeExt); ok && n != "" {
				pending[n] = true
				timer.Reset(themeDebounce)
			}
		case err, ok := <-tw.w.Errors:
			if !ok {
				return
			}
			slog.Warn("theme watcher", "err", err)
		case <-timer.C:
			if rewatch {
				rewatch = false
				tw.rewatch(pending)
			}
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

// rewatch watches the themes directory again after it was removed or
// renamed, re-creating it when absent, and adds the theme files it holds
// to pending. On failure live reload stops, with a log line.
func (tw *themeWatcher) rewatch(pending map[string]bool) {
	if err := os.MkdirAll(tw.dir, 0o700); err != nil {
		slog.Warn("live theme reload stopped", "err", err)
		return
	}
	_ = tw.w.Remove(tw.dir) // a stale watch of a renamed directory, if any
	for _, p := range tw.w.WatchList() {
		// On Windows a stale watch follows the renamed directory's new name.
		if p != tw.dir && p != tw.parent {
			_ = tw.w.Remove(p)
		}
	}
	if err := tw.w.Add(tw.dir); err != nil {
		slog.Warn("live theme reload stopped", "err", err)
		return
	}
	entries, err := os.ReadDir(tw.dir)
	if err != nil {
		slog.Warn("theme watcher", "err", err)
		return
	}
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), theme.ThemeExt); ok && n != "" {
			pending[n] = true
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
//     with neither the picker nor the first-run wizard open (nothing
//     previewed), when it is the theme the user wants (a startup fallback
//     whose file appeared).
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
		// The wizard reads its highlighted and current themes again.
		w, cmd := m.wizard.SetThemeNames(all, names)
		m.wizard = &w
		cmds = append(cmds, cmd)
	}
	for _, n := range names {
		if !m.opts.Catalog.IsUser(n) {
			continue
		}
		shown := m.opts.Palette.Name == n
		orig := pal != nil && (pal.paletteOrig.Name == n || m.themeName == n)
		// The first-run wizard previews themes in its theme step; the
		// "Set up sync" one has none.
		want := pal == nil && (m.wizard == nil || !m.opts.WizardNeeded) && m.themeName == n
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

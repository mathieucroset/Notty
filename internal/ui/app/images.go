package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/attach"
	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/imageviewer"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// dlgImportLarge confirms importing an image over images.max_import_mb.
const dlgImportLarge = "import-large"

// imageSizeMsg carries the size of an image file about to be imported
// into note.
type imageSizeMsg struct {
	note string
	path string
	mb   float64
	err  error
}

// imageImportedMsg reports an image copied into attachments/ for note,
// with the link to insert.
type imageImportedMsg struct {
	note string
	link string
	err  error
}

// imageViewerDoneMsg reports that the image viewer closed; quit is set
// when it was closed with ctrl+q.
type imageViewerDoneMsg struct {
	err  error
	quit bool
}

// importImage starts importing an image into the open note (spec §6.2):
// a file (pasted path, dropped file, :img) or clipboard bytes. Files over
// images.max_import_mb ask first.
func (m *Model) importImage(msg msgs.ImportImageMsg) tea.Cmd {
	if m.opts.Vault == nil || m.note.path == "" {
		return m.pushToast(msgs.ToastInfo, "Open a note to add an image")
	}
	if m.editor.ModeName() == "READ-ONLY" {
		return m.pushToast(msgs.ToastWarn, "Resolve the conflict in "+m.note.path+" first")
	}
	note := m.note.path
	if msg.Path != "" {
		p := config.ExpandHome(msg.Path)
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return func() tea.Msg {
			mb, err := attach.SizeMB(p)
			return imageSizeMsg{note: note, path: p, mb: mb, err: err}
		}
	}
	if len(msg.Data) == 0 {
		return nil
	}
	mb := float64(len(msg.Data)) / (1024 * 1024)
	if m.tooLarge(mb) {
		m.confirmLargeImport(fmt.Sprintf("The pasted image is %.1f MB", mb),
			pendingOp{kind: opImportImage, note: note, data: msg.Data, ext: msg.Ext})
		return nil
	}
	return importDataCmd(m.opts.Vault, note, msg.Data, msg.Ext, m.now())
}

// tooLarge reports whether an image of mb megabytes needs confirming.
func (m *Model) tooLarge(mb float64) bool {
	limit := m.opts.Config.Images.MaxImportMB
	return limit > 0 && mb > float64(limit)
}

// confirmLargeImport asks before importing an image over the size limit.
func (m *Model) confirmLargeImport(what string, op pendingOp) {
	msg := fmt.Sprintf("%s (limit %d MB). Import it anyway?", what, m.opts.Config.Images.MaxImportMB)
	d := dialog.NewConfirm(dlgImportLarge, "Large image", msg, "Import", "Cancel", false, m.opts.Styles)
	m.openDialog(d, op)
}

// handleImageSize imports a file whose size is known, asking first when
// it is over the limit.
func (m *Model) handleImageSize(msg imageSizeMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, "Could not add the image: "+imageError(msg.err))
	}
	if m.tooLarge(msg.mb) {
		m.confirmLargeImport(fmt.Sprintf("'%s' is %.1f MB", filepath.Base(msg.path), msg.mb),
			pendingOp{kind: opImportImage, note: msg.note, path: msg.path})
		return nil
	}
	return importPathCmd(m.opts.Vault, msg.note, msg.path, m.now())
}

// runImport performs a confirmed import.
func (m *Model) runImport(op pendingOp) tea.Cmd {
	if op.data != nil {
		return importDataCmd(m.opts.Vault, op.note, op.data, op.ext, m.now())
	}
	return importPathCmd(m.opts.Vault, op.note, op.path, m.now())
}

// importPathCmd copies the image file at path into attachments/.
func importPathCmd(v *vault.Vault, note, path string, now time.Time) tea.Cmd {
	return func() tea.Msg {
		link, err := attach.ImportPath(v, note, path, now)
		return imageImportedMsg{note: note, link: link, err: err}
	}
}

// importDataCmd writes clipboard image bytes into attachments/.
func importDataCmd(v *vault.Vault, note string, data []byte, ext string, now time.Time) tea.Cmd {
	return func() tea.Msg {
		link, err := attach.Import(v, note, data, ext, now)
		return imageImportedMsg{note: note, link: link, err: err}
	}
}

// handleImageImported inserts the new image's link on its own line in the
// note it was imported for.
func (m *Model) handleImageImported(msg imageImportedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, "Could not add the image: "+imageError(msg.err))
	}
	if msg.note != m.editor.Path() {
		return m.pushToast(msgs.ToastWarn, fmt.Sprintf("Image saved, but %s is no longer open: link it with %s", msg.note, msg.link))
	}
	m.noteChanged(msg.link)
	var cmd tea.Cmd
	m.editor, cmd = m.editor.InsertText(msg.link)
	return cmd
}

// imageError turns an import error into toast text.
func imageError(err error) string {
	if errors.Is(err, attach.ErrNotImage) {
		return "not a supported image (png, jpg, gif or webp)"
	}
	return err.Error()
}

// openImageViewer shows images full screen (spec §6.4). The viewer runs
// with the terminal released; ctrl+q inside it quits Notty.
func (m *Model) openImageViewer(msg msgs.OpenImageViewerMsg) tea.Cmd {
	if len(msg.Paths) == 0 {
		return nil
	}
	v := imageviewer.New(msg.Paths, msg.Index, m.opts.Caps)
	m.beginExec()
	return execCommand(v, func(err error) tea.Msg {
		return imageViewerDoneMsg{err: err, quit: v.QuitRequested}
	})
}

// handleImageViewerDone restores the preview's images after the viewer.
func (m *Model) handleImageViewerDone(msg imageViewerDoneMsg) tea.Cmd {
	m.endExec()
	ready := m.afterExec()
	if msg.quit {
		return m.quit()
	}
	if msg.err != nil {
		return tea.Batch(ready, m.pushToast(msgs.ToastError, fmt.Sprintf("The image viewer failed: %v", msg.err)))
	}
	return ready
}

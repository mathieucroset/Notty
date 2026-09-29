// Package editor is Notty's note editor component (spec §5 "ui/editor"). It
// wraps a *buffer.Buffer, a vim.Editor (the vim Machine or the non-modal
// Plain editor) and an incremental mdstyle cache, and renders them with
// soft-wrap, live markdown styling, a terminal cursor, autosave, and
// read-only and locked modes.
package editor

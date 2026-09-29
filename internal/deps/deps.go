//go:build tools

// Package deps pins module dependencies shared by parallel work streams.
// It is excluded from normal builds by the "tools" build tag.
package deps

import (
	_ "charm.land/bubbles/v2/key"
	_ "charm.land/bubbles/v2/textinput"
	_ "charm.land/bubbles/v2/spinner"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/glamour/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/BurntSushi/toml"
	_ "github.com/alecthomas/chroma/v2"
	_ "github.com/atotto/clipboard"
	_ "github.com/charmbracelet/x/ansi"
	_ "github.com/charmbracelet/x/ansi/kitty"
	_ "github.com/charmbracelet/x/ansi/sixel"
	_ "github.com/charmbracelet/x/ansi/iterm2"
	_ "github.com/charmbracelet/x/exp/teatest/v2"
	_ "github.com/charmbracelet/x/term"
	_ "github.com/fsnotify/fsnotify"
	_ "github.com/rivo/uniseg"
	_ "github.com/sahilm/fuzzy"
	_ "golang.org/x/image/webp"
	_ "golang.org/x/sync/errgroup"
)

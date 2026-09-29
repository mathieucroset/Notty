package theme

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	chroma "github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// hexString renders c as a "#rrggbb" string, suitable for
// ansi.StylePrimitive.Color / BackgroundColor.
func hexString(c color.Color) *string {
	r, g, b, _ := c.RGBA()
	s := fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)) //nolint:gosec
	return &s
}

func hexOf(c color.Color) string { return *hexString(c) }

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }

// chromaMu guards registration of chroma (syntax-highlighting) styles into
// the process-global chroma/v2/styles registry.
var chromaMu sync.Mutex

// ChromaStyleName returns the name of the chroma syntax-highlighting style
// for p, registering it in the global chroma style registry on first use.
// Both the glamour preview (via GlamourStyle) and the editor's live code
// highlighting call this so code-block colors always come from the same,
// theme-specific chroma style rather than glamour's fixed "charm" style
// name, which would otherwise freeze to whichever palette rendered first.
func ChromaStyleName(p Palette) string {
	name := "notty-" + p.Name
	registerChromaStyle(name, p)
	return name
}

// chromaEntry builds a chroma StyleEntries value (e.g. "#89b4fa bold") from
// palette-derived attributes. fg and bg may be nil to leave them unset.
func chromaEntry(fg, bg color.Color, bold, italic bool) string {
	var parts []string
	if fg != nil {
		parts = append(parts, hexOf(fg))
	}
	if bg != nil {
		parts = append(parts, "bg:"+hexOf(bg))
	}
	if italic {
		parts = append(parts, "italic")
	}
	if bold {
		parts = append(parts, "bold")
	}
	return strings.Join(parts, " ")
}

// registerChromaStyle registers name in the global chroma styles registry,
// built from p's tokens, unless a style with that name is already
// registered. Safe to call repeatedly and concurrently.
func registerChromaStyle(name string, p Palette) {
	chromaMu.Lock()
	defer chromaMu.Unlock()

	if _, ok := chromastyles.Registry[strings.ToLower(name)]; ok {
		return
	}

	chromastyles.Register(chroma.MustNewStyle(name, chroma.StyleEntries{
		chroma.Text:                chromaEntry(p.Text, nil, false, false),
		chroma.Error:               chromaEntry(p.Base, p.Error, false, false),
		chroma.Comment:             chromaEntry(p.Muted, nil, false, true),
		chroma.CommentPreproc:      chromaEntry(p.Accent2, nil, false, false),
		chroma.Keyword:             chromaEntry(p.Accent, nil, false, false),
		chroma.KeywordReserved:     chromaEntry(p.Accent, nil, false, false),
		chroma.KeywordNamespace:    chromaEntry(p.Accent2, nil, false, false),
		chroma.KeywordType:         chromaEntry(p.Headings[4], nil, false, false),
		chroma.Operator:            chromaEntry(p.Subtext, nil, false, false),
		chroma.Punctuation:         chromaEntry(p.Subtext, nil, false, false),
		chroma.Name:                chromaEntry(p.Text, nil, false, false),
		chroma.NameBuiltin:         chromaEntry(p.Accent2, nil, false, false),
		chroma.NameTag:             chromaEntry(p.Accent, nil, false, false),
		chroma.NameAttribute:       chromaEntry(p.Headings[2], nil, false, false),
		chroma.NameClass:           chromaEntry(p.Headings[1], nil, true, false),
		chroma.NameConstant:        chromaEntry(p.Headings[3], nil, false, false),
		chroma.NameDecorator:       chromaEntry(p.Warning, nil, false, false),
		chroma.NameFunction:        chromaEntry(p.Success, nil, false, false),
		chroma.LiteralNumber:       chromaEntry(p.Headings[3], nil, false, false),
		chroma.LiteralString:       chromaEntry(p.Success, nil, false, false),
		chroma.LiteralStringEscape: chromaEntry(p.Warning, nil, false, false),
		chroma.GenericDeleted:      chromaEntry(p.Error, nil, false, false),
		chroma.GenericEmph:         chromaEntry(nil, nil, false, true),
		chroma.GenericInserted:     chromaEntry(p.Success, nil, false, false),
		chroma.GenericStrong:       chromaEntry(nil, nil, true, false),
		chroma.GenericSubheading:   chromaEntry(p.Subtext, nil, false, false),
		chroma.Background:          chromaEntry(nil, p.Surface, false, false),
	}))
}

// GlamourStyle builds a glamour ansi.StyleConfig from p's tokens, so the
// markdown preview always matches the rest of the UI. It starts from
// glamour's built-in dark or light style (depending on p.Dark) and recolors
// everything from the palette.
func GlamourStyle(p Palette) ansi.StyleConfig {
	var s ansi.StyleConfig
	if p.Dark {
		s = styles.DarkStyleConfig
	} else {
		s = styles.LightStyleConfig
	}

	// The preview controls its own padding.
	s.Document.Margin = uintPtr(0)
	s.Document.Color = hexString(p.Text)

	s.BlockQuote.Color = hexString(p.Muted)
	s.BlockQuote.Italic = boolPtr(true)

	s.Paragraph.Color = hexString(p.Text)

	s.Heading.Color = hexString(p.Headings[0])
	s.Heading.Bold = boolPtr(true)

	headings := []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6}
	for i, h := range headings {
		h.Color = hexString(p.Headings[i])
		h.Bold = boolPtr(true)
		// The base dark style chips H1 with a hard-coded background; clear it
		// so every heading level is colored from the palette consistently.
		h.BackgroundColor = nil
	}

	s.Text.Color = hexString(p.Text)
	s.Strong.Bold = boolPtr(true)
	s.Emph.Italic = boolPtr(true)
	s.Strikethrough.CrossedOut = boolPtr(true)
	s.Strikethrough.Color = hexString(p.Muted)

	s.HorizontalRule.Color = hexString(p.Muted)

	s.Item.Color = hexString(p.Text)
	s.Enumeration.Color = hexString(p.Text)

	s.Task.Ticked = "☑ "
	s.Task.Unticked = "☐ "

	s.Link.Color = hexString(p.Accent)
	s.Link.Underline = boolPtr(true)
	s.LinkText.Color = hexString(p.Accent)
	s.LinkText.Bold = boolPtr(true)

	s.Image.Color = hexString(p.Accent2)
	s.Image.Underline = boolPtr(true)
	s.ImageText.Color = hexString(p.Subtext)

	s.Code.Color = hexString(p.Accent2)
	s.Code.BackgroundColor = hexString(p.Surface)

	s.CodeBlock.Color = hexString(p.Text)
	s.CodeBlock.BackgroundColor = hexString(p.Surface)
	s.CodeBlock.Margin = uintPtr(2)
	// Register (or reuse) a theme-specific chroma style and reference it by
	// name, rather than setting CodeBlock.Chroma: glamour's renderer always
	// registers an inline Chroma config under the single fixed style name
	// "charm", so the first palette rendered in a process would otherwise
	// win permanently.
	s.CodeBlock.Chroma = nil
	s.CodeBlock.Theme = ChromaStyleName(p)

	s.Table.Color = hexString(p.Text)
	s.Table.CenterSeparator = strPtr("┼")
	s.Table.ColumnSeparator = strPtr("│")
	s.Table.RowSeparator = strPtr("─")

	s.DefinitionTerm.Color = hexString(p.Text)
	s.DefinitionDescription.Color = hexString(p.Subtext)

	return s
}

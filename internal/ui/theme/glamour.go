package theme

import (
	"fmt"
	"image/color"

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

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }

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
	s.CodeBlock.Chroma = &ansi.Chroma{
		Text:                ansi.StylePrimitive{Color: hexString(p.Text)},
		Error:               ansi.StylePrimitive{Color: hexString(p.Base), BackgroundColor: hexString(p.Error)},
		Comment:             ansi.StylePrimitive{Color: hexString(p.Muted), Italic: boolPtr(true)},
		CommentPreproc:      ansi.StylePrimitive{Color: hexString(p.Accent2)},
		Keyword:             ansi.StylePrimitive{Color: hexString(p.Accent)},
		KeywordReserved:     ansi.StylePrimitive{Color: hexString(p.Accent)},
		KeywordNamespace:    ansi.StylePrimitive{Color: hexString(p.Accent2)},
		KeywordType:         ansi.StylePrimitive{Color: hexString(p.Headings[4])},
		Operator:            ansi.StylePrimitive{Color: hexString(p.Subtext)},
		Punctuation:         ansi.StylePrimitive{Color: hexString(p.Subtext)},
		Name:                ansi.StylePrimitive{Color: hexString(p.Text)},
		NameBuiltin:         ansi.StylePrimitive{Color: hexString(p.Accent2)},
		NameTag:             ansi.StylePrimitive{Color: hexString(p.Accent)},
		NameAttribute:       ansi.StylePrimitive{Color: hexString(p.Headings[2])},
		NameClass:           ansi.StylePrimitive{Color: hexString(p.Headings[1]), Bold: boolPtr(true)},
		NameConstant:        ansi.StylePrimitive{Color: hexString(p.Headings[3])},
		NameDecorator:       ansi.StylePrimitive{Color: hexString(p.Warning)},
		NameFunction:        ansi.StylePrimitive{Color: hexString(p.Success)},
		LiteralNumber:       ansi.StylePrimitive{Color: hexString(p.Headings[3])},
		LiteralString:       ansi.StylePrimitive{Color: hexString(p.Success)},
		LiteralStringEscape: ansi.StylePrimitive{Color: hexString(p.Warning)},
		GenericDeleted:      ansi.StylePrimitive{Color: hexString(p.Error)},
		GenericEmph:         ansi.StylePrimitive{Italic: boolPtr(true)},
		GenericInserted:     ansi.StylePrimitive{Color: hexString(p.Success)},
		GenericStrong:       ansi.StylePrimitive{Bold: boolPtr(true)},
		GenericSubheading:   ansi.StylePrimitive{Color: hexString(p.Subtext)},
		Background:          ansi.StylePrimitive{BackgroundColor: hexString(p.Surface)},
	}

	s.Table.Color = hexString(p.Text)
	s.Table.CenterSeparator = strPtr("┼")
	s.Table.ColumnSeparator = strPtr("│")
	s.Table.RowSeparator = strPtr("─")

	s.DefinitionTerm.Color = hexString(p.Text)
	s.DefinitionDescription.Color = hexString(p.Subtext)

	return s
}

package editor

import (
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/vim"
)

// keyNames maps Bubble Tea special key codes to vim.Key names.
var keyNames = map[rune]string{
	tea.KeyEscape:    "esc",
	tea.KeyEnter:     "enter",
	tea.KeyKpEnter:   "enter",
	tea.KeyBackspace: "backspace",
	tea.KeyDelete:    "delete",
	tea.KeyTab:       "tab",
	tea.KeyUp:        "up",
	tea.KeyDown:      "down",
	tea.KeyLeft:      "left",
	tea.KeyRight:     "right",
	tea.KeyHome:      "home",
	tea.KeyEnd:       "end",
	tea.KeySpace:     "space",
	tea.KeyPgUp:      "pgup",
	tea.KeyPgDown:    "pgdown",
}

// translateKey adapts a Bubble Tea key press to the vim engine's Key.
func translateKey(k tea.KeyPressMsg) vim.Key {
	vk := vim.Key{
		Ctrl:  k.Mod.Contains(tea.ModCtrl),
		Alt:   k.Mod.Contains(tea.ModAlt),
		Shift: k.Mod.Contains(tea.ModShift),
	}
	if name, ok := keyNames[k.Code]; ok {
		vk.Name = name
		if name == "space" && !vk.Ctrl && !vk.Alt {
			vk.Text = " "
		}
		return vk
	}
	vk.Code = k.Code
	if vk.Ctrl || vk.Alt {
		return vk
	}
	vk.Text = k.Text
	if vk.Text == "" && unicode.IsPrint(k.Code) {
		vk.Text = string(k.Code)
	}
	return vk
}

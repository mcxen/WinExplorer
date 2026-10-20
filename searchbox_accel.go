package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// searchbox_accel.go owns the 快速搜索 chord's text form: capture from a
// keydown, parse back into modifiers and a key, and the settings label.

// accelFromKey is the accelerator a captured keydown stores. A bare letter
// or digit is refused — it would steal typing — while function keys may
// stand alone. The text is what GlobalShortcut.Register parses.
func accelFromKey(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := accelKeyToken(key)
	if !ok || key == ui.KeyEscape {
		return "", false
	}
	if mods == 0 && (key < ui.KeyF1 || key > ui.KeyF12) {
		return "", false
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{
		{ui.Super, "Super"},
		{ui.Ctrl, "Ctrl"},
		{ui.Alt, "Alt"},
		{ui.Shift, "Shift"},
	} {
		if mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, name), "+"), true
}

// parseSearchAccel splits an accelerator we stored, or the default, into
// the modifiers and key the window shortcut matches. CmdOrCtrl is Command
// on macOS and Control elsewhere.
func parseSearchAccel(acc string) (ui.Modifiers, ui.Key, bool) {
	if strings.TrimSpace(acc) == "" {
		acc = defaultSearchHotkey
	}
	parts := strings.Split(acc, "+")
	if strings.HasSuffix(acc, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return 0, 0, false
	}
	key, ok := accelTokenKey(parts[len(parts)-1])
	if !ok {
		return 0, 0, false
	}
	var mods ui.Modifiers
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			mods |= ui.Ctrl
		case "alt", "option":
			mods |= ui.Alt
		case "shift":
			mods |= ui.Shift
		case "super", "cmd", "command", "meta", "win":
			mods |= ui.Super
		case "cmdorctrl", "commandorcontrol":
			mods |= ui.Cmd
		default:
			return 0, 0, false
		}
	}
	return mods, key, true
}

// shortcutLabel is the chord the settings row shows: macOS modifier
// symbols, and Ctrl+Shift+K elsewhere.
func shortcutLabel(acc string) string {
	mods, key, ok := parseSearchAccel(acc)
	if !ok {
		if strings.TrimSpace(acc) == "" {
			return "Ctrl+K"
		}
		return acc
	}
	keyText, _ := accelKeyToken(key)
	if len(keyText) == 1 {
		keyText = strings.ToUpper(keyText)
	}
	if runtime.GOOS == "darwin" {
		var b strings.Builder
		if mods&ui.Ctrl != 0 {
			b.WriteString("⌃")
		}
		if mods&ui.Alt != 0 {
			b.WriteString("⌥")
		}
		if mods&ui.Shift != 0 {
			b.WriteString("⇧")
		}
		if mods&ui.Super != 0 {
			b.WriteString("⌘")
		}
		b.WriteString(keyText)
		return b.String()
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Win")
	}
	return strings.Join(append(parts, keyText), "+")
}

func accelKeyToken(key ui.Key) (string, bool) {
	switch {
	case key >= ui.KeyA && key <= ui.KeyZ:
		return string(rune('A' + key - ui.KeyA)), true
	case key >= ui.Key0 && key <= ui.Key9:
		return string(rune('0' + key - ui.Key0)), true
	case key >= ui.KeyF1 && key <= ui.KeyF12:
		return fmt.Sprintf("F%d", int(key-ui.KeyF1)+1), true
	}
	switch key {
	case ui.KeySpace:
		return "Space", true
	case ui.KeyTab:
		return "Tab", true
	case ui.KeyEnter:
		return "Enter", true
	case ui.KeyBackspace:
		return "Backspace", true
	case ui.KeyDelete:
		return "Delete", true
	case ui.KeyInsert:
		return "Insert", true
	case ui.KeyHome:
		return "Home", true
	case ui.KeyEnd:
		return "End", true
	case ui.KeyPageUp:
		return "PageUp", true
	case ui.KeyPageDown:
		return "PageDown", true
	case ui.KeyUp:
		return "Up", true
	case ui.KeyDown:
		return "Down", true
	case ui.KeyLeft:
		return "Left", true
	case ui.KeyRight:
		return "Right", true
	case ui.KeyMinus:
		return "-", true
	case ui.KeyEqual:
		return "=", true
	case ui.KeyComma:
		return ",", true
	case ui.KeyPeriod:
		return ".", true
	case ui.KeySlash:
		return "/", true
	case ui.KeySemicolon:
		return ";", true
	case ui.KeyQuote:
		return "'", true
	case ui.KeyBracketLeft:
		return "[", true
	case ui.KeyBracketRight:
		return "]", true
	case ui.KeyBackslash:
		return "\\", true
	case ui.KeyBackquote:
		return "`", true
	default:
		return "", false
	}
}

func accelTokenKey(token string) (ui.Key, bool) {
	switch strings.ToLower(token) {
	case "space":
		return ui.KeySpace, true
	case "tab":
		return ui.KeyTab, true
	case "enter", "return":
		return ui.KeyEnter, true
	case "backspace":
		return ui.KeyBackspace, true
	case "delete":
		return ui.KeyDelete, true
	case "insert":
		return ui.KeyInsert, true
	case "home":
		return ui.KeyHome, true
	case "end":
		return ui.KeyEnd, true
	case "pageup":
		return ui.KeyPageUp, true
	case "pagedown":
		return ui.KeyPageDown, true
	case "up":
		return ui.KeyUp, true
	case "down":
		return ui.KeyDown, true
	case "left":
		return ui.KeyLeft, true
	case "right":
		return ui.KeyRight, true
	case "-":
		return ui.KeyMinus, true
	case "=":
		return ui.KeyEqual, true
	case ",", "comma":
		return ui.KeyComma, true
	case ".", "period":
		return ui.KeyPeriod, true
	case "/", "slash":
		return ui.KeySlash, true
	case ";", "semicolon":
		return ui.KeySemicolon, true
	case "'", "quote":
		return ui.KeyQuote, true
	case "[":
		return ui.KeyBracketLeft, true
	case "]":
		return ui.KeyBracketRight, true
	case "\\", "backslash":
		return ui.KeyBackslash, true
	case "`":
		return ui.KeyBackquote, true
	}
	if len(token) == 1 {
		r := []rune(strings.ToUpper(token))[0]
		switch {
		case r >= 'A' && r <= 'Z':
			return ui.KeyA + ui.Key(r-'A'), true
		case r >= '0' && r <= '9':
			return ui.Key0 + ui.Key(r-'0'), true
		}
	}
	if len(token) >= 2 && (token[0] == 'F' || token[0] == 'f') {
		n := 0
		for _, r := range token[1:] {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n >= 1 && n <= 12 {
			return ui.KeyF1 + ui.Key(n-1), true
		}
	}
	return 0, false
}

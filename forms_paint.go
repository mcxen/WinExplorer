package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/internal/forms"
)

// forms_paint.go draws declarative form fields for Settings and OpenUI.
// Controls stay on mygo / Onevra: TextBox, Select, Switch, Segmented, and
// a path row with a native folder/file picker.

type formFieldOpts struct {
	Locked   bool
	PathMode string // folder | file
	Options  []string
	Password bool
	Hint     string
}

// paintFormField draws one standardized control. value is the live string
// the surrounding form owns (OpenUI vals map or settingsSchedule fields).
func (a *App) paintFormField(c *ui.Context, kind, label string, value *string, opts formFieldOpts) {
	if value == nil {
		return
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
		if label != "" && kind != forms.KindSwitch {
			ui.Text(c, label).FontSize(12).TextColor(colorText())
		}
		switch kind {
		case forms.KindSelect:
			a.paintFormSelect(c, value, opts)
		case forms.KindSegmented, forms.KindDays:
			a.paintFormChips(c, value, opts)
		case forms.KindSwitch:
			a.paintFormSwitch(c, label, value, opts)
		case forms.KindMultiSelect:
			a.paintFormMulti(c, value, opts)
		case forms.KindPath:
			a.paintFormPath(c, value, opts)
		case forms.KindTextArea:
			box := components.TextBox(c, value, components.TextBoxOptions{Multiline: true, ReadOnly: opts.Locked}).
				FillWidth().MinHeight(64).FontSize(12).Lines(1, 5).Label(label)
			if opts.Hint != "" {
				box.Placeholder(opts.Hint)
			}
		default:
			box := components.TextBox(c, value, components.TextBoxOptions{ReadOnly: opts.Locked}).
				FillWidth().Height(28).FontSize(12).Label(label)
			if opts.Hint != "" {
				box.Placeholder(opts.Hint)
			}
			if opts.Password || kind == forms.KindPassword {
				box.Password()
			}
		}
	})
}

func (a *App) paintFormSelect(c *ui.Context, value *string, opts formFieldOpts) {
	if len(opts.Options) == 0 {
		return
	}
	if *value == "" {
		*value = opts.Options[0]
	}
	sel := ui.Select(c, value, opts.Options).FillWidth()
	if opts.Locked {
		sel.Disabled(true)
	}
}

// paintFormChips is DayChips / Segmented: exclusive options as ui.Segmented.
func (a *App) paintFormChips(c *ui.Context, value *string, opts formFieldOpts) {
	if len(opts.Options) == 0 {
		return
	}
	idx := 0
	for i, o := range opts.Options {
		if o == *value {
			idx = i
			break
		}
	}
	if *value == "" {
		*value = opts.Options[0]
	}
	seg := segmentedLabeled(c, &idx, opts.Options)
	if opts.Locked {
		seg.Disabled(true)
	}
	if seg.Changed() && idx >= 0 && idx < len(opts.Options) {
		*value = opts.Options[idx]
	}
}

func segmentedLabeled(c *ui.Context, idx *int, labels []string) ui.Element {
	switch len(labels) {
	case 0:
		return ui.Box(c)
	case 1:
		return ui.Segmented(c, idx, labels[0])
	case 2:
		return ui.Segmented(c, idx, labels[0], labels[1])
	case 3:
		return ui.Segmented(c, idx, labels[0], labels[1], labels[2])
	case 4:
		return ui.Segmented(c, idx, labels[0], labels[1], labels[2], labels[3])
	case 5:
		return ui.Segmented(c, idx, labels[0], labels[1], labels[2], labels[3], labels[4])
	case 6:
		return ui.Segmented(c, idx, labels[0], labels[1], labels[2], labels[3], labels[4], labels[5])
	default:
		return ui.Segmented(c, idx, labels[0], labels[1], labels[2], labels[3], labels[4], labels[5], labels[6])
	}
}

func (a *App) paintFormSwitch(c *ui.Context, label string, value *string, opts formFieldOpts) {
	on := parseBoolString(*value)
	sw := ui.Switch(c, &on).Shrink(0)
	if label != "" {
		sw.Label(label)
	}
	if opts.Locked {
		sw.Disabled(true)
	}
	if sw.Changed() {
		*value = boolString(on)
	}
}

func (a *App) paintFormMulti(c *ui.Context, value *string, opts formFieldOpts) {
	selected := map[string]bool{}
	for _, part := range strings.Split(*value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			selected[part] = true
		}
	}
	ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
		for i, opt := range opts.Options {
			on := selected[opt]
			sw := ui.Switch(c.Key(i), &on).Shrink(0).Label(opt)
			if opts.Locked {
				sw.Disabled(true)
			}
			if sw.Changed() {
				selected[opt] = on
				var parts []string
				for _, o := range opts.Options {
					if selected[o] {
						parts = append(parts, o)
					}
				}
				*value = strings.Join(parts, ",")
			}
		}
	})
}

func (a *App) paintFormPath(c *ui.Context, value *string, opts formFieldOpts) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		box := components.TextBox(c, value, components.TextBoxOptions{ReadOnly: opts.Locked}).
			Grow(1).MinWidth(0).Height(28).FontSize(12)
		if opts.Hint != "" {
			box.Placeholder(opts.Hint)
		}
		if !opts.Locked {
			if components.IconButton(c, lucide.FolderOpen(), "浏览", components.IconButtonPlain).Clicked() {
				a.formPickPath(value, opts.PathMode)
			}
		}
	})
}

func (a *App) formPickPath(value *string, mode string) {
	if a.assist.picking {
		return
	}
	a.assist.picking = true
	win := a.win
	dir := ""
	if value != nil {
		dir = strings.TrimSpace(*value)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	folder := mode != forms.PathFile
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       pathPickerTitle(folder),
			DefaultPath: dir,
			ButtonLabel: "选择",
			Directory:   folder,
		})
		a.postAssist(func() {
			a.assist.picking = false
			if err != nil {
				a.statusMsg = err.Error()
				return
			}
			if len(paths) > 0 && value != nil {
				*value = paths[0]
			}
		})
	}()
}

func pathPickerTitle(folder bool) string {
	if folder {
		return "选择文件夹"
	}
	return "选择文件"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func parseBoolString(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes" || s == "是"
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// paintFormFence draws a ```form JSON block by converting it to OpenUI so
// Settings and the assistant share one painter path. Invalid JSON shows a
// short note instead of a blank card.
func (a *App) paintFormFence(c *ui.Context, raw string, turns []int) {
	f, err := forms.ParseJSON(raw)
	if err != nil {
		ui.Text(c, "表单无法解析："+err.Error()).FontSize(12).TextColor(colorMuted()).Wrap()
		return
	}
	src := f.ToOpenUI()
	a.paintOpenBlock(c.Key("form-"+openBlockKey(src)), src, a.openTurnFor(turns, src))
}

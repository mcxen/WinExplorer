package main

import (
	"strconv"
	"strings"
	"time"

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
	Min      float64
	Max      float64
	Step     float64
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
		case forms.KindFileTypes:
			a.paintFormFileTypes(c, value, opts)
		case forms.KindTableSelect:
			a.paintFormTableSelect(c, value, opts)
		case forms.KindTags:
			a.paintFormTags(c, value, opts)
		case forms.KindDate:
			a.paintFormDate(c, value, opts)
		case forms.KindSlider:
			a.paintFormSlider(c, value, opts)
		case forms.KindProgress:
			a.paintFormProgress(c, value, opts)
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
	start := ""
	if value != nil {
		start = strings.TrimSpace(*value)
	}
	folder := mode != forms.PathFile
	// Dialog.Open errors with "no such file or directory" when DefaultPath
	// points at a missing ~/Downloads (common on CI / headless boxes).
	dir := forms.ExistingDir(start)
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

func (a *App) paintFormFileTypes(c *ui.Context, value *string, opts formFieldOpts) {
	if len(opts.Options) == 0 {
		return
	}
	selected := splitCSVSet(*value)
	row := ui.Row(c).FillWidth().Gap(4).Wrap()
	if opts.Locked {
		row.Disabled(true)
	}
	row.Children(func() {
		for i, opt := range opts.Options {
			on := selected[opt]
			tb := components.ToggleButton(c.Key(i), &on, opt)
			if tb.Changed() && !opts.Locked {
				selected[opt] = on
				*value = joinCSVSelected(opts.Options, selected)
			}
		}
	})
}

func (a *App) paintFormTableSelect(c *ui.Context, value *string, opts formFieldOpts) {
	if len(opts.Options) == 0 {
		return
	}
	selected := splitCSVSet(*value)
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for i, opt := range opts.Options {
			on := selected[opt]
			ui.Row(c.Key(i)).FillWidth().Gap(8).AlignItems(ui.Center).Padding(4, 6).Children(func() {
				sw := ui.Switch(c, &on).Shrink(0)
				if opts.Locked {
					sw.Disabled(true)
				}
				ui.Text(c, opt).FontSize(12).TextColor(colorText()).Grow(1).MinWidth(0)
				if sw.Changed() && !opts.Locked {
					selected[opt] = on
					*value = joinCSVSelected(opts.Options, selected)
				}
			})
		}
	})
}

func (a *App) paintFormTags(c *ui.Context, value *string, opts formFieldOpts) {
	tags := splitCSVList(*value)
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		box := components.TextBox(c, value, components.TextBoxOptions{ReadOnly: opts.Locked}).
			FillWidth().Height(28).FontSize(12)
		if opts.Hint != "" {
			box.Placeholder(opts.Hint)
		}
		if len(tags) == 0 {
			return
		}
		ui.Row(c).FillWidth().Gap(4).Wrap().Children(func() {
			for i, tag := range tags {
				chip := ui.Row(c.Key(i)).Gap(2).AlignItems(ui.Center).Padding(2, 6)
				chip.Children(func() {
					ui.Text(c, tag).FontSize(11).TextColor(colorText())
					if !opts.Locked {
						if components.IconButton(c, lucide.X(), "移除", components.IconButtonPlain).Clicked() {
							var keep []string
							for _, t := range tags {
								if t != tag {
									keep = append(keep, t)
								}
							}
							*value = strings.Join(keep, ",")
						}
					}
				})
			}
		})
	})
}

func (a *App) paintFormDate(c *ui.Context, value *string, opts formFieldOpts) {
	t := parseDateValue(*value)
	if opts.Locked {
		ui.Text(c, formatDateValue(t)).FontSize(12).TextColor(colorMuted())
		return
	}
	el := ui.DateInput(c, &t).FillWidth()
	if el.Changed() || strings.TrimSpace(*value) == "" {
		*value = formatDateValue(t)
	}
}

func (a *App) paintFormSlider(c *ui.Context, value *string, opts formFieldOpts) {
	lo, hi := opts.Min, opts.Max
	if hi == 0 && lo == 0 {
		hi = 100
	}
	if hi <= lo {
		hi = lo + 1
	}
	v := parseFloatString(*value, lo)
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	if opts.Locked {
		row.Disabled(true)
	}
	row.Children(func() {
		ui.Slider(c, &v, lo, hi).Grow(1).MinWidth(80)
		ui.Text(c, trimFloat(v)).FontSize(12).TextColor(colorText()).Width(40)
		if !opts.Locked {
			*value = trimFloat(v)
		}
	})
}

func (a *App) paintFormProgress(c *ui.Context, value *string, opts formFieldOpts) {
	frac := parseProgressFraction(*value)
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		components.Progress(c, frac).FillWidth().Height(6)
		ui.Text(c, strconv.Itoa(int(frac*100+0.5))+"%").FontSize(11).TextColor(colorMuted())
	})
}

func splitCSVSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func splitCSVList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func joinCSVSelected(opts []string, selected map[string]bool) string {
	var parts []string
	for _, o := range opts {
		if selected[o] {
			parts = append(parts, o)
		}
	}
	return strings.Join(parts, ",")
}

func parseDateValue(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006/01/02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Now()
}

func formatDateValue(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.Format("2006-01-02")
}

func parseFloatString(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

func trimFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func parseProgressFraction(s string) float64 {
	v := parseFloatString(s, 0)
	if v > 1 {
		v = v / 100
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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

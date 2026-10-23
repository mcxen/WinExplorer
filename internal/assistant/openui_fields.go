package assistant

import (
	"fmt"
	"strings"
)

// openui_fields.go realizes the extended OpenUI form controls (TimeField,
// PathField, DayChips, Number, SwitchField, Segmented, MultiSelect, DateField,
// FileTypeChips, TagsField, SliderField, ProgressBar, TableSelect) so the
// AI and Settings share one vocabulary.

// openFieldName reads a control's name: the spec's first positional
// argument, or an object's name/title/label key when the model wrote the
// control as an object. The parser names a plain value's expr with its
// key rather than wrapping it, so both shapes read here.
func openFieldName(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) string {
	if name, _ := openText(env, openArg(e.args, 0), stack); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	for _, arg := range e.args {
		if arg.kind != "object" {
			continue
		}
		for _, f := range arg.items {
			val := f
			if f.kind == "field" && len(f.items) > 0 {
				val = f.items[0]
			}
			switch strings.ToLower(f.name) {
			case "name", "title", "label", "field":
				if name, _ := openText(env, val, stack); strings.TrimSpace(name) != "" {
					return strings.TrimSpace(name)
				}
			}
		}
	}
	return ""
}

// openFieldOpts reads a control's options: the spec's second positional
// argument, or an object's options/items/values key.
func openFieldOpts(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []string {
	if opts := openStringOpts(env, openArg(e.args, 1), stack); len(opts) > 0 {
		return opts
	}
	for _, arg := range e.args {
		if arg.kind != "object" {
			continue
		}
		for _, f := range arg.items {
			val := f
			if f.kind == "field" && len(f.items) > 0 {
				val = f.items[0]
			}
			switch strings.ToLower(f.name) {
			case "options", "items", "values":
				if opts := openStringOpts(env, val, stack); len(opts) > 0 {
					return opts
				}
			}
		}
	}
	return nil
}

func openInputField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	ph, _ := openText(env, openArg(e.args, 1), stack)
	typeHint, _ := openText(env, openArg(e.args, 2), stack)
	v := &OUIView{Kind: "input", Name: name, Placeholder: ph, Multiline: e.name == "TextArea"}
	switch strings.ToLower(strings.TrimSpace(typeHint)) {
	case "password", "passwd", "secret":
		v.Password = true
	case "number", "int", "float":
		v.Kind = "number"
	case "time", "hhmm":
		v.Kind = "time"
	case "path", "folder", "dir":
		v.Kind = "path"
		v.PathMode = "folder"
	case "file":
		v.Kind = "path"
		v.PathMode = "file"
	}
	openMarkRequired(v, e.args, 2)
	return v
}

func openNamedField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, kind string) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	ph, _ := openText(env, openArg(e.args, 1), stack)
	v := &OUIView{Kind: kind, Name: name, Placeholder: ph}
	openMarkRequired(v, e.args, 2)
	return v
}

func openPathField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	v := openNamedField(env, e, stack, "path")
	if v == nil {
		return nil
	}
	mode, _ := openText(env, openArg(e.args, 2), stack)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "file":
		v.PathMode = "file"
	case "required", "必填":
		v.PathMode = "folder"
		v.Required = true
	default:
		v.PathMode = "folder"
	}
	openMarkRequired(v, e.args, 2)
	return v
}

func openDayChips(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	opts := openFieldOpts(env, e, stack)
	if len(opts) == 0 {
		opts = []string{"每天", "工作日", "周末"}
	}
	v := &OUIView{Kind: "days", Name: name, Options: opts}
	openMarkRequired(v, e.args, 1)
	return v
}

func openSwitchField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	label, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: "switch", Name: name, Label: strings.TrimSpace(label)}
}

func openSegmentedField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	opts := openFieldOpts(env, e, stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	return &OUIView{Kind: "segmented", Name: name, Options: opts}
}

func openMultiSelectField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	opts := openFieldOpts(env, e, stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	ph, _ := openText(env, openArg(e.args, 2), stack)
	v := &OUIView{Kind: "multi_select", Name: name, Options: opts, Placeholder: ph}
	openMarkRequired(v, e.args, 2)
	return v
}

func openSelectField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	opts := openFieldOpts(env, e, stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	ph, _ := openText(env, openArg(e.args, 2), stack)
	v := &OUIView{Kind: "select", Name: name, Options: opts, Placeholder: ph}
	openMarkRequired(v, e.args, 2)
	return v
}

func openStringOpts(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []string {
	var opts []string
	for _, item := range openList(env, e, stack) {
		if text, ok := openOption(env, item, stack); ok && text != "" {
			opts = append(opts, text)
		}
		if len(opts) >= openUIMaxCells {
			break
		}
	}
	return opts
}

func openMarkRequired(v *OUIView, args []OUIExpr, from int) {
	if v == nil {
		return
	}
	for _, arg := range args[min(from, len(args)):] {
		if (arg.kind == "object" || arg.kind == "string") && openRequired(arg) {
			v.Required = true
		}
	}
}

func openOptsField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, kind string) *OUIView {
	name := openFieldName(env, e, stack)
	opts := openFieldOpts(env, e, stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	v := &OUIView{Kind: kind, Name: name, Options: opts}
	openMarkRequired(v, e.args, 1)
	return v
}

func openSliderField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	lo := openFloatArg(env, openArg(e.args, 1), stack, 0)
	hi := openFloatArg(env, openArg(e.args, 2), stack, 100)
	if hi <= lo {
		hi = lo + 1
	}
	def, _ := openText(env, openArg(e.args, 3), stack)
	v := &OUIView{Kind: "slider", Name: name, Min: lo, Max: hi, Placeholder: strings.TrimSpace(def)}
	openMarkRequired(v, e.args, 2)
	return v
}

func openProgressBar(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	val, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: "progress", Name: name, Placeholder: strings.TrimSpace(val)}
}

func openFloatArg(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, def float64) float64 {
	if e.kind == "number" {
		return e.n
	}
	text, _ := openText(env, e, stack)
	text = strings.TrimSpace(text)
	if text == "" {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(text, "%f", &f); err == nil {
		return f
	}
	return def
}

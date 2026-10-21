package assistant

import "strings"

// openui_fields.go realizes the extended OpenUI form controls (TimeField,
// PathField, DayChips, Number, SwitchField, Segmented, MultiSelect) so the
// AI and Settings share one vocabulary.

func openInputField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
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
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
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
		if mode == "" || strings.EqualFold(mode, "folder") || strings.EqualFold(mode, "dir") {
			v.PathMode = "folder"
		} else {
			v.PathMode = "folder"
		}
	}
	openMarkRequired(v, e.args, 2)
	return v
}

func openDayChips(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	opts := openStringOpts(env, openArg(e.args, 1), stack)
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
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
	opts := openStringOpts(env, openArg(e.args, 1), stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	return &OUIView{Kind: "segmented", Name: name, Options: opts}
}

func openMultiSelectField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
	opts := openStringOpts(env, openArg(e.args, 1), stack)
	if name == "" || len(opts) == 0 {
		return nil
	}
	ph, _ := openText(env, openArg(e.args, 2), stack)
	v := &OUIView{Kind: "multi_select", Name: name, Options: opts, Placeholder: ph}
	openMarkRequired(v, e.args, 2)
	return v
}

func openSelectField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name, _ := openText(env, openArg(e.args, 0), stack)
	name = strings.TrimSpace(name)
	opts := openStringOpts(env, openArg(e.args, 1), stack)
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

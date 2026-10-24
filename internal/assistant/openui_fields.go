package assistant

import (
	"fmt"
	"strings"
)

// openui_fields.go realizes the extended OpenUI form controls (TimeField,
// PathField, DayChips, Number, SwitchField, Segmented, MultiSelect, DateField,
// FileTypeChips, TagsField, SliderField, ProgressBar, TableSelect,
// ListField, ImageField, VideoField) so the
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

func openListField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	opts := openFieldOpts(env, e, stack)
	if name == "" {
		return nil
	}
	mode, _ := openText(env, openArg(e.args, 2), stack)
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "single", "radio", "one":
		mode = "single"
	case "required", "必填":
		mode = "multi"
	default:
		if mode != "multi" {
			mode = "multi"
		}
	}
	hint, _ := openText(env, openArg(e.args, 3), stack)
	hint = strings.TrimSpace(hint)
	if hint == "required" || hint == "必填" {
		hint = ""
	}
	v := &OUIView{Kind: "list", Name: name, Options: opts, PathMode: mode, Placeholder: hint}
	openMarkRequired(v, e.args, 2)
	return v
}

func openMediaField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, kind string) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	src, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: kind, Name: name, Placeholder: strings.TrimSpace(src)}
}

// OPENUI4 — presentation components (Note, Badge, KeyValue, RatingField,
// Chart, DonutChart, Accordion) below. They realize here beside the fields so
// a program mixes them freely; the painter owns every color.

// openTone reads a tone argument, accepting the spellings a model writes for
// the same intent (warn/warning, error/danger). Empty stays neutral.
func openTone(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "ok", "good":
		return "success"
	case "warning", "warn":
		return "warning"
	case "danger", "error", "destructive":
		return "danger"
	case "info", "note", "tip":
		return "info"
	default:
		return ""
	}
}

// openIconExpr reads an icon name and keeps the ones the pane can draw, so a
// name out of the supported set paints the component without a glyph rather
// than losing it.
func openIconExpr(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) string {
	name, _ := openText(env, e, stack)
	name = strings.ToLower(strings.TrimSpace(name))
	if openSupportedIcons[name] {
		return name
	}
	return ""
}

var openSupportedIcons = map[string]bool{
	"info": true, "lightbulb": true, "sparkles": true, "circlecheck": true,
	"circlealert": true, "trianglealert": true, "circlex": true, "ban": true,
	"check": true, "clock": true, "star": true, "tag": true, "tags": true,
	"folder": true, "file": true, "filetext": true, "image": true, "video": true,
	"music": true, "archive": true, "harddrive": true, "gauge": true,
	"activity": true, "zap": true, "shieldcheck": true, "shieldalert": true,
	"chartpie": true, "chartcolumn": true, "chartline": true, "layers": true,
	"dot": true, "pin": true, "quote": true,
}

// openNote is Note(text, tone?, icon?): a callout strip.
func openNote(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	text, _ := openText(env, openArg(e.args, 0), stack)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	v := &OUIView{Kind: "note", Text: text}
	tone, _ := openText(env, openArg(e.args, 1), stack)
	v.Tone = openTone(tone)
	v.Icon = openIconExpr(env, openArg(e.args, 2), stack)
	return v
}

// openBadge is Badge(text, tone?): a compact status pill.
func openBadge(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	text, _ := openText(env, openArg(e.args, 0), stack)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	tone, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: "badge", Text: OpenShort(text, 24), Tone: openTone(tone)}
}

// openKeyValue is KeyValue(label, value, tone?): one summary row.
func openKeyValue(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	label, _ := openText(env, openArg(e.args, 0), stack)
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	val, _ := openText(env, openArg(e.args, 1), stack)
	tone, _ := openText(env, openArg(e.args, 2), stack)
	return &OUIView{Kind: "keyvalue", Label: OpenShort(label, 40), Text: val, Tone: openTone(tone)}
}

// openRatingField is RatingField(name, label?, max?, "required"?). Values
// 1–5, written into the form's message as a number string.
func openRatingField(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	name := openFieldName(env, e, stack)
	if name == "" {
		return nil
	}
	label, _ := openText(env, openArg(e.args, 1), stack)
	max := openFloatArg(env, openArg(e.args, 2), stack, 5)
	if max < 1 {
		max = 5
	}
	if max > 10 {
		max = 10
	}
	v := &OUIView{Kind: "rating", Name: name, Label: strings.TrimSpace(label), Min: 1, Max: max}
	openMarkRequired(v, e.args, 3)
	return v
}

// openSeriesList reads Chart's labels and series from either positional
// shape — Chart(labels, series) — or the single-object shape the JSON-minded
// model writes, {labels: […], series: [Series(…)]}. Series items are
// Series(label, values) or {label: …, values: […]}. Values are capped so a
// runaway program cannot stretch the reply by its data alone.
func openSeriesList(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) (labels []string, series []OUISeries) {
	for _, arg := range e.args {
		if arg.kind == "object" {
			for _, f := range arg.items {
				val := f
				if f.kind == "field" && len(f.items) > 0 {
					val = f.items[0]
				}
				switch strings.ToLower(f.name) {
				case "labels", "categories":
					for _, item := range openList(env, val, stack) {
						if text, ok := openText(env, item, stack); ok && strings.TrimSpace(text) != "" {
							labels = append(labels, OpenShort(strings.TrimSpace(text), 16))
						}
						if len(labels) >= openUIMaxCells {
							break
						}
					}
				case "series", "values", "items":
					series = append(series, openSeriesItems(env, val, stack)...)
				}
			}
		}
	}
	if len(labels) == 0 {
		for _, item := range openList(env, openArg(e.args, 0), stack) {
			if text, ok := openText(env, item, stack); ok && strings.TrimSpace(text) != "" {
				labels = append(labels, OpenShort(strings.TrimSpace(text), 16))
			}
			if len(labels) >= openUIMaxCells {
				break
			}
		}
	}
	if len(series) == 0 {
		series = openSeriesItems(env, openArg(e.args, 1), stack)
	}
	return labels, series
}

func openSeriesItems(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []OUISeries {
	var out []OUISeries
	for _, item := range openList(env, e, stack) {
		if s, ok := openSeriesItem(env, item, stack); ok {
			out = append(out, s)
			if len(out) >= 4 {
				break
			}
		}
	}
	return out
}

// openSeriesItem reads one series: Series(label, values), or an object
// {label: …, values: […]}. The parser names a plain value's expr with its
// key rather than wrapping it, so both shapes read here. A bare numbers array
// is a nameless series.
func openSeriesItem(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) (OUISeries, bool) {
	if e.kind == "ref" {
		if stack[e.name] || len(stack) > 16 {
			return OUISeries{}, false
		}
		next, ok := env[e.name]
		if !ok {
			return OUISeries{}, false
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return openSeriesItem(env, next, stack)
	}
	if e.kind == "call" && (e.name == "Series" || e.name == "Line" || e.name == "Bar") {
		label, _ := openText(env, openArg(e.args, 0), stack)
		var values []float64
		for _, item := range openList(env, openArg(e.args, 1), stack) {
			if n := openSeriesValue(env, item, stack); n != nil {
				values = append(values, *n)
			}
			if len(values) >= openUIMaxCells {
				break
			}
		}
		if len(values) == 0 {
			return OUISeries{}, false
		}
		return OUISeries{Label: strings.TrimSpace(label), Values: values}, true
	}
	if e.kind == "object" {
		var s OUISeries
		var hasValues bool
		for _, f := range e.items {
			val := f
			if f.kind == "field" && len(f.items) > 0 {
				val = f.items[0]
			}
			switch strings.ToLower(f.name) {
			case "label", "name", "title":
				s.Label, _ = openText(env, val, stack)
			case "values", "data", "points":
				for _, item := range openList(env, val, stack) {
					if n := openSeriesValue(env, item, stack); n != nil {
						s.Values = append(s.Values, *n)
					}
					if len(s.Values) >= openUIMaxCells {
						break
					}
				}
				hasValues = len(s.Values) > 0
			}
		}
		if hasValues {
			return s, true
		}
		return OUISeries{}, false
	}
	if e.kind == "array" {
		var values []float64
		for _, item := range e.items {
			if n := openSeriesValue(env, item, stack); n != nil {
				values = append(values, *n)
			}
			if len(values) >= openUIMaxCells {
				break
			}
		}
		if len(values) == 0 {
			return OUISeries{}, false
		}
		return OUISeries{Values: values}, true
	}
	return OUISeries{}, false
}

func openSeriesValue(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *float64 {
	if e.kind == "number" {
		return &e.n
	}
	text, _ := openText(env, e, stack)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var f float64
	if _, err := fmt.Sscanf(text, "%f", &f); err != nil {
		return nil
	}
	return &f
}

// openChart is Chart(labels, series): a bar chart of up to four series.
func openChart(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	labels, series := openSeriesList(env, e, stack)
	if len(labels) == 0 || len(series) == 0 {
		return nil
	}
	return &OUIView{Kind: "chart", Options: labels, Series: series}
}

// openDonutChart is DonutChart(slices): slice values come from each series'
// first value. Donut values only need their proportions, so percent-scale
// and absolute counts both pass through unchanged.
func openDonutChart(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	var slices []OUISeries
	total := 0.0
	for _, arg := range e.args {
		for _, item := range openList(env, arg, stack) {
			if s, ok := openSeriesItem(env, item, stack); ok {
				slices = append(slices, s)
				total += openSeriesFirst(s)
				if len(slices) >= 8 {
					break
				}
			}
		}
	}
	if len(slices) == 0 || total <= 0 {
		return nil
	}
	return &OUIView{Kind: "donut", Series: slices}
}

func openSeriesFirst(s OUISeries) float64 {
	if len(s.Values) == 0 {
		return 0
	}
	if s.Values[0] < 0 {
		return 0
	}
	return s.Values[0]
}

// openAccordion is Accordion(items): a stack of disclosures.
func openAccordion(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	var items []*OUIView
	for _, arg := range e.args {
		for _, item := range openList(env, arg, stack) {
			if v := realizeOpen(env, item, stack); v != nil && v.Kind == "accordionitem" {
				items = append(items, v)
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &OUIView{Kind: "accordion", Items: items}
}

// openAccordionItem is AccordionItem(title, children?, open?): children may
// sit in the second position; the boolean picks the initial disclosure.
func openAccordionItem(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	title, _ := openText(env, openArg(e.args, 0), stack)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	kids := openChildren(env, openArg(e.args, 1), stack)
	open := false
	if arg := openArg(e.args, 2); arg.kind == "bool" {
		open = arg.yes
	}
	return &OUIView{Kind: "accordionitem", Text: OpenShort(title, 60), Children: kids, Open: open}
}

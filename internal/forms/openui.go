package forms

import (
	"fmt"
	"strconv"
	"strings"
)

// ToOpenUI emits an OpenUI Lang program the pane already paints. The AI can
// also write these components by hand; present_form uses this converter.
func (f Form) ToOpenUI() string {
	f.Normalize()
	var b strings.Builder
	b.WriteString("root = Card([head, form])\n")
	title := f.Title
	if title == "" {
		title = "填写"
	}
	b.WriteString(fmt.Sprintf("head = CardHeader(%q, %q)\n", title, f.Description))
	var fieldRefs []string
	for i, field := range f.Fields {
		ref := fmt.Sprintf("f%d", i)
		fieldRefs = append(fieldRefs, ref)
		ctl := fmt.Sprintf("c%d", i)
		b.WriteString(fieldOpenUI(ctl, field))
		b.WriteString(fmt.Sprintf("%s = FormControl(%q, %s)\n", ref, field.Label, ctl))
	}
	var actRefs []string
	for i, act := range f.Actions {
		ref := fmt.Sprintf("a%d", i)
		actRefs = append(actRefs, ref)
		variant := act.Variant
		if variant == "" {
			variant = "secondary"
		}
		b.WriteString(fmt.Sprintf("%s = Button(%q, @ToAssistant(%q), %q)\n", ref, act.Label, act.Message, variant))
	}
	if len(actRefs) == 0 {
		b.WriteString(`ok = Button("提交", @ToAssistant("提交表单"), "primary")` + "\n")
		actRefs = []string{"ok"}
	}
	b.WriteString(fmt.Sprintf("actions = Buttons([%s], \"row\")\n", strings.Join(actRefs, ", ")))
	name := f.ID
	if name == "" {
		name = "form"
	}
	b.WriteString(fmt.Sprintf("form = Form(%q, actions, [%s])\n", name, strings.Join(fieldRefs, ", ")))
	return b.String()
}

func fieldOpenUI(ref string, field Field) string {
	req := ""
	if field.Required {
		req = `, "required"`
	}
	ph := field.Placeholder
	switch field.Kind {
	case KindTextArea:
		return fmt.Sprintf("%s = TextArea(%q, %q%s)\n", ref, field.ID, ph, req)
	case KindPassword:
		return fmt.Sprintf("%s = Input(%q, %q, \"password\"%s)\n", ref, field.ID, ph, req)
	case KindNumber:
		return fmt.Sprintf("%s = Number(%q, %q%s)\n", ref, field.ID, ph, req)
	case KindPath:
		mode := field.PathMode
		if mode == "" {
			mode = PathFolder
		}
		return fmt.Sprintf("%s = PathField(%q, %q, %q%s)\n", ref, field.ID, ph, mode, req)
	case KindSelect:
		return fmt.Sprintf("%s = Select(%q, [%s], %q%s)\n", ref, field.ID, quoteList(field.Options), ph, req)
	case KindMultiSelect:
		return fmt.Sprintf("%s = MultiSelect(%q, [%s], %q%s)\n", ref, field.ID, quoteList(field.Options), ph, req)
	case KindSwitch:
		return fmt.Sprintf("%s = SwitchField(%q, %q)\n", ref, field.ID, field.Label)
	case KindTime:
		return fmt.Sprintf("%s = TimeField(%q, %q%s)\n", ref, field.ID, ph, req)
	case KindDays:
		opts := field.Options
		if len(opts) == 0 {
			opts = []string{"每天", "工作日", "周末"}
		}
		return fmt.Sprintf("%s = DayChips(%q, [%s]%s)\n", ref, field.ID, quoteList(opts), req)
	case KindSegmented:
		return fmt.Sprintf("%s = Segmented(%q, [%s])\n", ref, field.ID, quoteList(field.Options))
	case KindDate:
		return fmt.Sprintf("%s = DateField(%q, %q%s)\n", ref, field.ID, ph, req)
	case KindFileTypes:
		return fmt.Sprintf("%s = FileTypeChips(%q, [%s]%s)\n", ref, field.ID, quoteList(field.Options), req)
	case KindTags:
		return fmt.Sprintf("%s = TagsField(%q, %q%s)\n", ref, field.ID, ph, req)
	case KindSlider:
		lo, hi := field.Min, field.Max
		if hi == 0 && lo == 0 {
			hi = 100
		}
		if field.Value != "" {
			return fmt.Sprintf("%s = SliderField(%q, %g, %g, %q%s)\n", ref, field.ID, lo, hi, field.Value, req)
		}
		return fmt.Sprintf("%s = SliderField(%q, %g, %g%s)\n", ref, field.ID, lo, hi, req)
	case KindProgress:
		return fmt.Sprintf("%s = ProgressBar(%q, %q)\n", ref, field.ID, field.Value)
	case KindTableSelect:
		return fmt.Sprintf("%s = TableSelect(%q, [%s]%s)\n", ref, field.ID, quoteList(field.Options), req)
	case KindList:
		mode := field.PathMode
		if mode == "" {
			mode = ListMulti
		}
		if ph != "" {
			return fmt.Sprintf("%s = ListField(%q, [%s], %q, %q%s)\n", ref, field.ID, quoteList(field.Options), mode, ph, req)
		}
		return fmt.Sprintf("%s = ListField(%q, [%s], %q%s)\n", ref, field.ID, quoteList(field.Options), mode, req)
	case KindImage:
		src := field.Value
		if src == "" {
			src = field.Placeholder
		}
		return fmt.Sprintf("%s = ImageField(%q, %q)\n", ref, field.ID, src)
	case KindVideo:
		src := field.Value
		if src == "" {
			src = field.Placeholder
		}
		return fmt.Sprintf("%s = VideoField(%q, %q)\n", ref, field.ID, src)
	case KindNote:
		tone := field.Tone
		if tone == "" {
			tone = "info"
		}
		src := field.Value
		if src == "" {
			src = field.Placeholder
		}
		return fmt.Sprintf("%s = Note(%q, %q)\n", ref, src, tone)
	case KindBadge:
		src := field.Value
		if src == "" {
			src = field.Placeholder
		}
		if field.Tone != "" {
			return fmt.Sprintf("%s = Badge(%q, %q)\n", ref, src, field.Tone)
		}
		return fmt.Sprintf("%s = Badge(%q)\n", ref, src)
	case KindKeyValue:
		val := field.Value
		if val == "" {
			val = field.Placeholder
		}
		if field.Tone != "" {
			return fmt.Sprintf("%s = KeyValue(%q, %q, %q)\n", ref, field.Label, val, field.Tone)
		}
		return fmt.Sprintf("%s = KeyValue(%q, %q)\n", ref, field.Label, val)
	case KindRating:
		max := field.Max
		if max == 0 {
			max = 5
		}
		return fmt.Sprintf("%s = RatingField(%q, %q, %g%s)\n", ref, field.ID, field.Label, max, req)
	case KindChart:
		labels, series := chartFieldData(field)
		return fmt.Sprintf("%s = Chart([%s], [%s])\n", ref, quoteList(labels), seriesOpenUI(series))
	case KindDonut:
		_, series := chartFieldData(field)
		parts := make([]string, 0, len(series))
		for _, s := range series {
			v := 0.0
			if len(s.Values) > 0 {
				v = s.Values[0]
			}
			parts = append(parts, fmt.Sprintf("Series(%q, [%g])", s.Label, v))
		}
		return fmt.Sprintf("%s = DonutChart([%s])\n", ref, strings.Join(parts, ", "))
	case KindAccord:
		items := make([]string, 0, len(field.Options))
		for _, opt := range field.Options {
			items = append(items, fmt.Sprintf("AccordionItem(%q)", opt))
		}
		return fmt.Sprintf("%s = Accordion([%s])\n", ref, strings.Join(items, ", "))
	default:
		return fmt.Sprintf("%s = Input(%q, %q, \"text\"%s)\n", ref, field.ID, ph, req)
	}
}

// chartFieldData reads a chart field's data: labels from Options, series
// values from the "标签: v1, v2" lines of SeriesLabels, or one nameless
// series from a plain comma list of numbers.
func chartFieldData(field Field) ([]string, []ChartSeries) {
	var labels []string
	var series []ChartSeries
	for _, opt := range field.Options {
		if label, vals, ok := splitSeriesLine(opt); ok {
			series = append(series, ChartSeries{Label: label, Values: vals})
			continue
		}
		labels = append(labels, opt)
	}
	if len(series) == 0 && len(labels) > 0 {
		var vals []float64
		for _, l := range labels {
			if v, err := strconv.ParseFloat(strings.TrimSpace(l), 64); err == nil {
				vals = append(vals, v)
			}
		}
		if len(vals) == len(labels) {
			return nil, []ChartSeries{{Values: vals}}
		}
	}
	return labels, series
}

// splitSeriesLine reads "标签: 1, 2, 3" (ASCII or full-width colon) into one
// series; a trailing percent on the first number is dropped (DonutChart takes
// proportions as they come).
func splitSeriesLine(s string) (string, []float64, bool) {
	label, rest, found := strings.Cut(s, ":")
	if !found {
		label, rest, found = strings.Cut(s, "：")
	}
	if !found {
		return "", nil, false
	}
	var vals []float64
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSuffix(strings.TrimSpace(part), "%")
		if part == "" {
			continue
		}
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return "", nil, false
		}
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return "", nil, false
	}
	return strings.TrimSpace(label), vals, true
}

// seriesOpenUI emits one series as Series(label, [values]).
func seriesOpenUI(series []ChartSeries) string {
	parts := make([]string, 0, len(series))
	for _, s := range series {
		vals := make([]string, 0, len(s.Values))
		for _, v := range s.Values {
			vals = append(vals, strconv.FormatFloat(v, 'g', -1, 64))
		}
		parts = append(parts, fmt.Sprintf("Series(%q, [%s])", s.Label, strings.Join(vals, ", ")))
	}
	return strings.Join(parts, ", ")
}

func quoteList(opts []string) string {
	parts := make([]string, 0, len(opts))
	for _, o := range opts {
		parts = append(parts, fmt.Sprintf("%q", o))
	}
	return strings.Join(parts, ", ")
}

// FenceOpenUI wraps a program in the openui markdown fence.
func FenceOpenUI(program string) string {
	return "```openui\n" + strings.TrimSpace(program) + "\n```\n"
}

// FenceForm wraps JSON for a ```form code block (alternate AI emit path).
func FenceForm(jsonBody string) string {
	return "```form\n" + strings.TrimSpace(jsonBody) + "\n```\n"
}

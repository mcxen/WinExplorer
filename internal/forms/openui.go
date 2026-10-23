package forms

import (
	"fmt"
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
	default:
		return fmt.Sprintf("%s = Input(%q, %q, \"text\"%s)\n", ref, field.ID, ph, req)
	}
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

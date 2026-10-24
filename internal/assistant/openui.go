package assistant

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// assistant_openui.go draws a fenced OpenUI Lang subset with the file
// manager's own controls. OpenUI's runtimes are web frameworks; embedding
// one would leave the immediate-mode pane. Buttons only send the next
// message. The model still has to call a tool before any file changes.

const (
	openUIFence      = "```openui"
	OpenUIMarkerOpen = "]]>openui:content"
	OpenUIMarkerEnd  = "]]>openui:end"
	openUIMaxStmts   = 120
	openUIMaxCols    = 8
	openUIMaxCells   = 60
)

// openPiece is one fenced program found in a transcript line.
type openPiece struct {
	turn int
	src  string
}

// OUIExpr is one OpenUI expression. Calls and actions keep positional args.
type OUIExpr struct {
	kind  string
	s     string
	n     float64
	yes   bool
	items []OUIExpr
	name  string
	args  []OUIExpr
}

// OUIView is the part of a program this pane knows how to draw.
type OUIView struct {
	Kind     string
	Text     string
	Sub      string
	Size     string
	Variant  string
	Row      bool
	Gap      float32
	Label    string
	Action   string
	Children []*OUIView
	Items    []*OUIView
	Cols     []OUICol
	Path     string
	Extra    int
	// Name, Placeholder, Options, Required and Multiline are a form
	// field's: Input, TextArea and Select inside a Form.
	Name        string
	Placeholder string
	Options     []string
	Required    bool
	Multiline   bool
	// PathMode is folder|file for path fields. Password marks a secret input.
	PathMode string
	Password bool
	// Min/Max are slider bounds (ProgressBar ignores them).
	Min float64
	Max float64

	// Tone tints the painted component (Note, Badge): info, success, warning
	// or danger. Tone-less paints the neutral surface.
	Tone string
	// Icon is a named lucide glyph (Badge, Note). Empty means none.
	Icon string
	// Series is one Chart line or DonutChart slice group: label plus values.
	Series []OUISeries
	// Open is an Accordion item's initial disclosure state.
	Open bool
}

// OUISeries is one named series of a chart: the label shown in the legend,
// the values aligned by index with the chart's labels, and DonutChart's
// single value (the first). Colors come from the painter's palette, so a
// program never carries them.
type OUISeries struct {
	Label  string
	Values []float64
}

// OUICol is one Table column. Cells line up by index with the other columns.
type OUICol struct {
	Label string
	Cells []string
}

type ouiParser struct {
	s     string
	i     int
	depth int
}

// SplitOpenUI pulls complete OpenUI programs out of a reply. Prose stays in
// order; an opener without a closer is dropped so a stopped stream does not
// leave the fence on screen. The fence is kept in the stored turn so the
// next request still shows the model what it drew.
func SplitOpenUI(text string) (string, []string) {
	var blocks []string
	for {
		fence := fenceStart(text)
		marker := strings.Index(text, OpenUIMarkerOpen)
		switch {
		case fence < 0 && marker < 0:
			return strings.TrimSpace(text), blocks
		case fence >= 0 && (marker < 0 || fence <= marker):
			block, next, ok := cutOpenFence(text)
			if !ok {
				return strings.TrimSpace(text[:fence]), blocks
			}
			if block = strings.TrimSpace(block); block != "" {
				blocks = append(blocks, block)
			}
			text = next
		default:
			block, next, ok := CutBlock(text, OpenUIMarkerOpen, OpenUIMarkerEnd)
			if !ok {
				return strings.TrimSpace(text[:marker]), blocks
			}
			if block = strings.TrimSpace(block); block != "" {
				blocks = append(blocks, block)
			}
			text = next
		}
	}
}

func fenceStart(text string) int {
	return strings.Index(strings.ToLower(text), openUIFence)
}

func cutOpenFence(text string) (block, rest string, ok bool) {
	i := fenceStart(text)
	if i < 0 {
		return "", "", false
	}
	from := i + len(openUIFence)
	j := strings.Index(text[from:], "```")
	if j < 0 {
		return "", "", false
	}
	block = text[from : from+j]
	rest = strings.TrimSpace(text[:i] + text[from+j+3:])
	return block, rest, true
}

// unclosedFence is the byte index of the first ```openui that has no closer.
// A closed block earlier in the reply does not hide the text around it.
func UnclosedFence(text string) int {
	lower := strings.ToLower(text)
	from := 0
	for from < len(text) {
		rel := strings.Index(lower[from:], openUIFence)
		if rel < 0 {
			return -1
		}
		i := from + rel
		after := i + len(openUIFence)
		if after > len(text) {
			return i
		}
		j := strings.Index(text[after:], "```")
		if j < 0 {
			return i
		}
		from = after + j + 3
	}
	return -1
}

// ParseOpenUI reads one fenced program. A model that strays from the
// vocabulary — @action without its parens, a Table column as an object, a
// statement that stops half-written — is read as far as it can be rather
// than dropped whole: a card missing one button beats no card. Whatever
// cannot be read at all returns nil, which the painter draws as prose.
// The parse is panic-guarded: a pathological program must not take the
// pane's frame down with it.
func ParseOpenUI(src string) (view *OUIView) {
	defer func() {
		if recover() != nil {
			view = nil
		}
	}()
	src = strings.TrimPrefix(strings.TrimSpace(src), "\uFEFF")
	env := map[string]OUIExpr{}
	var order []string
	for _, line := range strings.Split(src, "\n") {
		if len(order) >= openUIMaxStmts {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		name, expr, ok := splitOpenStmt(line)
		if !ok {
			continue
		}
		p := &ouiParser{s: expr}
		e, ok := p.parseExpr()
		p.skip()
		if !ok || (p.i < len(p.s) && !strings.HasPrefix(p.s[p.i:], "//")) {
			continue
		}
		env[name] = e
		order = append(order, name)
	}
	if _, ok := env["root"]; ok {
		return realizeOpen(env, OUIExpr{kind: "ref", name: "root"}, map[string]bool{})
	}
	for i := len(order) - 1; i >= 0; i-- {
		e := env[order[i]]
		if e.kind == "call" && (e.name == "Stack" || e.name == "Card") {
			return realizeOpen(env, OUIExpr{kind: "ref", name: order[i]}, map[string]bool{})
		}
	}
	return nil
}

func splitOpenStmt(line string) (name, expr string, ok bool) {
	i := strings.Index(line, "=")
	if i <= 0 {
		return "", "", false
	}
	name = strings.TrimSpace(line[:i])
	expr = strings.TrimSpace(line[i+1:])
	if name == "" || expr == "" || !openIdent(name) {
		return "", "", false
	}
	return name, expr, true
}

func openIdent(s string) bool {
	r, w := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || (r != '_' && !unicode.IsLetter(r)) {
		return false
	}
	for _, r := range s[w:] {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func (p *ouiParser) parseExpr() (OUIExpr, bool) {
	if p.depth > 32 {
		return OUIExpr{}, false
	}
	p.depth++
	defer func() { p.depth-- }()
	left, ok := p.parseTerm()
	if !ok {
		return OUIExpr{}, false
	}
	for {
		p.skip()
		if p.peek() != '+' {
			break
		}
		p.i++
		right, ok := p.parseTerm()
		if !ok {
			return OUIExpr{}, false
		}
		left = OUIExpr{kind: "concat", items: []OUIExpr{left, right}}
	}
	return left, true
}

func (p *ouiParser) parseTerm() (OUIExpr, bool) {
	p.skip()
	switch p.peek() {
	case '"':
		s, ok := p.parseString()
		if !ok {
			return OUIExpr{}, false
		}
		return OUIExpr{kind: "string", s: s}, true
	case '[':
		p.i++
		items, ok := p.parseList(']')
		if !ok {
			return OUIExpr{}, false
		}
		return OUIExpr{kind: "array", items: items}, true
	case '{':
		fields, ok := p.parseObject()
		if !ok {
			return OUIExpr{}, false
		}
		return OUIExpr{kind: "object", items: fields}, true
	case '@':
		p.i++
		name, ok := p.parseIdent()
		if !ok {
			return OUIExpr{}, false
		}
		p.skip()
		if p.peek() != '(' {
			// An action the model wrote without its parens — @confirm — is
			// read as a no-argument one rather than losing the button.
			return OUIExpr{kind: "action", name: name}, true
		}
		p.i++
		args, ok := p.parseList(')')
		if !ok {
			// A truncated action keeps its name: the button sends its own
			// label, which is what openButtonArgs falls back to.
			return OUIExpr{kind: "action", name: name}, true
		}
		return OUIExpr{kind: "action", name: name, args: args}, true
	}
	if n, ok := p.parseNumber(); ok {
		return OUIExpr{kind: "number", n: n}, true
	}
	name, ok := p.parseIdent()
	if !ok {
		return OUIExpr{}, false
	}
	p.skip()
	if p.peek() == '(' {
		p.i++
		args, ok := p.parseList(')')
		if !ok {
			return OUIExpr{}, false
		}
		return OUIExpr{kind: "call", name: name, args: args}, true
	}
	switch name {
	case "true":
		return OUIExpr{kind: "bool", yes: true}, true
	case "false":
		return OUIExpr{kind: "bool"}, true
	case "null":
		return OUIExpr{kind: "null"}, true
	default:
		return OUIExpr{kind: "ref", name: name}, true
	}
}

func (p *ouiParser) parseList(end byte) ([]OUIExpr, bool) {
	var items []OUIExpr
	for {
		p.skip()
		if p.i >= len(p.s) {
			return nil, false
		}
		if p.s[p.i] == end {
			p.i++
			return items, true
		}
		item, ok := p.parseExpr()
		if !ok {
			return nil, false
		}
		items = append(items, item)
		p.skip()
		if p.i >= len(p.s) {
			return nil, false
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case end:
			p.i++
			return items, true
		default:
			return nil, false
		}
	}
}

// parseObject reads {key: value, …}. Each value comes back with its key
// as its name, for the rules of a form field ({required: true}).
func (p *ouiParser) parseObject() ([]OUIExpr, bool) {
	if p.peek() != '{' {
		return nil, false
	}
	p.i++
	var out []OUIExpr
	for {
		p.skip()
		if p.peek() == '}' {
			p.i++
			return out, true
		}
		var key string
		var ok bool
		if p.peek() == '"' {
			key, ok = p.parseString()
		} else {
			key, ok = p.parseIdent()
		}
		if !ok {
			return nil, false
		}
		p.skip()
		if p.peek() != ':' {
			return nil, false
		}
		p.i++
		v, ok := p.parseExpr()
		if !ok {
			return nil, false
		}
		if v.kind != "ref" && v.kind != "call" {
			v.name = key
		} else {
			v = OUIExpr{kind: "field", name: key, items: []OUIExpr{v}}
		}
		out = append(out, v)
		p.skip()
		switch p.peek() {
		case ',':
			p.i++
		case '}':
			p.i++
			return out, true
		default:
			return nil, false
		}
	}
}

func (p *ouiParser) parseString() (string, bool) {
	if p.peek() != '"' {
		return "", false
	}
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			p.i++
			return b.String(), true
		}
		if c == '\\' {
			p.i++
			if p.i >= len(p.s) {
				return "", false
			}
			switch p.s[p.i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(p.s[p.i])
			}
			p.i++
			continue
		}
		b.WriteByte(c)
		p.i++
	}
	return "", false
}

func (p *ouiParser) parseNumber() (float64, bool) {
	start := p.i
	if p.peek() == '-' {
		p.i++
	}
	if p.i >= len(p.s) || p.s[p.i] < '0' || p.s[p.i] > '9' {
		p.i = start
		return 0, false
	}
	for p.i < len(p.s) && ((p.s[p.i] >= '0' && p.s[p.i] <= '9') || p.s[p.i] == '.') {
		p.i++
	}
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		p.i = start
		return 0, false
	}
	return n, true
}

func (p *ouiParser) parseIdent() (string, bool) {
	if p.i >= len(p.s) {
		return "", false
	}
	r, w := utf8.DecodeRuneInString(p.s[p.i:])
	if r == utf8.RuneError || (r != '_' && !unicode.IsLetter(r)) {
		return "", false
	}
	start := p.i
	p.i += w
	for p.i < len(p.s) {
		r, w = utf8.DecodeRuneInString(p.s[p.i:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		p.i += w
	}
	return p.s[start:p.i], true
}

func (p *ouiParser) skip() {
	for p.i < len(p.s) {
		r, w := utf8.DecodeRuneInString(p.s[p.i:])
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return
		}
		p.i += w
	}
}

func (p *ouiParser) peek() byte {
	if p.i >= len(p.s) {
		return 0
	}
	return p.s[p.i]
}

func realizeOpen(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	if e.kind == "ref" {
		if stack[e.name] || len(stack) > 16 {
			return nil
		}
		next, ok := env[e.name]
		if !ok {
			return nil
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return realizeOpen(env, next, stack)
	}
	if e.kind != "call" {
		if text, ok := openText(env, e, stack); ok && text != "" {
			return &OUIView{Kind: "text", Text: text, Size: "medium"}
		}
		return nil
	}
	switch e.name {
	case "Stack":
		kids := openChildren(env, openArg(e.args, 0), stack)
		if len(kids) == 0 {
			return nil
		}
		row, gap := openStackLayout(e.args)
		return &OUIView{Kind: "stack", Row: row, Gap: gap, Children: kids}
	case "Card":
		kids := openChildren(env, openArg(e.args, 0), stack)
		if len(kids) == 0 {
			return nil
		}
		return &OUIView{Kind: "card", Gap: 6, Children: kids}
	case "CardHeader":
		title, _ := openText(env, openArg(e.args, 0), stack)
		sub, _ := openText(env, openArg(e.args, 1), stack)
		if title == "" && sub == "" {
			return nil
		}
		return &OUIView{Kind: "header", Text: title, Sub: sub}
	case "TextContent":
		text, _ := openText(env, openArg(e.args, 0), stack)
		if text == "" {
			return nil
		}
		size, _ := openText(env, openArg(e.args, 1), stack)
		if size == "" {
			size = "medium"
		}
		return &OUIView{Kind: "text", Text: text, Size: size}
	case "Table":
		cols := openCols(env, openArg(e.args, 0), stack)
		if len(cols) == 0 {
			return nil
		}
		return &OUIView{Kind: "table", Cols: cols}
	case "Tree":
		items := openTreeItems(env, openArg(e.args, 0), stack)
		if len(items) == 0 {
			return nil
		}
		tree := &OUIView{Kind: "tree", Items: items}
		trimOpenTree(tree, 6, 200)
		return tree
	case "TreeItem":
		label, _ := openText(env, openArg(e.args, 0), stack)
		label = strings.TrimSpace(label)
		if label == "" {
			return nil
		}
		pathArg := openArg(e.args, 1)
		path, pathOK := openText(env, pathArg, stack)
		childrenArg := openArg(e.args, 2)
		// With path optional, TreeItem(label, children) is the compact form;
		// an explicit null keeps children in the third position.
		if !pathOK && childrenArg.kind == "" && pathArg.kind != "null" {
			childrenArg = pathArg
		}
		return &OUIView{
			Kind:  "treeitem",
			Text:  label,
			Path:  strings.TrimSpace(path),
			Items: openTreeItems(env, childrenArg, stack),
		}
	case "Buttons":
		kids := openButtons(env, openArg(e.args, 0), stack)
		if len(kids) == 0 {
			return nil
		}
		dir, _ := openText(env, openArg(e.args, 1), stack)
		return &OUIView{Kind: "stack", Row: openIsRow(dir), Gap: 6, Children: kids}
	case "Button":
		label, _ := openText(env, openArg(e.args, 0), stack)
		label = strings.TrimSpace(label)
		if label == "" {
			return nil
		}
		action, variant := openButtonArgs(env, e.args, stack)
		if action == "" {
			action = label
		}
		return &OUIView{Kind: "button", Label: label, Action: action, Variant: variant}
	case "Form":
		// Form(name, buttons, fields) as OpenUI writes it; the two lists
		// are told apart by what they hold, so either order draws.
		var fields, buttons []*OUIView
		for _, arg := range e.args[min(1, len(e.args)):] {
			for _, v := range openChildren(env, arg, stack) {
				if v.Kind == "button" || (v.Kind == "stack" && openAllButtons(v)) {
					buttons = append(buttons, v)
				} else {
					fields = append(fields, v)
				}
			}
		}
		if len(fields) == 0 && len(buttons) == 0 {
			return nil
		}
		name, _ := openText(env, openArg(e.args, 0), stack)
		return &OUIView{Kind: "form", Name: name, Children: append(fields, buttons...)}
	case "FormControl":
		label, _ := openText(env, openArg(e.args, 0), stack)
		field := realizeOpen(env, openArg(e.args, 1), stack)
		if field == nil {
			return nil
		}
		if OpenIsField(field.Kind) {
			field.Label = strings.TrimSpace(label)
			return field
		}
		// A presentation component keeps its own shape; the control's label
		// only names it above, the way a field's does.
		return &OUIView{Kind: "stack", Children: []*OUIView{
			{Kind: "text", Text: strings.TrimSpace(label), Size: "small"},
			field,
		}}
	case "Input", "TextArea":
		return openInputField(env, e, stack)
	case "Number":
		return openNamedField(env, e, stack, "number")
	case "TimeField":
		return openNamedField(env, e, stack, "time")
	case "PathField":
		return openPathField(env, e, stack)
	case "DayChips":
		return openDayChips(env, e, stack)
	case "SwitchField", "Toggle":
		return openSwitchField(env, e, stack)
	case "Segmented":
		return openSegmentedField(env, e, stack)
	case "MultiSelect":
		return openMultiSelectField(env, e, stack)
	case "Select":
		return openSelectField(env, e, stack)
	case "DateField":
		return openNamedField(env, e, stack, "date")
	case "FileTypeChips":
		return openOptsField(env, e, stack, "filetypes")
	case "TagsField":
		return openNamedField(env, e, stack, "tags")
	case "SliderField":
		return openSliderField(env, e, stack)
	case "ProgressBar":
		return openProgressBar(env, e, stack)
	case "TableSelect":
		return openOptsField(env, e, stack, "table_select")
	case "ListField", "FileList":
		return openListField(env, e, stack)
	case "ImageField", "Image":
		return openMediaField(env, e, stack, "image")
	case "VideoField", "Video":
		return openMediaField(env, e, stack, "video")
	case "Note", "Callout":
		return openNote(env, e, stack)
	case "Badge", "Tag":
		return openBadge(env, e, stack)
	case "KeyValue", "KV", "Stat":
		return openKeyValue(env, e, stack)
	case "RatingField", "Rating":
		return openRatingField(env, e, stack)
	case "Chart":
		return openChart(env, e, stack)
	case "DonutChart", "Donut":
		return openDonutChart(env, e, stack)
	case "Accordion":
		return openAccordion(env, e, stack)
	case "AccordionItem":
		return openAccordionItem(env, e, stack)
	case "Divider":
		return &OUIView{Kind: "divider"}
	case "SectionHeader":
		// The model sometimes reaches for a second header shape under a
		// different name; drawing it as a header keeps the reply in the
		// card's rhythm instead of failing the statement.
		title, _ := openText(env, openArg(e.args, 0), stack)
		sub, _ := openText(env, openArg(e.args, 1), stack)
		if strings.TrimSpace(title) == "" {
			return nil
		}
		return &OUIView{Kind: "header", Text: strings.TrimSpace(title), Sub: strings.TrimSpace(sub)}
	case "MediaList", "MediaCards":
		return openMediaList(env, e, stack)
	default:
		return nil
	}
}

// openTreeItems realizes TreeItem children, ignoring unrelated entries so a
// malformed tree still shows the nodes that are valid.
func openTreeItems(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []*OUIView {
	var out []*OUIView
	for _, item := range openList(env, e, stack) {
		if v := realizeOpen(env, item, stack); v != nil && v.Kind == "treeitem" {
			out = append(out, v)
		}
	}
	return out
}

// trimOpenTree bounds what a reply can ask the pane to draw. The first 200
// nodes and six levels stay visible; the remainder is reported on the last
// kept branch instead of silently disappearing.
func trimOpenTree(tree *OUIView, maxDepth, maxNodes int) {
	if tree == nil {
		return
	}
	kept := 0
	var walk func(parent *OUIView, depth int)
	walk = func(parent *OUIView, depth int) {
		old := parent.Items
		items := make([]*OUIView, 0, len(old))
		for _, item := range old {
			size := openTreeSize(item)
			if depth > maxDepth || kept >= maxNodes {
				parent.Extra += size
				continue
			}
			kept++
			items = append(items, item)
			walk(item, depth+1)
		}
		parent.Items = items
	}
	walk(tree, 1)
}

func openTreeSize(v *OUIView) int {
	if v == nil {
		return 0
	}
	n := 1
	for _, ch := range v.Items {
		n += openTreeSize(ch)
	}
	return n
}

func OpenIsField(kind string) bool {
	switch kind {
	case "input", "select", "number", "path", "time", "date", "days", "switch", "segmented",
		"multi_select", "filetypes", "tags", "slider", "progress", "table_select",
		"list", "image", "video", "rating":
		return true
	default:
		return false
	}
}

func openAllButtons(v *OUIView) bool {
	for _, ch := range v.Children {
		if ch.Kind != "button" {
			return false
		}
	}
	return len(v.Children) > 0
}

// openRequired reads a field's rule: {required: true}, or the word
// "required" among its trailing arguments (the system prompt cannot show
// braces, which its formatter takes for placeholders).
func openRequired(obj OUIExpr) bool {
	if obj.kind == "string" {
		v := strings.ToLower(strings.TrimSpace(obj.s))
		return v == "required" || v == "必填"
	}
	for _, f := range obj.items {
		if strings.EqualFold(f.name, "required") && f.kind == "bool" && f.yes {
			return true
		}
	}
	return false
}

// openOption is one entry of a Select: a string, or SelectItem(value, label)
// read by its label.
func openOption(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) (string, bool) {
	if e.kind == "call" && e.name == "SelectItem" {
		if label, ok := openText(env, openArg(e.args, 1), stack); ok && label != "" {
			return label, true
		}
		return openText(env, openArg(e.args, 0), stack)
	}
	return openText(env, e, stack)
}

func openArg(args []OUIExpr, i int) OUIExpr {
	if i < 0 || i >= len(args) {
		return OUIExpr{}
	}
	return args[i]
}

func openChildren(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []*OUIView {
	var out []*OUIView
	for _, item := range openList(env, e, stack) {
		if v := realizeOpen(env, item, stack); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func openButtons(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []*OUIView {
	var out []*OUIView
	for _, item := range openList(env, e, stack) {
		v := realizeOpen(env, item, stack)
		if v != nil && v.Kind == "button" {
			out = append(out, v)
		}
	}
	return out
}

func openList(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []OUIExpr {
	if e.kind == "ref" {
		if stack[e.name] || len(stack) > 16 {
			return nil
		}
		next, ok := env[e.name]
		if !ok {
			return nil
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return openList(env, next, stack)
	}
	if e.kind == "array" {
		return e.items
	}
	if e.kind == "" {
		return nil
	}
	return []OUIExpr{e}
}

func openCols(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) []OUICol {
	var cols []OUICol
	for _, item := range openList(env, e, stack) {
		col, ok := openCol(env, item, stack)
		if !ok {
			continue
		}
		cols = append(cols, col)
		if len(cols) >= openUIMaxCols {
			break
		}
	}
	return cols
}

func openCol(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) (OUICol, bool) {
	if e.kind == "ref" {
		if stack[e.name] || len(stack) > 16 {
			return OUICol{}, false
		}
		next, ok := env[e.name]
		if !ok {
			return OUICol{}, false
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return openCol(env, next, stack)
	}
	// The spec writes Col("名称", ["甲"]); a model that learned JSON
	// writes {label: "名称", cells: ["甲"]}. Both make a column.
	if e.kind == "object" {
		return openColFromObject(env, e.items, stack)
	}
	if e.kind != "call" || e.name != "Col" || len(e.args) < 2 {
		return OUICol{}, false
	}
	label, _ := openText(env, e.args[0], stack)
	label = strings.TrimSpace(label)
	if label == "" {
		label = "列"
	}
	var cells []string
	for _, item := range openList(env, e.args[1], stack) {
		text, ok := openText(env, item, stack)
		if !ok {
			continue
		}
		cells = append(cells, text)
		if len(cells) >= openUIMaxCells {
			break
		}
	}
	return OUICol{Label: label, Cells: cells}, true
}

// openColFromObject reads {label: …, cells: …} into a column. Keys the
// model spells another way — title/name/header, values/items — still
// land where the column wants them. The parser names a plain value's expr
// with its key rather than wrapping it, so both shapes read here.
func openColFromObject(env map[string]OUIExpr, fields []OUIExpr, stack map[string]bool) (OUICol, bool) {
	var label string
	var cells []string
	for _, f := range fields {
		val := f
		if f.kind == "field" && len(f.items) > 0 {
			val = f.items[0]
		}
		switch strings.ToLower(f.name) {
		case "label", "title", "name", "header":
			label, _ = openText(env, val, stack)
		case "cells", "values", "items":
			for _, item := range openList(env, val, stack) {
				if text, ok := openText(env, item, stack); ok {
					cells = append(cells, text)
				}
			}
		}
	}
	if label == "" && len(cells) == 0 {
		return OUICol{}, false
	}
	if len(cells) > openUIMaxCells {
		cells = cells[:openUIMaxCells]
	}
	if label == "" {
		label = "列"
	}
	return OUICol{Label: label, Cells: cells}, true
}

func openText(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) (string, bool) {
	switch e.kind {
	case "":
		return "", false
	case "string":
		return e.s, true
	case "number":
		if e.n == float64(int64(e.n)) {
			return strconv.FormatInt(int64(e.n), 10), true
		}
		return strconv.FormatFloat(e.n, 'f', -1, 64), true
	case "bool":
		return strconv.FormatBool(e.yes), true
	case "concat":
		var b strings.Builder
		for _, item := range e.items {
			s, ok := openText(env, item, stack)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return b.String(), true
	case "ref":
		if stack[e.name] || len(stack) > 16 {
			return "", false
		}
		next, ok := env[e.name]
		if !ok {
			return "", false
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return openText(env, next, stack)
	case "call":
		if e.name == "TextContent" && len(e.args) > 0 {
			return openText(env, e.args[0], stack)
		}
		return "", false
	default:
		return "", false
	}
}

func openStackLayout(args []OUIExpr) (row bool, gap float32) {
	gap = 8
	if len(args) > 1 && args[1].kind == "string" {
		row = openIsRow(args[1].s)
	} else if len(args) > 1 && args[1].kind == "number" {
		gap = openGap(args[1].n)
	}
	if len(args) > 2 && args[2].kind == "number" {
		gap = openGap(args[2].n)
	}
	return row, gap
}

func openGap(n float64) float32 {
	if n < 0 {
		n = 0
	}
	if n > 32 {
		n = 32
	}
	return float32(n)
}

func openIsRow(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "row", "horizontal", "h":
		return true
	default:
		return false
	}
}

func openButtonArgs(env map[string]OUIExpr, args []OUIExpr, stack map[string]bool) (action, variant string) {
	if len(args) < 2 {
		return "", ""
	}
	second := args[1]
	if second.kind == "string" && openIsVariant(second.s) && len(args) == 2 {
		return "", second.s
	}
	action = openAction(env, second, stack)
	if len(args) > 2 {
		variant, _ = openText(env, args[2], stack)
	}
	return action, variant
}

func openIsVariant(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "primary", "secondary", "ghost", "outline", "link", "danger", "destructive":
		return true
	default:
		return false
	}
}

func openAction(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) string {
	if e.kind == "ref" {
		if stack[e.name] || len(stack) > 16 {
			return ""
		}
		next, ok := env[e.name]
		if !ok {
			return ""
		}
		stack[e.name] = true
		defer delete(stack, e.name)
		return openAction(env, next, stack)
	}
	if e.kind == "action" && strings.EqualFold(e.name, "ToAssistant") {
		if len(e.args) == 0 {
			return ""
		}
		text, _ := openText(env, e.args[0], stack)
		return strings.TrimSpace(text)
	}
	if e.kind == "string" {
		return strings.TrimSpace(e.s)
	}
	return ""
}

func OpenShort(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

func OpenTextStyle(size string) (font float32, heavy, muted bool) {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "small", "sm":
		return 12, false, true
	case "large", "lg":
		return 15, false, false
	case "large-heavy", "largeheavy", "xl":
		return 16, true, false
	default:
		return 13.5, false, false
	}
}

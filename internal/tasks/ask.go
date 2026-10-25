package tasks

// Ask is a question an operation puts to the user mid-run: a name
// conflict with the choices it allows, or a free-text password after the
// archive password book failed. The task keeps its state and slot while
// it waits; the UI shows the question inline and calls Manager.Answer.
type Ask struct {
	Item    string   // what the question is about ("报告.pdf" / "locked.zip")
	Detail  string   // e.g. "目标已存在" / "需要解压密码"
	Options []string // choice ids, in order (replace, skip, keep / submit, cancel)
	Rest    int      // how many items an "apply to the rest" would cover
	// Kind is AskKindPassword for a password field; empty means button choices.
	Kind string
}

// Answer is the chosen option; All applies it to the rest of the run.
// Text/Save are used by AskKindPassword (the typed password and whether
// to append it to the password book).
type Answer struct {
	Choice string
	All    bool
	Text   string
	Save   bool
}

// Conflict choices.
const (
	ChoiceReplace   = "replace"
	ChoiceSkip      = "skip"
	ChoiceKeep      = "keep"
	ChoiceSubmit    = "submit"
	ChoiceCancel    = "cancel"
	AskKindPassword = "password"
)

// Ask puts q to the user and blocks until it is answered or the task is
// canceled (ErrCanceled). A nil Reporter answers with the last option.
func (r *Reporter) Ask(q Ask) (Answer, error) {
	if r == nil {
		if len(q.Options) == 0 {
			return Answer{}, nil
		}
		return Answer{Choice: q.Options[len(q.Options)-1]}, nil
	}
	ch := make(chan Answer, 1)
	r.t.mu.Lock()
	qq := q
	r.t.snap.Ask, r.t.answer = &qq, ch
	r.t.mu.Unlock()
	r.t.m.emit(Event{ID: r.t.snap.ID, Type: EventState})
	defer func() {
		r.t.mu.Lock()
		r.t.snap.Ask, r.t.answer = nil, nil
		r.t.mu.Unlock()
		r.t.m.emit(Event{ID: r.t.snap.ID, Type: EventState})
	}()
	select {
	case a := <-ch:
		return a, nil
	case <-r.t.ctx.Done():
		return Answer{}, ErrCanceled
	}
}

// Answer replies to the question task id waits on.
func (m *Manager) Answer(id string, a Answer) error {
	t := m.find(id)
	if t == nil {
		return ErrNoTask
	}
	t.mu.Lock()
	ch := t.answer
	t.mu.Unlock()
	if ch == nil {
		return ErrState
	}
	select {
	case ch <- a:
	default:
	}
	return nil
}

package fsops

// Move records one item's before and after locations for the app's undo
// stack. The application keeps stack lifetime and feedback; this package
// only owns the portable operation data.
type Move struct {
	From, To string
}

// Undo describes one reversible file operation.
type Undo struct {
	Desc    string
	Created []string
	Moved   []Move
}

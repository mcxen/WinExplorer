module sunvale

go 1.27.2

require (
	github.com/egoist/mygo v0.5.5
	github.com/oneo-me/onevra/source v0.2026.11
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// replace pins the latest local checkout of Onevra (main @ 587ccf0), which
// includes fixes past the v0.2026.11 tag. Remove this line once the next
// Onevra release ships.
replace github.com/oneo-me/onevra/source => /Users/mcx/onevra/source

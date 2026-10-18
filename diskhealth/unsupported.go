package diskhealth

import "errors"

// errNotSupported marks a platform that has no native way to answer a
// probe, so the caller falls back to a portable source instead.
var errNotSupported = errors.New("diskhealth: not supported on this platform")

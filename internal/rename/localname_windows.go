//go:build windows

package rename

// LocalNameProblem applies the local filesystem's naming rules: NTFS's
// illegal characters, device names and trailing dot or space.
func LocalNameProblem(name string) string { return WindowsNameProblem(name) }

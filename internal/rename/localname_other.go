//go:build !windows && !darwin

package rename

// LocalNameProblem applies the local filesystem's naming rules: on Linux
// and the rest, every byte but NUL and the separator is a valid name.
func LocalNameProblem(string) string { return "" }

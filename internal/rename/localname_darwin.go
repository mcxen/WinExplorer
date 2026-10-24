//go:build darwin

package rename

import "strings"

// LocalNameProblem applies the local filesystem's naming rules: HFS+ and
// APFS store the colon historically used as the path separator, so Finder
// refuses it in a name.
func LocalNameProblem(name string) string {
	if strings.Contains(name, ":") {
		return "名称不能包含冒号"
	}
	return ""
}

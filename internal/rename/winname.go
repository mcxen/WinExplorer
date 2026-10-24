package rename

import "strings"

// winname.go holds the Windows filename rules as a pure function, shared
// (not behind the windows build tag) so its table test runs on every
// platform CI covers. Explorer enforces these at input time; without them
// a rename fails late with a raw syscall error — or worse, Win32 silently
// strips a trailing dot or space and the item lands under a name other
// than the one typed.

// windowsReservedStems are the DOS device names Windows still refuses,
// whatever extension follows them (CON.txt is as invalid as CON).
var windowsReservedStems = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// WindowsNameProblem reports why name cannot exist on Windows, or "".
func WindowsNameProblem(name string) string {
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
			return `名称不能包含 < > : " | ? * 等字符`
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	if windowsReservedStems[strings.ToUpper(stem)] {
		return "名称不能是设备名（如 CON、NUL、COM1）"
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "名称不能以点或空格结尾"
	}
	return ""
}

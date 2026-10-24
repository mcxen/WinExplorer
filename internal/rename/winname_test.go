package rename

import "testing"

// TestWindowsNameProblem covers the rules Explorer enforces at input time:
// illegal characters (control bytes included), device names with any
// extension, and a trailing dot or space Win32 would silently strip. It
// runs on every platform — the rules are a pure function — so CI catches a
// regression without a Windows runner.
func TestWindowsNameProblem(t *testing.T) {
	const (
		badChar = `名称不能包含 < > : " | ? * 等字符`
		badDev  = "名称不能是设备名（如 CON、NUL、COM1）"
		badTail = "名称不能以点或空格结尾"
	)
	cases := []struct {
		name string
		want string
	}{
		{"笔记.txt", ""},
		{"新建文件夹", ""},
		{" leading space is kept", ""},
		{"a<b.txt", badChar},
		{"a:b", badChar},
		{`say"no`, badChar},
		{"星*号", badChar},
		{"a\x01b", badChar},
		{"CON", badDev},
		{"con.txt", badDev},
		{"NUL.tar.gz", badDev},
		{"COM7.log", badDev},
		{"lpt9", badDev},
		{"console.txt", ""}, // the stem is "console", not the device
		{" концерт.flac", ""},
		{"结尾点.", badTail},
		{"结尾空格 ", badTail},
	}
	for _, c := range cases {
		if got := WindowsNameProblem(c.name); got != c.want {
			t.Errorf("WindowsNameProblem(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

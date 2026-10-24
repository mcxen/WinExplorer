package main

import (
	"strings"
	"testing"
)

// TestShellDeleteErrorText keeps the status line's delete wording mapped:
// the codes users actually hit (access denied, sharing violation, the
// long-path failure) name a reason; the unmapped rest keeps its number.
func TestShellDeleteErrorText(t *testing.T) {
	cases := []struct {
		code  int
		holds string
	}{
		{0x78, "拒绝访问"},
		{5, "拒绝访问"},
		{0x20, "正被其他程序占用"},
		{0x7F, "路径过长"},
		{0x85, "网络错误"},
		{0x2, "不存在"},
		{9999, "9999"},
	}
	for _, c := range cases {
		if got := shellDeleteErrorText(c.code); !strings.Contains(got, c.holds) {
			t.Errorf("shellDeleteErrorText(%d) = %q, want it to contain %q", c.code, got, c.holds)
		}
	}
}

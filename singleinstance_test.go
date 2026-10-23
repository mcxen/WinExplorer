package main

import "testing"

func TestAcquireSingleInstance(t *testing.T) {
	cases := []struct {
		name      string
		sockFirst bool
		osFree    int // attempt at which the mutex becomes free; -1 never
		want      bool
		raised    bool
	}{
		{"first", true, 0, true, false},
		{"second via socket", false, 0, false, false},
		{"relaunch: old process exits", true, 3, true, false},
		{"socket broken, mutex held", true, -1, false, true},
	}
	for _, c := range cases {
		raised, tries := false, 0
		got := acquireSingleInstance(singleInstanceDeps{
			osLock: func() (bool, func()) {
				free := c.osFree >= 0 && tries >= c.osFree
				tries++
				return free, func() {}
			},
			socketLock: func() bool { return c.sockFirst },
			raise:      func() { raised = true },
			retries:    5,
			sleep:      func() {},
		})
		if got != c.want || raised != c.raised {
			t.Errorf("%s: go on=%v raised=%v, want %v %v", c.name, got, raised, c.want, c.raised)
		}
	}
}

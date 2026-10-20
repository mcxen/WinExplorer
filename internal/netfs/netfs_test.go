package netfs

import "testing"

// TestIsNetworkPathLocal: a temp folder is local, the cached answer agrees on
// the second ask, and the empty path is never a network path. A network
// mount cannot be relied on in a test, so the negative side is what runs;
// the platform probes are exercised by every listing of a real share.
func TestIsNetworkPathLocal(t *testing.T) {
	dir := t.TempDir()
	ForgetNetworkPath(dir)
	if IsNetworkPath(dir) {
		t.Fatalf("%s reads as a network path", dir)
	}
	if IsNetworkPath(dir) {
		t.Fatalf("%s reads as a network path on the cached answer too", dir)
	}
	if IsNetworkPath("") {
		t.Fatal("the empty path reads as a network path")
	}
}

// TestForgetNetworkPath: dropping a verdict makes the next ask probe again,
// which is what a remounted share needs. The re-probe is observable as the
// same answer arriving from a cache-less lookup.
func TestForgetNetworkPath(t *testing.T) {
	dir := t.TempDir()
	if IsNetworkPath(dir) {
		t.Fatalf("%s reads as a network path", dir)
	}
	if _, cached := netfsCache.Load(dir); !cached {
		t.Fatal("the verdict was not cached")
	}
	ForgetNetworkPath(dir)
	if _, cached := netfsCache.Load(dir); cached {
		t.Fatal("the verdict survived ForgetNetworkPath")
	}
	if IsNetworkPath(dir) {
		t.Fatalf("%s reads as a network path after the cache drop", dir)
	}
}

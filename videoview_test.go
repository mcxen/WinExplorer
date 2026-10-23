package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestClampVidFrac(t *testing.T) {
	if clampVidFrac(-0.2) != 0 || clampVidFrac(1.5) != 1 || clampVidFrac(0.4) != 0.4 {
		t.Fatalf("clampVidFrac bounds")
	}
}

func TestVideoAtSec(t *testing.T) {
	if videoAtSec(0.5, 10) != 5 {
		t.Fatalf("mid")
	}
	if videoAtSec(2, 10) != 10 {
		t.Fatalf("clamp high")
	}
	if videoAtSec(0.5, 0) != 0 {
		t.Fatalf("zero duration")
	}
}

func TestResetVidQV(t *testing.T) {
	a := &App{}
	a.resetVidQV("/a.mp4", 10, nil)
	a.vidQV.frac = 0.4
	a.resetVidQV("/a.mp4", 10, nil)
	if a.vidQV.frac != 0.4 {
		t.Fatalf("same path must keep scrub: %+v", a.vidQV)
	}
	a.resetVidQV("/b.mp4", 20, nil)
	if a.vidQV.path != "/b.mp4" || a.vidQV.frac == 0.4 {
		t.Fatalf("new path must reset: %+v", a.vidQV)
	}
	// Long clip seeds near 1s.
	a.resetVidQV("/c.mp4", 100, nil)
	want := 1.0 / 100
	if a.vidQV.frac != want {
		t.Fatalf("seed frac = %v want %v", a.vidQV.frac, want)
	}
}

func hasFFmpeg(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func writeTestMP4(t *testing.T, path string) {
	t.Helper()
	if !hasFFmpeg(t) {
		t.Skip("ffmpeg not installed")
	}
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=8",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg make clip: %v\n%s", err, out)
	}
}

func TestBuildPreviewKeepsVideoKind(t *testing.T) {
	if !hasFFmpeg(t) {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "demo.mp4")
	writeTestMP4(t, clip)
	a := &App{}
	st := a.buildPreview(clip)
	if st.kind != previewVideo {
		t.Fatalf("kind = %v want previewVideo", st.kind)
	}
	if st.bitmap == nil && st.media.poster == nil {
		t.Fatal("expected poster bitmap")
	}
	if st.media.duration <= 0 {
		t.Fatalf("duration = %v", st.media.duration)
	}
}

func TestSetVidScrubFracRequestsFrame(t *testing.T) {
	if !hasFFmpeg(t) {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "scrub.mp4")
	writeTestMP4(t, clip)
	a := &App{}
	a.resetVidQV(clip, 2, nil)
	a.setVidScrubFrac(0.6)
	if a.vidQV.frac != 0.6 {
		t.Fatalf("frac = %v", a.vidQV.frac)
	}
	// Wait for debounced extract.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.vidQV.frame != nil && !a.vidQV.loading {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("scrub frame never arrived: loading=%v err=%q", a.vidQV.loading, a.vidQV.err)
}

func TestVideoFrameAtMissingBinary(t *testing.T) {
	// Force a path that is not a video so ffmpeg fails even if present.
	dir := t.TempDir()
	bad := filepath.Join(dir, "not-video.mp4")
	os.WriteFile(bad, []byte("not a video"), 0o644)
	_, err := videoFrameAt(bad, 0, 64)
	if err == nil {
		t.Fatal("expected error for corrupt video")
	}
}

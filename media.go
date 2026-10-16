package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
)

// audioPlayer wraps a MyGo audio.Player with a one-shot state used by the
// preview pane's transport controls. MyGo's audio package decodes WAV,
// MP3 (audio/mp3) and Ogg Vorbis (audio/vorbis) without cgo; other audio
// formats fall back to opening in the system player.
type audioPlayer struct {
	player *audio.Player
	f      *os.File
	dur    time.Duration
	ticker chan struct{}
	mu     sync.Mutex
}

// stop halts playback, ends the repaint ticker and closes the underlying
// file, which OpenStream still reads from while it plays.
func (p *audioPlayer) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.player != nil {
		p.player.Stop()
	}
	if p.ticker != nil {
		close(p.ticker)
		p.ticker = nil
	}
	if p.f != nil {
		p.f.Close()
		p.f = nil
	}
}

// startTicker repaints the window ~4×/s while the sound plays, so the
// position text and the seek bar advance. One ticker per player.
func (p *audioPlayer) startTicker() {
	p.mu.Lock()
	if p.player == nil || p.ticker != nil || !p.player.Playing() {
		p.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	p.ticker = stop
	pl := p.player
	p.mu.Unlock()
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if !pl.Playing() {
					return
				}
				repaint()
			}
		}
	}()
}

// loadAudio opens path as a streamed sound if MyGo can decode it. The
// file stays open for the player's lifetime; stop() closes it.
func loadAudio(path string) mediaInfo {
	f, err := os.Open(path)
	if err != nil {
		return mediaInfo{}
	}
	player, err := audio.OpenStream(f)
	if err != nil {
		f.Close()
		return mediaInfo{}
	}
	return mediaInfo{player: &audioPlayer{player: player, f: f, dur: player.Duration()}}
}

// isAudioDecodable reports whether MyGo's audio package can play ext.
func isAudioDecodable(ext string) bool {
	switch ext {
	case "wav", "aiff", "aif", "mp3", "ogg", "oga", "m4a", "flac", "opus":
		return true
	}
	return false
}

// loadVideo extracts a poster frame via ffmpeg and reports the duration via
// ffprobe. Full playback is delegated to the system player (see openWithOS),
// matching how Explorer's preview pane shows a still, not an inline player.
func loadVideo(path string) mediaInfo {
	mi := mediaInfo{}
	mi.duration = videoDuration(path)
	mi.poster = videoPoster(path)
	return mi
}

// videoDuration returns the clip length in seconds, or 0 if unknown.
func videoDuration(path string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	var d float64
	if _, e := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &d); e != nil {
		return 0
	}
	return d
}

// videoPoster renders a frame from a video at preview size.
func videoPoster(path string) *ui.Bitmap {
	img, err := videoPosterImage(path, 1024)
	if err != nil {
		return nil
	}
	return ui.NewBitmap(img)
}

// videoPosterImage renders a frame from a video to a PNG with ffmpeg and
// decodes it. It tries one second in (where a clip has a title card) and
// falls back to the first frame, scaled to fit side px. Returns an error
// when ffmpeg is unavailable or the extraction fails.
func videoPosterImage(path string, side int) (image.Image, error) {
	side = max(side, 16)
	dir, err := os.MkdirTemp("", "we-vp")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "poster.png")
	filter := fmt.Sprintf("scale='min(%d,iw)':-1", side)

	attempts := [][]string{
		{"-y", "-ss", "00:00:01", "-i", path, "-frames:v", "1", "-update", "1", "-vf", filter, out},
		{"-y", "-i", path, "-frames:v", "1", "-update", "1", "-vf", filter, out},
	}
	var last error
	for _, args := range attempts {
		if b, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
			last = fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(b)))
			continue
		}
		f, e := os.Open(out)
		if e != nil {
			last = e
			continue
		}
		img, _, e := image.Decode(f)
		f.Close()
		if e != nil {
			last = e
			continue
		}
		return img, nil
	}
	if last == nil {
		last = fmt.Errorf("没有提取到画面")
	}
	return nil, last
}

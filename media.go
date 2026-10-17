package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// The OS fallback covers the formats MyGo's decoders do not (m4a,
	// aiff, flac…): the file plays through the OS's player with its
	// reported duration for the clock. Seek is not available in it.
	afName    string
	afArgs    []string
	afCmd     *exec.Cmd
	afStarted time.Time
	mu        sync.Mutex
}

// stop halts playback, ends the repaint ticker, stops an afplay run and
// closes the underlying file, which OpenStream still reads from while it
// plays.
func (p *audioPlayer) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.player != nil {
		p.player.Stop()
	}
	p.stopAfplayLocked()
	if p.ticker != nil {
		close(p.ticker)
		p.ticker = nil
	}
	if p.f != nil {
		p.f.Close()
		p.f = nil
	}
}

func (p *audioPlayer) stopAfplayLocked() {
	if p.afCmd != nil && p.afCmd.Process != nil {
		_ = p.afCmd.Process.Kill()
		_ = p.afCmd.Wait()
	}
	p.afCmd = nil
}

// playing reports whether sound is coming out, either through MyGo's
// player or an afplay run.
func (p *audioPlayer) playing() bool {
	if p == nil {
		return false
	}
	if p.player != nil {
		return p.player.Playing()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playingLocked()
}

// playingLocked is playing's afplay check, with the mutex held.
func (p *audioPlayer) playingLocked() bool {
	return p.afCmd != nil && p.afCmd.Process != nil && p.afCmd.ProcessState == nil
}

// toggle starts or stops the sound, whichever way it is played. It never
// locks twice: startTicker takes the mutex itself.
func (p *audioPlayer) toggle() {
	if p == nil {
		return
	}
	if p.player != nil {
		if p.player.Playing() {
			p.player.Pause()
		} else {
			p.player.Play()
		}
		p.startTicker()
		return
	}
	p.mu.Lock()
	if p.afCmd != nil {
		p.stopAfplayLocked()
		p.mu.Unlock()
		return
	}
	if p.afName == "" {
		p.mu.Unlock()
		return
	}
	cmd := exec.Command(p.afName, p.afArgs...)
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return
	}
	p.afCmd = cmd
	p.afStarted = time.Now()
	p.mu.Unlock()
	p.startTicker()
}

// position is how far into the sound we are, afplay's being the elapsed
// wall time capped at the duration.
func (p *audioPlayer) position() time.Duration {
	if p == nil {
		return 0
	}
	if p.player != nil {
		return p.player.Position()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pos := time.Since(p.afStarted)
	if p.afCmd == nil || pos > p.dur {
		return p.dur
	}
	return pos
}

// startTicker repaints the window ~4×/s while the sound plays, so the
// position text and the seek bar advance. One ticker per player.
func (p *audioPlayer) startTicker() {
	p.mu.Lock()
	if p.ticker != nil || (p.player != nil && !p.player.Playing()) || (p.player == nil && !p.playingLocked()) {
		p.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	p.ticker = stop
	self := p
	p.mu.Unlock()
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if !self.playing() {
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
		// Formats MyGo's decoders do not cover (m4a, aiff, flac…) still
		// preview and play through the OS's fallback player.
		if name, args, dur, ok := sysAudioPlay(path); ok {
			return mediaInfo{player: &audioPlayer{afName: name, afArgs: args, dur: dur}}
		}
		return mediaInfo{}
	}
	return mediaInfo{player: &audioPlayer{player: player, f: f, dur: player.Duration()}}
}

// parseAfinfoDuration reads the "estimated duration: 123.456 sec" line.
func parseAfinfoDuration(out string) (time.Duration, bool) {
	const key = "estimated duration:"
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, key)
		if i < 0 {
			continue
		}
		v := strings.TrimSpace(line[i+len(key):])
		v = strings.TrimSuffix(strings.TrimSpace(v), "sec")
		if d, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && d > 0 {
			return time.Duration(d * float64(time.Second)), true
		}
	}
	return 0, false
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

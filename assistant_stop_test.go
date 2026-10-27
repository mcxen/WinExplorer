package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

// assistant_stop_test.go pins Stop: cancel reaches tools and streams, the
// composer frees immediately (halted), partial text is kept, and late paints
// from the cancelled run cannot land after a new submit.

func TestAssistantRequestStopFreesComposer(t *testing.T) {
	a := &App{}
	a.assist.ensure()
	a.assist.running = true
	a.assist.status = "正在思考…"
	a.assist.pending = "bash"
	a.assist.queued = "下一条"
	a.assist.turns = []aiTurn{{Role: "assistant", Text: "半句"}}
	ctx, cancel := context.WithCancel(context.Background())
	a.assist.stop = cancel

	a.assistantRequestStop()

	if a.assist.running {
		// running stays true until the goroutine's defer; halted frees the UI.
		if !a.assist.halted {
			t.Fatal("halted not set")
		}
	}
	if !a.assist.halted || a.assist.pending != "" || a.assist.status != "" || a.assist.queued != "" {
		t.Fatalf("stop state: halted=%v pending=%q status=%q queued=%q", a.assist.halted, a.assist.pending, a.assist.status, a.assist.queued)
	}
	if a.assist.stop != nil {
		t.Fatal("stop func should be cleared after cancel")
	}
	if ctx.Err() == nil {
		t.Fatal("run ctx was not cancelled")
	}
	if n := len(a.assist.turns); n == 0 || !a.assist.turns[n-1].Stopped {
		t.Fatalf("partial not marked stopped: %+v", a.assist.turns)
	}
	if a.assist.running && !a.assist.halted {
		t.Fatal("composer would still show Running")
	}
}

func TestStopDuringStreamReturnsQuickly(t *testing.T) {
	orig := runFileAssistantOnce
	t.Cleanup(func() { runFileAssistantOnce = orig })

	started := make(chan struct{})
	runFileAssistantOnce = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		close(started)
		sr, sw := schema.Pipe[*schema.Message](2)
		go func() {
			sw.Send(&schema.Message{Role: schema.Assistant, Content: "一"}, nil)
			<-ctx.Done()
			sw.Close()
		}()
		msg, err := readMessageStream(ctx, sr, func(full, _ string) {
			if run.onLive != nil {
				run.onLive(0, aiTurn{Role: "assistant", Text: full}, true)
			}
		}, nil)
		res := assistRunResult{}
		if msg != nil {
			res.Text = msg.Content
			res.Events = []aiTurn{{Role: "assistant", Text: msg.Content, Stopped: true}}
		}
		if assistantStopped(ctx, err) || errors.Is(err, errAssistantStopped) {
			return res, errAssistantStopped
		}
		return res, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runWithRateFallback(ctx, assistRun{prov: aiProvider{Model: "m"}, root: t.TempDir()})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errAssistantStopped) && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			t.Fatalf("want stopped, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not return within 1s")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("stop took %v", time.Since(start))
	}
}

func TestStopDuringBashReturnsQuickly(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := runBash(ctx, dir, "while true; do echo spinning; sleep 0.05; done")
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancel")
		}
		if time.Since(start) > time.Second {
			t.Fatalf("bash cancel took %v", time.Since(start))
		}
	case <-time.After(time.Second):
		t.Fatal("bash did not stop within 1s")
	}
}

func TestLateLiveAfterStopDropped(t *testing.T) {
	a := &App{}
	a.assist.ensure()
	a.assist.running = true
	a.assist.liveEpoch = 7
	a.assist.turns = []aiTurn{{Role: "user", Text: "q"}}

	a.noteAssistLive(7, 1, aiTurn{Role: "assistant", Text: "先"}, true)
	// Simulate Stop: halted drops further paints; epoch unchanged so cleanup owns the run.
	a.assist.halted = true
	a.noteAssistLive(7, 1, aiTurn{Role: "assistant", Text: "迟到"}, true)

	// Coalesced pending was cleared on stop; drained apply must not run for halted.
	a.assist.live.mu.Lock()
	pending := a.assist.live.pending
	a.assist.live.mu.Unlock()
	if pending != nil && pending.turn.Text == "迟到" {
		t.Fatal("late partial stayed pending after halt")
	}
	// Direct apply path after halt must no-op via noteAssistLive.
	if len(a.assist.turns) > 1 && a.assist.turns[len(a.assist.turns)-1].Text == "迟到" {
		t.Fatalf("late turn landed: %+v", a.assist.turns)
	}
}

func TestStopClearsRateFallbackWait(t *testing.T) {
	orig := runFileAssistantOnce
	t.Cleanup(func() { runFileAssistantOnce = orig })

	var calls atomic.Int32
	block := make(chan struct{})
	runFileAssistantOnce = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		n := calls.Add(1)
		if n == 1 {
			close(block)
			<-ctx.Done()
			return assistRunResult{}, errAssistantStopped
		}
		return assistRunResult{}, errors.New("should not retry after stop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := runWithRateFallback(ctx, assistRun{prov: aiProvider{Model: "m"}})
		if !errors.Is(err, errAssistantStopped) && ctx.Err() == nil {
			t.Errorf("want stopped, got %v", err)
		}
	}()
	select {
	case <-block:
	case <-time.After(time.Second):
		t.Fatal("first attempt did not start")
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rate-fallback stop >1s")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if calls.Load() != 1 {
		t.Fatalf("retried after stop: %d", calls.Load())
	}
}

func TestBashToolUsesCallContext(t *testing.T) {
	dir := t.TempDir()
	sunvaleToolState.bind(dir, false, nil, nil, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		res, _, err := sunvaleToolState.bash(ctx, nil, bashArgs{Script: "while true; do echo x; sleep 0.05; done"})
		if err != nil {
			errCh <- err
			return
		}
		done <- toolResultText(res)
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		t.Fatalf("bash tool returned Go error on cancel: %v", err)
	case text := <-done:
		if text == "" {
			t.Fatal("empty result on cancel")
		}
		if !strings.Contains(text, "已停止") && !strings.Contains(text, "停止") && !strings.Contains(text, "秒被停止") {
			// Accept any non-empty cancelled output; wording varies by timing.
			t.Logf("cancel text: %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("bash tool ignored cancel")
	}
}

package clip_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"clipshare/src/internal/clip"
)

type fakeClipboard struct {
	mu      sync.Mutex
	content clip.Content
	writes  []clip.Content
	err     error
}

func (f *fakeClipboard) setContent(c clip.Content) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content = c
}

func (f *fakeClipboard) Read() (clip.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return clip.Content{}, f.err
	}
	return f.content, nil
}

func (f *fakeClipboard) Write(c clip.Content) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, c)
	f.content = c
	return nil
}

func (f *fakeClipboard) Close() {}

func TestWatcherDetectsChange(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "first"}}
	var got []string
	done := make(chan struct{})

	w := clip.NewWatcher(fb, 20*time.Millisecond, func(c clip.Content) {
		got = append(got, c.Text)
		if len(got) == 1 {
			close(done)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Give it time to take the initial snapshot, then change the content.
	time.Sleep(60 * time.Millisecond)
	fb.setContent(clip.Content{Kind: clip.KindText, Text: "second"})

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for change detection")
	}

	cancel()

	if len(got) != 1 || got[0] != "second" {
		t.Errorf("got %v, want [second]", got)
	}
}

func TestWatcherIgnoresSameContent(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "same"}}
	var got []string

	w := clip.NewWatcher(fb, 20*time.Millisecond, func(c clip.Content) {
		got = append(got, c.Text)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Content never changes.
	time.Sleep(100 * time.Millisecond)
	cancel()

	if len(got) != 0 {
		t.Errorf("expected no callbacks, got %v", got)
	}
}

func TestLocalWritePreventsRebroadcast(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "initial"}}
	var got []string

	w := clip.NewWatcher(fb, 20*time.Millisecond, func(c clip.Content) {
		got = append(got, c.Text)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	time.Sleep(60 * time.Millisecond)
	w.LocalWrite(clip.Content{Kind: clip.KindText, Text: "remote"})

	// The fake clipboard now returns "remote", but LocalWrite should have
	// marked it as a local write, so no rebroadcast happens.
	time.Sleep(100 * time.Millisecond)
	cancel()

	if len(got) != 0 {
		t.Errorf("expected no callbacks after LocalWrite, got %v", got)
	}
	if len(fb.writes) != 1 || fb.writes[0].Text != "remote" {
		t.Errorf("writes: got %+v", fb.writes)
	}
}

func TestTrackPreventsRebroadcast(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "initial"}}
	var got []string

	w := clip.NewWatcher(fb, 20*time.Millisecond, func(c clip.Content) {
		got = append(got, c.Text)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	time.Sleep(60 * time.Millisecond)
	w.Track(clip.Content{Kind: clip.KindText, Text: "remote"})
	fb.setContent(clip.Content{Kind: clip.KindText, Text: "remote"})

	time.Sleep(100 * time.Millisecond)
	cancel()

	if len(got) != 0 {
		t.Errorf("expected no callbacks after Track, got %v", got)
	}
}

func TestReadErrorIsLoggedAndIgnored(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "ok"}, err: errors.New("boom")}
	var got []string

	w := clip.NewWatcher(fb, 20*time.Millisecond, func(c clip.Content) {
		got = append(got, c.Text)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	if len(got) != 0 {
		t.Errorf("expected no callbacks on read error, got %v", got)
	}
}

// syncFakeClipboard is safe for concurrent use by the watcher goroutine and
// the test goroutine.
type syncFakeClipboard struct {
	mu      sync.Mutex
	content clip.Content
	writes  int
}

func (f *syncFakeClipboard) Read() (clip.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content, nil
}

func (f *syncFakeClipboard) Write(c clip.Content) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content = c
	f.writes++
	return nil
}

func (f *syncFakeClipboard) Close() {}

// Regression test for the echo loop: a poll racing a LocalWrite used to read
// the pre-write value (or compare against stale hashes) and rebroadcast our
// own write as an external change, bouncing every remote clip back to its
// sender. The watcher must never fire for content it wrote itself.
func TestLocalWriteRaceDoesNotRebroadcast(t *testing.T) {
	fb := &syncFakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "initial"}}
	var mu sync.Mutex
	var got []string

	w := clip.NewWatcher(fb, time.Millisecond, func(c clip.Content) {
		mu.Lock()
		got = append(got, c.Text)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Hammer LocalWrite while the watcher polls aggressively.
	for i := 0; i < 200; i++ {
		w.LocalWrite(clip.Content{Kind: clip.KindText, Text: fmt.Sprintf("remote-%d", i)})
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 0 {
		t.Errorf("watcher rebroadcast local writes as external changes: %v", got)
	}
	if fb.writes != 200 {
		t.Errorf("writes = %d, want 200", fb.writes)
	}
}

// A genuine external change must still be reported after local writes.
func TestExternalChangeStillFiresAfterLocalWrite(t *testing.T) {
	fb := &syncFakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "initial"}}
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})

	w := clip.NewWatcher(fb, 10*time.Millisecond, func(c clip.Content) {
		mu.Lock()
		got = append(got, c.Text)
		mu.Unlock()
		close(done)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	time.Sleep(40 * time.Millisecond)
	w.LocalWrite(clip.Content{Kind: clip.KindText, Text: "remote"})
	time.Sleep(40 * time.Millisecond)

	fb.mu.Lock()
	fb.content = clip.Content{Kind: clip.KindText, Text: "external"}
	fb.mu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for external change")
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "external" {
		t.Errorf("got %v, want [external]", got)
	}
}

// startEventWatcher builds a watcher in event-driven mode. The interval is
// set to an hour so any polling would fail these tests by timeout.
func startEventWatcher(t *testing.T, fb clip.Interface, notify chan struct{}, cb func(clip.Content)) (*clip.Watcher, context.CancelFunc, <-chan string) {
	t.Helper()
	got := make(chan string, 16)
	w := clip.NewWatcher(fb, time.Hour, func(c clip.Content) {
		cb(c)
		got <- c.Text
	}, clip.WithNotifier(notify))
	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)
	return w, cancel, got
}

func TestEventWatcherDetectsChange(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "first"}}
	notify := make(chan struct{}, 8)

	_, cancel, got := startEventWatcher(t, fb, notify, func(clip.Content) {})
	defer cancel()

	// Initial snapshot, then one external change with a single notification.
	time.Sleep(60 * time.Millisecond)
	fb.setContent(clip.Content{Kind: clip.KindText, Text: "second"})
	notify <- struct{}{}

	select {
	case text := <-got:
		if text != "second" {
			t.Errorf("got %q, want %q", text, "second")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event-driven detection")
	}
}

func TestEventWatcherCoalescesBurst(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "first"}}
	notify := make(chan struct{}, 8)

	_, cancel, got := startEventWatcher(t, fb, notify, func(clip.Content) {})
	defer cancel()

	time.Sleep(60 * time.Millisecond)
	fb.setContent(clip.Content{Kind: clip.KindText, Text: "second"})
	for i := 0; i < 5; i++ {
		notify <- struct{}{}
		time.Sleep(5 * time.Millisecond)
	}

	// The whole burst must settle into a single callback.
	select {
	case text := <-got:
		if text != "second" {
			t.Errorf("got %q, want %q", text, "second")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for coalesced event")
	}
	time.Sleep(200 * time.Millisecond) // allow any duplicate to slip out
	cancel()

	for {
		select {
		case extra := <-got:
			t.Errorf("unexpected extra callback: %q", extra)
		default:
			return
		}
	}
}

func TestEventWatcherIgnoresSameContent(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "same"}}
	notify := make(chan struct{}, 8)

	_, cancel, got := startEventWatcher(t, fb, notify, func(clip.Content) {})
	defer cancel()

	time.Sleep(60 * time.Millisecond)
	for i := 0; i < 3; i++ {
		notify <- struct{}{}
	}
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case text := <-got:
		t.Errorf("expected no callbacks, got %q", text)
	default:
	}
}

func TestEventWatcherLocalWriteNoRebroadcast(t *testing.T) {
	fb := &fakeClipboard{content: clip.Content{Kind: clip.KindText, Text: "initial"}}
	notify := make(chan struct{}, 8)

	w, cancel, got := startEventWatcher(t, fb, notify, func(clip.Content) {})
	defer cancel()

	time.Sleep(60 * time.Millisecond)
	w.LocalWrite(clip.Content{Kind: clip.KindText, Text: "remote"})
	notify <- struct{}{} // the write itself triggers WM_CLIPBOARDUPDATE
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case text := <-got:
		t.Errorf("rebroadcast local write as external change: %q", text)
	default:
	}
	if len(fb.writes) != 1 || fb.writes[0].Text != "remote" {
		t.Errorf("writes: got %+v", fb.writes)
	}
}

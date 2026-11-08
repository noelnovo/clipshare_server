package clip

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"clipshare/src/internal/log"
)

// eventSettleDelay lets a burst of clipboard-change notifications die down
// before reading. Clipboard writers frequently place several updates back to
// back, and the final writer may still hold the clipboard open when the
// notification arrives.
const eventSettleDelay = 50 * time.Millisecond

// Watcher polls the clipboard for external changes and fires onChange with
// the new content. It applies loop protection: content written by the local
// client (via Track/LocalWrite) is not reported again.
//
// The mutex serializes poll reads against LocalWrite/Track so a tick that
// races a local write can never observe the pre-write value and misreport it
// as an external change (which would echo content back to its sender).
type Watcher struct {
	mu       sync.Mutex
	clip     Interface
	interval time.Duration
	onChange func(Content)
	lastHash uint64
	skipHash uint64

	// notify, when non-nil, delivers clipboard-change events and switches Run
	// to event-driven mode (no polling). Set via WithNotifier before Run.
	notify <-chan struct{}
}

// WatcherOption customizes optional Watcher behavior.
type WatcherOption func(*Watcher)

// WithNotifier switches the watcher to event-driven mode: it reacts to
// notifications on ch instead of polling every interval. The interval is
// unused in this mode.
func WithNotifier(ch <-chan struct{}) WatcherOption {
	return func(w *Watcher) { w.notify = ch }
}

func NewWatcher(c Interface, interval time.Duration, onChange func(Content), opts ...WatcherOption) *Watcher {
	w := &Watcher{clip: c, interval: interval, onChange: onChange}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func hashContent(c Content) uint64 {
	h := fnv.New64a()
	switch c.Kind {
	case KindImage:
		h.Write([]byte{0})
		h.Write(c.Image)
		h.Write([]byte(c.Mime))
	default:
		h.Write([]byte{1})
		h.Write([]byte(c.Text))
	}
	return h.Sum64()
}

// Run watches the clipboard until ctx is cancelled: event-driven when a
// notifier is attached, interval polling otherwise.
func (w *Watcher) Run(ctx context.Context) {
	w.mu.Lock()
	w.lastHash = w.snapshot()
	w.mu.Unlock()

	if w.notify != nil {
		w.runEvents(ctx)
		return
	}

	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.check()
		}
	}
}

// runEvents reacts to clipboard-change notifications instead of polling. Each
// notification settles first so bursts coalesce into a single read.
func (w *Watcher) runEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.notify:
			if !w.settle(ctx) {
				return
			}
			w.check()
		}
	}
}

// settle waits eventSettleDelay for the notification burst to finish,
// extending the window for every further notification. It returns false if
// ctx was cancelled while waiting.
func (w *Watcher) settle(ctx context.Context) bool {
	t := time.NewTimer(eventSettleDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.notify:
			if !t.Stop() {
				<-t.C
			}
			t.Reset(eventSettleDelay)
		case <-t.C:
			return true
		}
	}
}

// check reads the clipboard and fires onChange when its hash changed since
// the previous check. Read errors are logged and treated as no change.
func (w *Watcher) check() {
	w.mu.Lock()
	c, err := w.clip.Read()
	if err != nil {
		w.mu.Unlock()
		log.Errorf("clipboard read: %v", err)
		return
	}
	h := hashContent(c)
	fire := h != w.lastHash && h != w.skipHash
	w.lastHash = h
	w.mu.Unlock()
	if fire {
		w.onChange(c)
	}
}

// LocalWrite writes content to the clipboard and marks it so the watcher does
// not rebroadcast it as an external change.
func (w *Watcher) LocalWrite(c Content) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.clip.Write(c); err != nil {
		log.Errorf("clipboard write: %v", err)
		return
	}
	w.skipHash = hashContent(c)
	w.lastHash = hashContent(c)
}

// Track records a remote value as the current local state without writing.
func (w *Watcher) Track(c Content) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.skipHash = hashContent(c)
	w.lastHash = hashContent(c)
}

func (w *Watcher) snapshot() uint64 {
	c, err := w.clip.Read()
	if err != nil {
		return 0
	}
	return hashContent(c)
}

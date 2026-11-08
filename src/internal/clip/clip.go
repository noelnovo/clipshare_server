package clip

import (
	"context"
	"errors"
)

// Kind identifies clipboard content.
type Kind int

const (
	KindText Kind = iota
	KindImage
)

// Content is a clipboard payload: either text or an image (raw bytes).
type Content struct {
	Kind  Kind
	Text  string
	Image []byte
	Mime  string // MIME type for images, e.g. "image/png"
}

// Interface abstracts reading/writing the system clipboard.
type Interface interface {
	Read() (Content, error)
	Write(c Content) error
	Close()
}

// ErrNotifierUnavailable reports that the platform provides no clipboard
// change-notification mechanism (e.g. headless sessions without Wayland or
// X11, unsupported OSes), so the watcher falls back to interval polling.
// It is returned by StartClipboardNotifier.
var ErrNotifierUnavailable = errors.New("clipboard change notifications unavailable on this platform")

// StartClipboardNotifier starts delivering clipboard-change notifications on
// the returned channel (one buffered signal per change, bursts coalesced).
// The stop function idles the notifier; ctx cancellation stops it too. When
// the platform has no notification mechanism it returns
// ErrNotifierUnavailable and the caller should fall back to polling.
func StartClipboardNotifier(ctx context.Context) (<-chan struct{}, func(), error) {
	return startClipboardNotifier(ctx)
}

//go:build !windows && !linux

package clip

import "context"

// startClipboardNotifier has no implementation on this platform; the watcher
// keeps interval polling (see Watcher.Run).
func startClipboardNotifier(context.Context) (<-chan struct{}, func(), error) {
	return nil, func() {}, ErrNotifierUnavailable
}

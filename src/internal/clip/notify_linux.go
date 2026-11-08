//go:build linux

package clip

import (
	"bufio"
	"context"
	"fmt"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"os"
	"os/exec"
	"time"
)

// Linux clipboard-change notifications.
//
// Two event sources, tried in the same order the clipboard backend
// auto-detects them (see backend_linux.go):
//
//   - Wayland: wl-paste --watch (from wl-clipboard) subscribes to the
//     compositor's data-control selection events and runs a command on every
//     change — its output is piped back to the daemon, one line per change.
//   - X11: the XFixes SelectSelectionInput request delivers a
//     SelectionNotifyEvent whenever any client takes ownership of the
//     CLIPBOARD selection — no polling, no subprocess churn.
//
// Interval polling survives only as the Watcher's last-resort fallback for
// sessions where neither mechanism is available (e.g. headless).

func startClipboardNotifier(ctx context.Context) (<-chan struct{}, func(), error) {
	if ch, stop, err := startWaylandNotifier(ctx); err == nil {
		return ch, stop, nil
	}
	if ch, stop, err := startX11Notifier(ctx); err == nil {
		return ch, stop, nil
	}
	return nil, func() {}, ErrNotifierUnavailable
}

// startWaylandNotifier spawns `wl-paste --watch echo changed` and turns each
// line of its output into a notification signal. Availability mirrors the
// waylandBackend auto-detection in backend_linux.go: WAYLAND_DISPLAY plus a
// wl-paste binary on PATH. If wl-paste exits immediately (no compositor, bad
// session) the error propagates so the caller falls back to polling instead
// of going deaf.
func startWaylandNotifier(ctx context.Context) (<-chan struct{}, func(), error) {
	if os.Getenv("WAYLAND_DISPLAY") == "" || !haveCmd("wl-paste") {
		return nil, func() {}, ErrNotifierUnavailable
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, func() {}, fmt.Errorf("watch pipe: %w", err)
	}

	cmd := exec.CommandContext(ctx, "wl-paste", "--watch", "echo", "changed")
	cmd.Stdout = pw
	cmd.Stderr = nil // keep daemon stderr clean when the child fails
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, func() {}, fmt.Errorf("wl-paste watch: %w", err)
	}
	// Drop the parent's write end; only the child (and its --watch commands,
	// which inherit stdout) writes from here on.
	pw.Close()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ch := make(chan struct{}, 1)
	go func() {
		defer pr.Close()
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			select {
			case ch <- struct{}{}:
			default: // a change signal is already pending; coalesce
			}
		}
	}()

	// Detect instant death so a broken session degrades to polling rather
	// than an event watcher that never fires.
	select {
	case err := <-exited:
		return nil, func() {}, fmt.Errorf("wl-paste --watch exited immediately: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	stop := func() { _ = cmd.Process.Kill() }
	return ch, stop, nil
}

// startX11Notifier delivers X11 clipboard-change events via XFixes. It needs
// DISPLAY and an X server with the XFixes extension (universal on modern
// servers, including XWayland). The returned stop function closes the X
// connection, which unblocks the event loop; ctx cancellation does too.
func startX11Notifier(ctx context.Context) (<-chan struct{}, func(), error) {
	conn, err := xgb.NewConn()
	if err != nil {
		// No DISPLAY or unreachable server.
		return nil, func() {}, ErrNotifierUnavailable
	}
	if err := xfixes.Init(conn); err != nil {
		conn.Close()
		return nil, func() {}, ErrNotifierUnavailable
	}
	// Init only registers opcodes; the server rejects XFixes requests beyond
	// 1.0 until the client performs the QueryVersion handshake.
	// SelectSelectionInput needs 2.0; request 5.0 and let the server clamp.
	if _, err := xfixes.QueryVersion(conn, 5, 0).Reply(); err != nil {
		conn.Close()
		return nil, func() {}, ErrNotifierUnavailable
	}

	root := xproto.Setup(conn).DefaultScreen(conn).Root
	reply, err := xproto.InternAtom(conn, false, uint16(len("CLIPBOARD")), "CLIPBOARD").Reply()
	if err != nil || reply.Atom == xproto.AtomNone {
		conn.Close()
		return nil, func() {}, fmt.Errorf("intern CLIPBOARD atom: %v", err)
	}
	// Checked request: fail fast (and fall back to polling) on a broken or
	// ancient server rather than going deaf.
	if err := xfixes.SelectSelectionInputChecked(conn, root, reply.Atom,
		uint32(xfixes.SelectionEventMaskSetSelectionOwner)).Check(); err != nil {
		conn.Close()
		return nil, func() {}, fmt.Errorf("select selection input: %v", err)
	}

	ch := make(chan struct{}, 1)
	go func() {
		for {
			ev, xerr := conn.WaitForEvent()
			if ev == nil || xerr != nil {
				return // connection closed by stop/ctx
			}
			// Only SetSelectionOwner was subscribed to, so every
			// SelectionNotifyEvent here is a clipboard change.
			select {
			case ch <- struct{}{}:
			default: // a change signal is already pending; coalesce
			}
		}
	}()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	return ch, conn.Close, nil
}

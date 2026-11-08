package clip

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Live integration check: with a real X server, taking CLIPBOARD ownership
// must produce an XFixes SelectionNotifyEvent. Skips on headless machines.
func TestX11NotifierReceivesSelectionEvent(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("DISPLAY not set; no X11 session")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, stop, err := startX11Notifier(ctx)
	if err != nil {
		t.Fatalf("startX11Notifier: %v", err)
	}
	defer stop()

	time.Sleep(200 * time.Millisecond) // let the subscription land

	// No CombinedOutput here: the selection-owning child xclip forks inherits
	// its stdio and would hold output pipes open forever (same wedge
	// backend_linux.go's runStdin documents).
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Stdin = strings.NewReader("x11-smoke-payload")
	if err := cmd.Run(); err != nil {
		t.Fatalf("xclip write failed: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no selection event within 3s of taking clipboard ownership")
	}
}

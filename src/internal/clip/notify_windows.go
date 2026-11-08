//go:build windows

package clip

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Windows clipboard-change notifications. A message-only window registered
// with AddClipboardFormatListener receives WM_CLIPBOARDUPDATE whenever any
// application changes the clipboard — the same event-driven mechanism the
// system clipboard history service uses. This replaces interval polling:
// zero wakeups while the clipboard is idle, one read per actual change.

const (
	wmQuit            = 0x0012
	wmClipboardUpdate = 0x031D
	hwndMessage       = ^uintptr(2) // HWND_MESSAGE: parent for message-only windows
)

var (
	user32 = syscall.NewLazyDLL("user32")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procUnregisterClassW              = user32.NewProc("UnregisterClassW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procPostThreadMessageW            = user32.NewProc("PostThreadMessageW")

	kernel32 = syscall.NewLazyDLL("kernel32")

	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
)

type point struct{ x, y int32 }

// msg mirrors the win32 MSG structure; Go's natural field alignment matches
// the C layout on both amd64 and 386.
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// wndClassEx mirrors the win32 WNDCLASSEX structure.
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
	hIconSm       uintptr
}

var (
	listenerMu sync.Mutex
	// listenerChans maps a notifier window handle to its notification
	// channel so the shared window procedure can route WM_CLIPBOARDUPDATE.
	listenerChans = make(map[uintptr]chan struct{})
)

// clipWndProc is the window procedure shared by all notifier windows. It must
// be created exactly once per process.
var clipWndProc = syscall.NewCallback(func(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmClipboardUpdate {
		listenerMu.Lock()
		ch := listenerChans[hwnd]
		listenerMu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default: // a change signal is already pending; coalesce
			}
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
})

var notifierSeq atomic.Uint64

type clipboardNotifier struct {
	notify chan struct{}

	// Written by pump before the ready handshake; read afterwards.
	className *uint16
	hInstance uintptr
	hwnd      uintptr
	threadID  uintptr

	stopOnce sync.Once
}

// startClipboardNotifier creates a hidden listener window and pumps its
// messages until stop is called or ctx is cancelled. On success the returned
// channel receives one buffered signal per clipboard change and is never
// closed; the pump goroutine exits after teardown.
func startClipboardNotifier(ctx context.Context) (<-chan struct{}, func(), error) {
	n := &clipboardNotifier{notify: make(chan struct{}, 1)}

	ready := make(chan error, 1)
	go n.pump(ready)

	// Wait for the window to exist (or startup to fail) so events can flow
	// as soon as we return, and so stop knows the pump thread id.
	if err := <-ready; err != nil {
		return n.notify, func() {}, err
	}

	go func() {
		<-ctx.Done()
		n.requestStop()
	}()

	return n.notify, n.requestStop, nil
}

func (n *clipboardNotifier) requestStop() {
	n.stopOnce.Do(func() {
		procPostThreadMessageW.Call(n.threadID, wmQuit, 0, 0)
	})
}

func (n *clipboardNotifier) pump(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, err := syscall.UTF16PtrFromString(
		fmt.Sprintf("clipshare-clip-notify-%d-%d", os.Getpid(), notifierSeq.Add(1)))
	if err != nil {
		ready <- err
		return
	}
	hInstance, _, _ := procGetModuleHandleW.Call(0)

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   clipWndProc,
		hInstance:     hInstance,
		lpszClassName: uintptr(unsafe.Pointer(className)),
	}
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		ready <- errors.New("register clipboard listener window class failed")
		return
	}
	n.className = className
	n.hInstance = hInstance

	hwnd, _, _ := procCreateWindowExW.Call(
		0,                                  // dwExStyle
		uintptr(unsafe.Pointer(className)), // lpClassName
		0,                                  // lpWindowName
		0,                                  // dwStyle
		0, 0, 0, 0,                         // X, Y, width, height
		hwndMessage,                        // hWndParent: message-only window
		0,                                  // hMenu
		hInstance,
		0, // lpParam
	)
	if hwnd == 0 {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInstance)
		ready <- errors.New("create clipboard listener window failed")
		return
	}
	n.hwnd = hwnd

	// Publish the channel before registering the listener so no
	// WM_CLIPBOARDUPDATE can arrive for an unmapped window.
	listenerMu.Lock()
	listenerChans[hwnd] = n.notify
	listenerMu.Unlock()

	if r, _, _ := procAddClipboardFormatListener.Call(hwnd); r == 0 {
		listenerMu.Lock()
		delete(listenerChans, hwnd)
		listenerMu.Unlock()
		procDestroyWindow.Call(hwnd)
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hInstance)
		ready <- errors.New("AddClipboardFormatListener failed")
		return
	}

	n.threadID, _, _ = procGetCurrentThreadId.Call()
	ready <- nil

	// Pump messages until WM_QUIT (or pump error). Clipboard notifications
	// arrive directly in clipWndProc; DispatchMessage keeps other window
	// traffic flowing.
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0: WM_QUIT, -1: error
			break
		}
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	// The window belongs to this thread; teardown must run here too.
	procRemoveClipboardFormatListener.Call(n.hwnd)
	listenerMu.Lock()
	delete(listenerChans, n.hwnd)
	listenerMu.Unlock()
	procDestroyWindow.Call(n.hwnd)
	procUnregisterClassW.Call(uintptr(unsafe.Pointer(n.className)), n.hInstance)
}

//go:build windows

package tray

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmNull        = 0x0000
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmLButtonDbl  = 0x0203
	nimAdd        = 0x00000000
	nimDelete     = 0x00000002
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	mfString      = 0x00000000
	tpmRightAlign = 0x0008
	tpmBottom     = 0x0020
	tpmReturnCmd  = 0x0100
	tpmNoNotify   = 0x0080
	trayMsg       = 0x0400 + 20
	idSettings    = 1
	idClose       = 2
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procShellNotify         = shell32.NewProc("Shell_NotifyIconW")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")

	trayProc = syscall.NewCallback(wndProc)
)

type notifyIconData struct {
	Size            uint32
	Wnd             windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type point struct {
	X, Y int32
}

type winMsg struct {
	HWND    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var (
	trayReady  atomic.Bool
	trayHWND   windows.Handle
	trayIcon   windows.Handle
	labelMu    sync.Mutex
	labelCur   Labels
	handlerCur Handlers
	className  = windows.StringToUTF16Ptr("NetPulsarTray")
)

// Start shows the notification-area icon. The caller returns after the icon is added.
func Start(icon []byte, tip string, labels Labels, h Handlers) error {
	hicon, err := hiconFromPNG(icon, 32)
	if err != nil {
		return err
	}
	ready := make(chan error, 1)
	go messageLoop(hicon, tip, labels, h, ready)
	return <-ready
}

// SetLabels updates the right-click menu the next time it opens.
func SetLabels(labels Labels) {
	labelMu.Lock()
	labelCur = labels
	labelMu.Unlock()
}

// Stop removes the icon.
func Stop() {
	if trayHWND != 0 {
		procPostMessageW.Call(uintptr(trayHWND), wmClose, 0, 0)
	}
}

// Enabled reports whether the icon was added.
func Enabled() bool { return trayReady.Load() }

func messageLoop(hicon windows.Handle, tip string, labels Labels, h Handlers, ready chan<- error) {
	labelMu.Lock()
	labelCur = labels
	handlerCur = h
	labelMu.Unlock()
	trayIcon = hicon

	var wc wndClassEx
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = trayProc
	mod, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	if mod == 0 {
		ready <- err
		return
	}
	wc.Instance = windows.Handle(mod)
	wc.ClassName = className
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		ready <- err
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, uintptr(wc.Instance), 0)
	if hwnd == 0 {
		ready <- err
		return
	}
	trayHWND = windows.Handle(hwnd)
	if err := notify(nimAdd, hicon, tip); err != nil {
		ready <- err
		return
	}
	trayReady.Store(true)
	ready <- nil
	go promoteIcon()

	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func notify(action uint32, hicon windows.Handle, tip string) error {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = trayHWND
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = trayMsg
	nid.Icon = hicon
	copy(nid.Tip[:], windows.StringToUTF16(tip))
	r, _, err := procShellNotify.Call(uintptr(action), uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return err
		}
		return windows.ERROR_INVALID_HANDLE
	}
	return nil
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case trayMsg:
		switch lParam {
		case wmLButtonUp, wmLButtonDbl:
			call(handlerCur.Show)
		case wmRButtonUp:
			showMenu(windows.Handle(hwnd))
		}
		return 0
	case wmClose:
		removeIcon()
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

func showMenu(hwnd windows.Handle) {
	labelMu.Lock()
	labels := labelCur
	h := handlerCur
	labelMu.Unlock()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendItem(menu, idSettings, labels.Settings)
	appendItem(menu, idClose, labels.Close)
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightAlign|tpmBottom|tpmReturnCmd|tpmNoNotify, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
	switch cmd {
	case idSettings:
		call(h.Settings)
	case idClose:
		call(h.Close)
	}
}

func appendItem(menu uintptr, id uintptr, text string) {
	if text == "" {
		text = " "
	}
	p, _ := windows.UTF16PtrFromString(text)
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(p)))
}

func removeIcon() {
	if trayHWND == 0 {
		return
	}
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = trayHWND
	nid.ID = 1
	procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	trayReady.Store(false)
}

// promoteIcon asks Windows 11 to keep the icon on the visible notification area.
// New icons otherwise land in the overflow chevron.
func promoteIcon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		if markPromoted(exe) || time.Now().After(deadline) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func markPromoted(exe string) bool {
	root, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\NotifyIconSettings`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return false
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return false
	}
	found := false
	for _, name := range names {
		sub, err := registry.OpenKey(root, name, registry.QUERY_VALUE|registry.SET_VALUE)
		if err != nil {
			continue
		}
		path, _, err := sub.GetStringValue("ExecutablePath")
		if err == nil && strings.EqualFold(path, exe) {
			_ = sub.SetDWordValue("IsPromoted", 1)
			found = true
		}
		sub.Close()
	}
	return found
}

//go:build windows

package desktop

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	shcore = windows.NewLazySystemDLL("shcore.dll")

	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procWindowFromPoint     = user32.NewProc("WindowFromPoint")
	procGetAncestor         = user32.NewProc("GetAncestor")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetDpiForMonitor    = shcore.NewProc("GetDpiForMonitor")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsZoomed            = user32.NewProc("IsZoomed")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procShowWindow          = user32.NewProc("ShowWindow")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procDwmGetWindowAttr    = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")

	enumCB    = syscall.NewCallback(monitorEnum)
	enumWinCB = syscall.NewCallback(fullscreenEnum)
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

type point struct {
	X, Y int32
}

const monitorPrimary = 1

var enumTmp []Monitor

func Query(title string) Info {
	info := Info{Scale: 1}
	var pt point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r != 0 {
		info.CursorX = int(pt.X)
		info.CursorY = int(pt.Y)
		info.CursorOK = true
	}
	if ours := findWindow(title); ours != 0 && info.CursorOK {
		info.KnowsHit = true
		info.CursorOnWidget = cursorOnWindow(pt, ours)
	}
	if k, _, _ := procGetAsyncKeyState.Call(1); k&0x8000 != 0 {
		info.MouseDown = true
	}
	info.Monitors = enumMonitors()
	if title != "" {
		if rect, ok := windowByTitle(title); ok {
			info.Window = rect
			info.HasWindow = true
		}
	}
	if idx := IndexAt(info.Monitors, info.Window.X, info.Window.Y, info.Window.W, info.Window.H); idx >= 0 && info.Monitors[idx].Scale > 0 {
		info.Scale = info.Monitors[idx].Scale
	}
	info.Fullscreen = anyFullscreen(info.Monitors, findWindow(title))
	return info
}

func enumMonitors() []Monitor {
	enumTmp = enumTmp[:0]
	procEnumDisplayMonitors.Call(0, 0, enumCB, 0)
	out := make([]Monitor, len(enumTmp))
	copy(out, enumTmp)
	return out
}

func monitorEnum(hmon, _, _, _ uintptr) uintptr {
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	var dpiX, dpiY uint32
	hr, _, _ := procGetDpiForMonitor.Call(hmon, 0, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	scale := 1.0
	if hr == 0 && dpiX >= 96 {
		scale = float64(dpiX) / 96
	}
	enumTmp = append(enumTmp, Monitor{
		Work: Rect{
			X: int(mi.RcWork.Left),
			Y: int(mi.RcWork.Top),
			W: int(mi.RcWork.Right - mi.RcWork.Left),
			H: int(mi.RcWork.Bottom - mi.RcWork.Top),
		},
		Bounds: Rect{
			X: int(mi.RcMonitor.Left),
			Y: int(mi.RcMonitor.Top),
			W: int(mi.RcMonitor.Right - mi.RcMonitor.Left),
			H: int(mi.RcMonitor.Bottom - mi.RcMonitor.Top),
		},
		Scale:   scale,
		Primary: mi.DwFlags&monitorPrimary != 0,
	})
	return 1
}

var (
	fsMonitors []Monitor
	fsSkip     uintptr
	fsFound    bool
	fsFore     uintptr
)

const (
	wsCaption              = 0x00C00000
	swpNoSize              = 0x0001
	swpNoMove              = 0x0002
	swpNoActivate          = 0x0010
	swpFrameChange         = 0x0020
	swpNoZOrder            = 0x0004
	swpShowWindow          = 0x0040
	hwndTop        uintptr = 0
)

// Raise brings the widget above the window the user is working in without taking focus.
// topmost keeps it above ordinary windows afterwards.
func Raise(title string, topmost bool) {
	hwnd := findWindow(title)
	if hwnd == 0 {
		return
	}
	insert := hwndTop
	if topmost {
		insert = ^uintptr(0)
	}
	procSetWindowPos.Call(hwnd, insert, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
}

func anyFullscreen(monitors []Monitor, skip uintptr) bool {
	if len(monitors) == 0 {
		return false
	}
	fsMonitors = monitors
	fsSkip = skip
	fsFound = false
	fsFore, _, _ = procGetForegroundWindow.Call()
	procEnumWindows.Call(enumWinCB, 0)
	return fsFound
}

func fullscreenEnum(hwnd, _ uintptr) uintptr {
	if fsFound || hwnd == 0 || hwnd == fsSkip {
		return 1
	}
	if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
		return 1
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return 1
	}
	if cloaked(hwnd) || shellClass(hwnd) {
		return 1
	}
	if fsFore != 0 && hwnd != fsFore {
		return 1
	}
	var r winRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return 1
	}
	win := Rect{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
	for _, m := range fsMonitors {
		if !Covers(win, m.Bounds) {
			continue
		}
		if ordinaryMaximized(hwnd) {
			return 1
		}
		fsFound = true
		return 0
	}
	return 1
}

func ordinaryMaximized(hwnd uintptr) bool {
	zoomed, _, _ := procIsZoomed.Call(hwnd)
	if zoomed == 0 {
		return false
	}
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, ^uintptr(15))
	return style&wsCaption != 0
}

func cloaked(hwnd uintptr) bool {
	var cloaked uint32
	r, _, _ := procDwmGetWindowAttr.Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	return r == 0 && cloaked != 0
}

func shellClass(hwnd uintptr) bool {
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return false
	}
	switch windows.UTF16ToString(buf[:n]) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return true
	default:
		return false
	}
}

// HideFromTaskbar keeps the widget out of the taskbar. It belongs in the notification area.
func HideFromTaskbar(title string) {
	hwnd := findWindow(title)
	if hwnd == 0 {
		return
	}
	const (
		gwlExStyle     = uintptr(^uint(19))
		wsExToolWindow = 0x00000080
		wsExAppWindow  = 0x00040000
	)
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	want := (style | wsExToolWindow) &^ wsExAppWindow
	if style != want {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible != 0 {
			procShowWindow.Call(hwnd, 0)
		}
		procSetWindowLongPtrW.Call(hwnd, gwlExStyle, want)
		procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChange|swpNoActivate)
		if visible != 0 {
			procShowWindow.Call(hwnd, 8)
		}
	}
	deleteTaskbarTab(hwnd)
}

func deleteTaskbarTab(hwnd uintptr) {
	windows.NewLazySystemDLL("ole32.dll").NewProc("CoInitializeEx").Call(0, 2)
	clsid := windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iid := windows.GUID{Data1: 0x56FDF341, Data2: 0xFD6D, Data3: 0x11D0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	var obj uintptr
	hr, _, _ := windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&clsid)),
		0,
		1,
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if hr != 0 || obj == 0 {
		return
	}
	vtbl := *(**[8]uintptr)(unsafe.Pointer(obj))
	syscall.SyscallN(vtbl[3], obj)
	syscall.SyscallN(vtbl[5], obj, hwnd)
	syscall.SyscallN(vtbl[2], obj)
}

func findWindow(title string) uintptr {
	if title == "" {
		return 0
	}
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	class, _ := windows.UTF16PtrFromString("wailsWindow")
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(t)))
	if hwnd == 0 {
		hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	}
	return hwnd
}

// cursorOnWindow reports whether the pointer is on our top-level window, not a window in front of it.
func cursorOnWindow(pt point, ours uintptr) bool {
	packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
	hwnd, _, _ := procWindowFromPoint.Call(packed)
	if hwnd == 0 {
		return false
	}
	root, _, _ := procGetAncestor.Call(hwnd, 2)
	if root == 0 {
		root = hwnd
	}
	return root == ours
}

func windowByTitle(title string) (Rect, bool) {
	hwnd := findWindow(title)
	if hwnd == 0 {
		return Rect{}, false
	}
	var r winRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return Rect{}, false
	}
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return Rect{}, false
	}
	return Rect{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}, true
}

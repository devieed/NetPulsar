//go:build linux

package desktop

import (
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
)

var linuxOnce sync.Mutex
var linuxConn *xgb.Conn

// Raise is unused on Linux; the window manager owns stacking.
func Raise(title string, topmost bool) {
	_, _ = title, topmost
}

// HideFromTaskbar is unused on Linux.
func HideFromTaskbar(title string) { _ = title }

func Query(title string) Info {
	_ = title
	info := Info{Scale: 1}
	conn := linuxConnection()
	if conn == nil {
		return info
	}
	setup := xproto.Setup(conn)
	if setup == nil || len(setup.Roots) == 0 {
		return info
	}
	root := setup.DefaultScreen(conn).Root
	if reply, err := xproto.QueryPointer(conn, root).Reply(); err == nil && reply != nil {
		info.CursorX = int(reply.RootX)
		info.CursorY = int(reply.RootY)
		info.CursorOK = true
		info.MouseDown = reply.Mask&xproto.ButtonMask1 != 0
	}
	info.Monitors = linuxMonitors(conn, root, setup.DefaultScreen(conn))
	return info
}

func linuxConnection() *xgb.Conn {
	linuxOnce.Lock()
	defer linuxOnce.Unlock()
	if linuxConn != nil {
		return linuxConn
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return nil
	}
	if err := randr.Init(conn); err != nil {
		conn.Close()
		return nil
	}
	linuxConn = conn
	return linuxConn
}

func linuxMonitors(conn *xgb.Conn, root xproto.Window, screen *xproto.ScreenInfo) []Monitor {
	res, err := randr.GetScreenResourcesCurrent(conn, root).Reply()
	if err != nil || res == nil {
		if screen == nil {
			return nil
		}
		return []Monitor{{
			Work:    Rect{W: int(screen.WidthInPixels), H: int(screen.HeightInPixels)},
			Scale:   1,
			Primary: true,
		}}
	}
	out := make([]Monitor, 0, len(res.Outputs))
	for _, output := range res.Outputs {
		oi, err := randr.GetOutputInfo(conn, output, 0).Reply()
		if err != nil || oi == nil || oi.Connection != randr.ConnectionConnected || oi.Crtc == 0 {
			continue
		}
		ci, err := randr.GetCrtcInfo(conn, oi.Crtc, 0).Reply()
		if err != nil || ci == nil || ci.Width == 0 || ci.Height == 0 {
			continue
		}
		out = append(out, Monitor{
			Work: Rect{
				X: int(ci.X),
				Y: int(ci.Y),
				W: int(ci.Width),
				H: int(ci.Height),
			},
			Scale:   1,
			Primary: len(out) == 0,
		})
	}
	if len(out) == 0 && screen != nil {
		return []Monitor{{
			Work:    Rect{W: int(screen.WidthInPixels), H: int(screen.HeightInPixels)},
			Scale:   1,
			Primary: true,
		}}
	}
	return out
}

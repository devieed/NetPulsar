package desktop

// Rect uses the same absolute coordinates as the cursor. Origin is the top-left and Y grows downward.
type Rect struct {
	X, Y, W, H int
}

// Monitor is one screen. Work excludes the taskbar. Bounds is the full panel.
type Monitor struct {
	Work    Rect
	Bounds  Rect
	Scale   float64
	Primary bool
}

// Info is one snapshot of the desktop.
type Info struct {
	CursorX        int
	CursorY        int
	CursorOK       bool
	MouseDown      bool
	KnowsHit       bool
	CursorOnWidget bool
	Monitors       []Monitor
	Window         Rect
	HasWindow      bool
	Scale          float64
	Fullscreen     bool
}

func Primary(list []Monitor) Monitor {
	for _, m := range list {
		if m.Primary && m.Work.W > 0 && m.Work.H > 0 {
			return m
		}
	}
	for _, m := range list {
		if m.Work.W > 0 && m.Work.H > 0 {
			return m
		}
	}
	return Monitor{Work: Rect{W: 1920, H: 1080}, Scale: 1, Primary: true}
}

// IndexAt returns the monitor that contains the window center, or the nearest one when the center is off-screen.
func IndexAt(list []Monitor, x, y, w, h int) int {
	if len(list) == 0 {
		return -1
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	cx, cy := x+w/2, y+h/2
	best, bestD := 0, int(^uint(0)>>1)
	for i, m := range list {
		r := m.Work
		if cx >= r.X && cx < r.X+r.W && cy >= r.Y && cy < r.Y+r.H {
			return i
		}
		d := distToRect(cx, cy, r)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// Covers reports whether a window fills a whole monitor, including the taskbar.
func Covers(win, mon Rect) bool {
	const tol = 8
	if mon.W < 200 || mon.H < 200 || win.W < 200 || win.H < 200 {
		return false
	}
	return win.X <= mon.X+tol && win.Y <= mon.Y+tol &&
		win.X+win.W >= mon.X+mon.W-tol && win.Y+win.H >= mon.Y+mon.H-tol
}

func Contains(r Rect, x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

func Overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

func distToRect(x, y int, r Rect) int {
	dx, dy := 0, 0
	if x < r.X {
		dx = r.X - x
	} else if x > r.X+r.W {
		dx = x - (r.X + r.W)
	}
	if y < r.Y {
		dy = r.Y - y
	} else if y > r.Y+r.H {
		dy = y - (r.Y + r.H)
	}
	if dx > dy {
		return dx
	}
	return dy
}

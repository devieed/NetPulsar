package dock

import (
	"math"
	"time"

	"netpulsar/internal/desktop"
)

type Edge int

const (
	EdgeLeft Edge = iota
	EdgeRight
	EdgeTop
	EdgeBottom
)

const animDuration = 180 * time.Millisecond

// Input is one frame of desktop state. Window coordinates and the cursor share one absolute space.
type Input struct {
	Now            time.Time
	WinX, WinY     int
	WinW, WinH     int
	Monitors       []desktop.Monitor
	CursorX        int
	CursorY        int
	CursorOK       bool
	MouseDown      bool
	KnowsHit       bool
	CursorOnWidget bool
	Interacting    bool
	EdgeHide       bool
	AutoHide       bool
	AutoHideAfter  time.Duration
	EdgeDelay      time.Duration
	SnapPx         int
	PeekPx         int
	AllowHide      bool
	HasSaved       bool
	SavedX         int
	SavedY         int
	ForceShow      bool
	ForceCenter    bool
}

// Command is what the window should do this frame. Coordinates stay absolute.
type Command struct {
	SetPos  bool
	Instant bool
	SyncTop bool
	Raise   bool
	X, Y    int
	Save    bool
	SaveX   int
	SaveY   int
}

type State struct {
	Ready        bool
	HomeX, HomeY int
	Latched      []Edge
	Hidden       bool
	WasDown      bool
	LastInteract time.Time
	GraceUntil   time.Time
	Animating    bool
	AnimFromX    int
	AnimFromY    int
	AnimToX      int
	AnimToY      int
	AnimStart    time.Time
}

func Tick(st State, in Input) (State, Command) {
	if len(in.Monitors) == 0 || in.WinW < 20 || in.WinH < 20 {
		return st, Command{}
	}
	if in.EdgeDelay <= 0 {
		in.EdgeDelay = 700 * time.Millisecond
	}
	if in.AutoHideAfter <= 0 {
		in.AutoHideAfter = 6 * time.Second
	}
	mon := monitorOf(st, in)
	scale := mon.Scale
	if scale < 1 {
		scale = 1
	}
	snapPx := scalePx(in.SnapPx, scale)
	peek := scalePx(in.PeekPx, scale)
	if peek < 8 {
		peek = 8
	}

	if !st.Ready || in.ForceCenter {
		return placeInitial(st, in, mon, snapPx)
	}
	// A pressed button counts as a drag only when it starts on the widget.
	// A click in another application must not clear the docked edge.
	if in.MouseDown && !st.Hidden && (st.WasDown || cursorInside(st, in, mon, scale, peek)) {
		st.Animating = false
		st.WasDown = true
		st.LastInteract = in.Now
		st.HomeX, st.HomeY = in.WinX, in.WinY
		st.Latched = nil
		return st, Command{}
	}
	over := cursorInside(st, in, mon, scale, peek)
	if in.ForceShow || (st.Hidden && over) {
		st.Hidden = false
		st.Animating = false
		st.WasDown = false
		st.LastInteract = in.Now
		st.GraceUntil = in.Now.Add(2 * time.Second)
		cmd := move(st, st.HomeX, st.HomeY, in.WinX, in.WinY, in.Now, false)
		st = cmd.state
		cmd.out.SyncTop = true
		cmd.out.Raise = true
		return st, cmd.out
	}
	if st.WasDown {
		st.WasDown = false
		x, y, edges := Snap(in.WinX, in.WinY, in.WinW, in.WinH, mon.Work, snapPx)
		st.HomeX, st.HomeY = x, y
		st.Latched = edges
		st.Hidden = false
		st.LastInteract = in.Now
		cmd := move(st, x, y, in.WinX, in.WinY, in.Now, false)
		st = cmd.state
		cmd.out.Save = true
		cmd.out.SaveX, cmd.out.SaveY = st.HomeX, st.HomeY
		cmd.out.SyncTop = true
		return st, cmd.out
	}
	if st.Animating {
		out := stepAnim(&st, in.Now)
		if !st.Animating {
			out.SyncTop = true
		}
		return st, out
	}

	if !st.Hidden {
		nx, ny := pinEdges(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work, st.Latched)
		if nx != st.HomeX || ny != st.HomeY {
			st.HomeX, st.HomeY = nx, ny
			cmd := move(st, nx, ny, in.WinX, in.WinY, in.Now, true)
			st = cmd.state
			cmd.out.Save = true
			cmd.out.SaveX, cmd.out.SaveY = nx, ny
			return st, cmd.out
		}
	}

	if over {
		st.LastInteract = in.Now
		if st.Hidden {
			st.Hidden = false
			cmd := move(st, st.HomeX, st.HomeY, in.WinX, in.WinY, in.Now, false)
			st = cmd.state
			cmd.out.Raise = true
			return st, cmd.out
		}
		return st, Command{}
	}

	if st.Hidden || !in.AllowHide || !in.CursorOK || in.Now.Before(st.GraceUntil) {
		if st.Hidden {
			edges := st.Latched
			if len(edges) == 0 {
				edges = nearestEdges(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work)
			}
			hx, hy := HideTarget(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work, edges, peek)
			if abs(in.WinX-hx) > 2 || abs(in.WinY-hy) > 2 {
				cmd := move(st, hx, hy, in.WinX, in.WinY, in.Now, false)
				st = cmd.state
				return st, cmd.out
			}
		}
		return st, Command{}
	}

	idle := in.Now.Sub(st.LastInteract)
	if !st.Hidden && in.EdgeHide && len(st.Latched) == 0 {
		st.Latched = flushEdges(in.WinX, in.WinY, in.WinW, in.WinH, mon.Work, snapPx)
	}
	var edges []Edge
	hide := false
	if in.EdgeHide && len(st.Latched) > 0 && idle >= in.EdgeDelay {
		hide = true
		edges = st.Latched
	} else if in.AutoHide && idle >= in.AutoHideAfter {
		hide = true
		if len(st.Latched) > 0 {
			edges = st.Latched
		} else {
			edges = nearestEdges(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work)
		}
	}
	if !hide {
		return st, Command{}
	}
	st.Hidden = true
	hx, hy := HideTarget(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work, edges, peek)
	cmd := move(st, hx, hy, in.WinX, in.WinY, in.Now, false)
	return cmd.state, cmd.out
}

type moveResult struct {
	state State
	out   Command
}

func placeInitial(st State, in Input, mon desktop.Monitor, snapPx int) (State, Command) {
	st.Ready = true
	st.Hidden = false
	st.Animating = false
	st.LastInteract = in.Now
	st.GraceUntil = in.Now.Add(1500 * time.Millisecond)
	if in.HasSaved && !in.ForceCenter && onAny(in.Monitors, in.SavedX, in.SavedY, in.WinW, in.WinH) {
		st.HomeX, st.HomeY = in.SavedX, in.SavedY
	} else {
		p := desktop.Primary(in.Monitors)
		st.HomeX = p.Work.X + (p.Work.W-in.WinW)/2
		st.HomeY = p.Work.Y + (p.Work.H-in.WinH)/4
		if st.HomeY < p.Work.Y {
			st.HomeY = p.Work.Y + scalePx(24, p.Scale)
		}
	}
	homeMon := mon
	if idx := desktop.IndexAt(in.Monitors, st.HomeX, st.HomeY, in.WinW, in.WinH); idx >= 0 {
		homeMon = in.Monitors[idx]
	}
	st.Latched = flushEdges(st.HomeX, st.HomeY, in.WinW, in.WinH, homeMon.Work, scalePx(6, homeMon.Scale))
	_ = snapPx
	cmd := move(st, st.HomeX, st.HomeY, in.WinX, in.WinY, in.Now, true)
	st = cmd.state
	cmd.out.Save = true
	cmd.out.SaveX, cmd.out.SaveY = st.HomeX, st.HomeY
	cmd.out.SyncTop = true
	return st, cmd.out
}

func move(st State, toX, toY, fromX, fromY int, now time.Time, instant bool) moveResult {
	if abs(toX-fromX) <= 2 && abs(toY-fromY) <= 2 {
		st.Animating = false
		return moveResult{state: st}
	}
	if instant {
		st.Animating = false
		return moveResult{state: st, out: Command{SetPos: true, Instant: true, X: toX, Y: toY}}
	}
	if st.Animating && st.AnimToX == toX && st.AnimToY == toY {
		return moveResult{state: st, out: stepAnim(&st, now)}
	}
	st.Animating = true
	st.AnimFromX, st.AnimFromY = fromX, fromY
	st.AnimToX, st.AnimToY = toX, toY
	st.AnimStart = now
	return moveResult{state: st, out: stepAnim(&st, now)}
}

func stepAnim(st *State, now time.Time) Command {
	t := float64(now.Sub(st.AnimStart)) / float64(animDuration)
	if t >= 1 {
		st.Animating = false
		return Command{SetPos: true, X: st.AnimToX, Y: st.AnimToY}
	}
	if t < 0 {
		t = 0
	}
	e := t * t * (3 - 2*t)
	x := st.AnimFromX + int(math.Round(float64(st.AnimToX-st.AnimFromX)*e))
	y := st.AnimFromY + int(math.Round(float64(st.AnimToY-st.AnimFromY)*e))
	return Command{SetPos: true, X: x, Y: y}
}

func monitorOf(st State, in Input) desktop.Monitor {
	x, y := in.WinX, in.WinY
	if st.Ready {
		x, y = st.HomeX, st.HomeY
	}
	idx := desktop.IndexAt(in.Monitors, x, y, in.WinW, in.WinH)
	if idx < 0 {
		return desktop.Primary(in.Monitors)
	}
	return in.Monitors[idx]
}

func Snap(x, y, w, h int, mon desktop.Rect, snapPx int) (int, int, []Edge) {
	if snapPx < 1 {
		snapPx = 1
	}
	var edges []Edge
	if w > 0 && w < mon.W {
		if x-mon.X <= snapPx {
			x = mon.X
			edges = append(edges, EdgeLeft)
		} else if mon.X+mon.W-(x+w) <= snapPx {
			x = mon.X + mon.W - w
			edges = append(edges, EdgeRight)
		}
	}
	if h > 0 && h < mon.H {
		if y-mon.Y <= snapPx {
			y = mon.Y
			edges = append(edges, EdgeTop)
		} else if mon.Y+mon.H-(y+h) <= snapPx {
			y = mon.Y + mon.H - h
			edges = append(edges, EdgeBottom)
		}
	}
	return x, y, edges
}

func HideTarget(x, y, w, h int, mon desktop.Rect, edges []Edge, peek int) (int, int) {
	if peek < 1 {
		peek = 1
	}
	if peek > w/2 {
		peek = w / 2
	}
	if h > 0 && peek > h/2 {
		// Width is capped above. Height stays as given so a short window can still leave a sliver.
	}
	for _, e := range edges {
		switch e {
		case EdgeLeft:
			x = mon.X - w + peek
		case EdgeRight:
			x = mon.X + mon.W - peek
		case EdgeTop:
			y = mon.Y - h + peek
		case EdgeBottom:
			y = mon.Y + mon.H - peek
		}
	}
	return x, y
}

func flushEdges(x, y, w, h int, mon desktop.Rect, tol int) []Edge {
	if tol < 0 {
		tol = 0
	}
	var edges []Edge
	if abs(x-mon.X) <= tol {
		edges = append(edges, EdgeLeft)
	}
	if abs((mon.X+mon.W)-(x+w)) <= tol {
		edges = append(edges, EdgeRight)
	}
	if abs(y-mon.Y) <= tol {
		edges = append(edges, EdgeTop)
	}
	if abs((mon.Y+mon.H)-(y+h)) <= tol {
		edges = append(edges, EdgeBottom)
	}
	return edges
}

func nearestEdges(x, y, w, h int, mon desktop.Rect) []Edge {
	left := x - mon.X
	right := mon.X + mon.W - (x + w)
	top := y - mon.Y
	bottom := mon.Y + mon.H - (y + h)
	if smaller(left, right) <= smaller(top, bottom) {
		if left <= right {
			return []Edge{EdgeLeft}
		}
		return []Edge{EdgeRight}
	}
	if top <= bottom {
		return []Edge{EdgeTop}
	}
	return []Edge{EdgeBottom}
}

func pinEdges(x, y, w, h int, mon desktop.Rect, edges []Edge) (int, int) {
	for _, e := range edges {
		switch e {
		case EdgeLeft:
			x = mon.X
		case EdgeRight:
			x = mon.X + mon.W - w
		case EdgeTop:
			y = mon.Y
		case EdgeBottom:
			y = mon.Y + mon.H - h
		}
	}
	return x, y
}

func onAny(list []desktop.Monitor, x, y, w, h int) bool {
	win := rect(x, y, w, h)
	for _, m := range list {
		if desktop.Overlaps(win, m.Work) {
			return true
		}
	}
	return false
}

func cursorInside(st State, in Input, mon desktop.Monitor, scale float64, peek int) bool {
	if in.Interacting {
		return true
	}
	if !in.CursorOK {
		return false
	}
	if st.Hidden {
		area := mon.Bounds
		if area.W < 1 || area.H < 1 {
			area = mon.Work
		}
		if !desktop.Contains(area, in.CursorX, in.CursorY) && !desktop.Contains(mon.Work, in.CursorX, in.CursorY) {
			return false
		}
		edges := st.Latched
		if len(edges) == 0 {
			edges = nearestEdges(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work)
		}
		zone := peekZone(st.HomeX, st.HomeY, in.WinW, in.WinH, mon.Work, edges, peek)
		zone = growInward(zone, edges, scalePx(28, scale))
		return desktop.Contains(zone, in.CursorX, in.CursorY)
	}
	if in.KnowsHit && !in.CursorOnWidget {
		return false
	}
	if !desktop.Contains(mon.Work, in.CursorX, in.CursorY) {
		return false
	}
	zone := expand(rect(in.WinX, in.WinY, in.WinW, in.WinH), scalePx(8, scale))
	return desktop.Contains(zone, in.CursorX, in.CursorY)
}

// growInward widens the hidden hot zone toward the desktop so a pointer aimed at the edge still counts when another window covers the sliver.
func growInward(zone desktop.Rect, edges []Edge, extra int) desktop.Rect {
	if extra < 1 {
		return zone
	}
	left, right, top, bottom := false, false, false, false
	for _, e := range edges {
		switch e {
		case EdgeLeft:
			left = true
		case EdgeRight:
			right = true
		case EdgeTop:
			top = true
		case EdgeBottom:
			bottom = true
		}
	}
	if left && !right {
		zone.W += extra
	}
	if right && !left {
		zone.X -= extra
		zone.W += extra
	}
	if top && !bottom {
		zone.H += extra
	}
	if bottom && !top {
		zone.Y -= extra
		zone.H += extra
	}
	return zone
}

// peekZone is only the strip that stays on screen after docking, not the part of the window that sits off-screen.
func peekZone(homeX, homeY, winW, winH int, mon desktop.Rect, edges []Edge, peek int) desktop.Rect {
	if peek < 1 {
		peek = 1
	}
	x, y, w, h := homeX, homeY, winW, winH
	for _, e := range edges {
		switch e {
		case EdgeLeft:
			x, w = mon.X, peek
		case EdgeRight:
			x, w = mon.X+mon.W-peek, peek
		case EdgeTop:
			y, h = mon.Y, peek
		case EdgeBottom:
			y, h = mon.Y+mon.H-peek, peek
		}
	}
	return rect(x, y, w, h)
}

func rect(x, y, w, h int) desktop.Rect { return desktop.Rect{X: x, Y: y, W: w, H: h} }

func expand(r desktop.Rect, n int) desktop.Rect {
	return desktop.Rect{X: r.X - n, Y: r.Y - n, W: r.W + n*2, H: r.H + n*2}
}

func scalePx(n int, scale float64) int {
	if n < 0 {
		n = 0
	}
	if scale < 1 {
		scale = 1
	}
	return int(math.Round(float64(n) * scale))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func smaller(a, b int) int {
	if a < b {
		return a
	}
	return b
}

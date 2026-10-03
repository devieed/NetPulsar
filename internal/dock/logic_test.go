package dock

import (
	"testing"
	"time"

	"netpulsar/internal/desktop"
)

func TestSnapAndHide(t *testing.T) {
	mon := desktop.Rect{X: 0, Y: 0, W: 1000, H: 800}
	x, y, edges := Snap(12, 40, 200, 120, mon, 36)
	if x != 0 || y != 40 || len(edges) != 1 || edges[0] != EdgeLeft {
		t.Fatalf("snap got %d,%d %v", x, y, edges)
	}
	hx, hy := HideTarget(x, y, 200, 120, mon, edges, 18)
	if hx != -182 || hy != 40 {
		t.Fatalf("hide got %d,%d", hx, hy)
	}
}

func TestInitialPlaceCentersAndSaves(t *testing.T) {
	in := Input{
		Now:      time.Unix(100, 0),
		WinW:     200,
		WinH:     100,
		Monitors: []desktop.Monitor{{Work: desktop.Rect{W: 1000, H: 800}, Scale: 1, Primary: true}},
		CursorOK: true,
	}
	st, cmd := Tick(State{}, in)
	if !st.Ready || !cmd.Save || !cmd.Instant {
		t.Fatalf("state=%+v cmd=%+v", st, cmd)
	}
	if st.HomeX != 400 || st.HomeY != 175 {
		t.Fatalf("home %d,%d", st.HomeX, st.HomeY)
	}
}

func TestEdgeHideStartsAfterIdle(t *testing.T) {
	now := time.Unix(200, 0)
	st := State{
		Ready:        true,
		HomeX:        0,
		HomeY:        40,
		Latched:      []Edge{EdgeLeft},
		LastInteract: now.Add(-2 * time.Second),
	}
	in := Input{
		Now:       now,
		WinX:      0,
		WinY:      40,
		WinW:      200,
		WinH:      120,
		Monitors:  []desktop.Monitor{{Work: desktop.Rect{W: 1000, H: 800}, Scale: 1, Primary: true}},
		CursorX:   900,
		CursorY:   400,
		CursorOK:  true,
		EdgeHide:  true,
		AllowHide: true,
		SnapPx:    36,
		PeekPx:    18,
	}
	next, _ := Tick(st, in)
	if !next.Hidden || next.AnimToX != -182 {
		t.Fatalf("hidden=%v to=%d", next.Hidden, next.AnimToX)
	}
}

func TestHiddenStaysUntilCursorHitsPeek(t *testing.T) {
	now := time.Unix(400, 0)
	mon := []desktop.Monitor{{Work: desktop.Rect{W: 1000, H: 800}, Scale: 1, Primary: true}}
	st := State{
		Ready:   true,
		Hidden:  true,
		HomeX:   0,
		HomeY:   40,
		Latched: []Edge{EdgeLeft},
	}
	away := Input{
		Now: now, WinX: -182, WinY: 40, WinW: 200, WinH: 120,
		Monitors: mon, CursorX: 500, CursorY: 400, CursorOK: true,
		MouseDown: true, EdgeHide: true, AllowHide: true, PeekPx: 18,
	}
	next, cmd := Tick(st, away)
	if !next.Hidden || cmd.X > 0 {
		t.Fatalf("click away popped out: hidden=%v cmd=%+v", next.Hidden, cmd)
	}
	onPeek := away
	onPeek.MouseDown = false
	onPeek.CursorX = 6
	onPeek.CursorY = 80
	next, cmd = Tick(st, onPeek)
	if next.Hidden || next.AnimToX != 0 || !cmd.Raise {
		t.Fatalf("peek should reveal: hidden=%v to=%d cmd=%+v", next.Hidden, next.AnimToX, cmd)
	}
	near := away
	near.MouseDown = false
	near.CursorX = 40
	near.CursorY = 80
	next, _ = Tick(st, near)
	if next.Hidden {
		t.Fatalf("hot zone should include x=40")
	}
	far := near
	far.CursorX = 80
	next, _ = Tick(st, far)
	if !next.Hidden {
		t.Fatalf("x=80 is not the edge")
	}
}

func TestDragDoesNotMoveProgrammatically(t *testing.T) {
	now := time.Unix(300, 0)
	st := State{Ready: true, HomeX: 10, HomeY: 10, LastInteract: now}
	in := Input{
		Now:       now,
		WinX:      80,
		WinY:      90,
		WinW:      200,
		WinH:      120,
		Monitors:  []desktop.Monitor{{Work: desktop.Rect{W: 1000, H: 800}, Scale: 1, Primary: true}},
		CursorX:   100,
		CursorY:   100,
		MouseDown: true,
		CursorOK:  true,
	}
	next, cmd := Tick(st, in)
	if cmd.SetPos || next.HomeX != 80 || next.HomeY != 90 {
		t.Fatalf("state=%+v cmd=%+v", next, cmd)
	}
}

func TestClickInOtherAppStillHides(t *testing.T) {
	now := time.Unix(500, 0)
	st := State{
		Ready:        true,
		HomeX:        0,
		HomeY:        40,
		Latched:      []Edge{EdgeLeft},
		LastInteract: now.Add(-2 * time.Second),
	}
	in := Input{
		Now: now, WinX: 0, WinY: 40, WinW: 200, WinH: 120,
		Monitors:       []desktop.Monitor{{Work: desktop.Rect{W: 1000, H: 800}, Scale: 1, Primary: true}},
		CursorX:        20,
		CursorY:        60,
		CursorOK:       true,
		KnowsHit:       true,
		CursorOnWidget: false,
		MouseDown:      true,
		EdgeHide:       true,
		AllowHide:      true,
		SnapPx:         36,
		PeekPx:         18,
	}
	next, _ := Tick(st, in)
	if !next.Hidden || len(next.Latched) == 0 {
		t.Fatalf("other app kept the widget open: %+v", next)
	}
}

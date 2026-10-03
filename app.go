package main

import (
	"context"
	"errors"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"netpulsar/internal/autostart"
	"netpulsar/internal/desktop"
	"netpulsar/internal/dock"
	"netpulsar/internal/locale"
	"netpulsar/internal/metrics"
	"netpulsar/internal/settings"
	"netpulsar/internal/skin"
	"netpulsar/internal/tray"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the backend bound to the skin page.
type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
	settings    *settings.Store
	skins       *skin.Catalog
	locales     *locale.Catalog
	col         *metrics.Collector
	lastMu      sync.Mutex
	last        metrics.Snapshot
	hasLast     bool
	wake        chan struct{}
	ready       atomic.Bool
	interact    atomic.Bool
	forceShow   atomic.Bool
	forceCenter atomic.Bool
	dockHidden  atomic.Bool
	displayed   atomic.Bool
	debug       bool
	started     time.Time
	hasLastAbs  bool
	lastAbsX    int
	lastAbsY    int
	topKnown    bool
	appliedTop  bool
	logo        []byte
	taskbarAt   time.Time
}

func newApp(store *settings.Store, catalog *skin.Catalog, locales *locale.Catalog) *App {
	return &App{
		settings: store,
		skins:    catalog,
		locales:  locales,
		col:      metrics.New(),
		wake:     make(chan struct{}, 1),
		debug:    os.Getenv("NETPULSAR_DEBUG") == "1",
		started:  time.Now(),
	}
}

func (a *App) startup(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	a.ctx = ctx
	a.cancel = cancel
	a.syncTop(false)
	_ = autostart.Apply(a.settings.Get().LaunchAtLogin)
	if err := tray.Start(a.logo, "NetPulsar", a.trayLabels(), tray.Handlers{
		Show:     a.Reveal,
		Settings: a.openFromTray,
		Close:    a.Quit,
	}); err == nil {
		desktop.HideFromTaskbar(windowTitle)
	}
	go a.metricsLoop(runCtx)
	go a.dockLoop(runCtx)
}

func (a *App) shutdown(context.Context) {
	tray.Stop()
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) openFromTray() {
	a.Reveal()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "open-settings")
	}
}

func (a *App) trayLabels() tray.Labels {
	text := a.locales.Merged(a.settings.Get().Locale)
	settingsLabel := text["action.settings"]
	if settingsLabel == "" {
		settingsLabel = "Settings"
	}
	closeLabel := text["action.close"]
	if closeLabel == "" {
		closeLabel = "Close"
	}
	return tray.Labels{Settings: settingsLabel, Close: closeLabel}
}

func (a *App) onSecond(args []string) {
	for _, arg := range args {
		if arg == "--reset" {
			a.ResetPlacement()
			return
		}
	}
	a.Reveal()
}

// Bootstrap hands settings, skin and language catalogs, and directories to the page in one call.
func (a *App) Bootstrap() map[string]any {
	cur := a.settings.Get()
	return map[string]any{
		"settings":  cur,
		"skins":     a.skins.List(),
		"skinDir":   a.skins.Dir,
		"configDir": a.settings.Dir(),
		"locales":   a.locales.List(),
		"localeDir": a.locales.Dir,
		"strings":   a.locales.Merged(cur.Locale),
		"tray":      tray.Enabled(),
	}
}

func (a *App) GetSnapshot() metrics.Snapshot {
	a.lastMu.Lock()
	defer a.lastMu.Unlock()
	if !a.hasLast {
		return metrics.Snapshot{Host: "local", Cores: []float64{}, Procs: []metrics.Proc{}}
	}
	return a.last
}

func (a *App) SaveSettings(next settings.Settings) (settings.Settings, error) {
	if _, ok := a.skins.Get(next.Skin); !ok {
		next.Skin = a.skins.Resolve(next.Skin).ID
	}
	next.Locale = a.locales.Resolve(next.Locale)
	prevLogin := a.settings.Get().LaunchAtLogin
	cur, err := a.settings.Update(next)
	if err != nil {
		return settings.Settings{}, err
	}
	if cur.LaunchAtLogin != prevLogin {
		if err := autostart.Apply(cur.LaunchAtLogin); err != nil {
			return cur, err
		}
	}
	a.topKnown = false
	a.syncTop(a.fullscreen())
	tray.SetLabels(a.trayLabels())
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return cur, nil
}

func (a *App) Resize(w, h int) {
	if w < 160 {
		w = 160
	}
	if h < 48 {
		h = 48
	}
	if w > 1200 {
		w = 1200
	}
	if h > 1000 {
		h = 1000
	}
	if a.ctx != nil {
		wruntime.WindowSetSize(a.ctx, w, h)
	}
}

func (a *App) Ready() { a.ready.Store(true) }

func (a *App) SetInteracting(v bool) { a.interact.Store(v) }

func (a *App) hideTaskbar() {
	if !tray.Enabled() {
		return
	}
	desktop.HideFromTaskbar(windowTitle)
	a.taskbarAt = time.Now()
}

func (a *App) Reveal() {
	// Only pull a docked window back out. Moving a visible window here captures the pointer.
	if a.dockHidden.Load() {
		a.forceShow.Store(true)
	}
	if a.ctx != nil {
		wruntime.WindowUnminimise(a.ctx)
		wruntime.WindowShow(a.ctx)
		a.hideTaskbar()
	}
}

func (a *App) ResetPlacement() settings.Settings {
	cur := a.settings.ResetPlacement()
	a.forceCenter.Store(true)
	a.forceShow.Store(true)
	if a.ctx != nil {
		wruntime.WindowShow(a.ctx)
		a.hideTaskbar()
	}
	return cur
}

func (a *App) Quit() {
	if a.ctx != nil {
		wruntime.Quit(a.ctx)
	}
}

func (a *App) OpenSkinsDir() { _ = openDir(a.skins.Dir) }

func (a *App) OpenLocalesDir() { _ = openDir(a.locales.Dir) }

func (a *App) OpenConfigDir() { _ = openDir(a.settings.Dir()) }

func (a *App) CopyText(text string) error {
	if a.ctx == nil {
		return errors.New("not ready")
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	return wruntime.ClipboardSetText(a.ctx, text)
}

func (a *App) OpenProcessFolder(pid int) error {
	path, err := metrics.Executable(int32(pid))
	if err != nil {
		return err
	}
	if path == "" {
		return errors.New("no path")
	}
	return revealFile(path)
}

func (a *App) ProcessPath(pid int) (string, error) {
	return metrics.Executable(int32(pid))
}

func (a *App) RestoreSkin(id string) error { return a.skins.Restore(id) }

func (a *App) metricsLoop(ctx context.Context) {
	_ = a.col.Sample(a.settings.Get().ProcessSort)
	for {
		if !a.displayed.Load() {
			select {
			case <-ctx.Done():
				return
			case <-a.wake:
			}
			continue
		}
		a.publish()
		ms := a.settings.Get().RefreshMs
		timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (a *App) publish() {
	snap := a.col.Sample(a.settings.Get().ProcessSort)
	if snap.Cores == nil {
		snap.Cores = []float64{}
	}
	if snap.Procs == nil {
		snap.Procs = []metrics.Proc{}
	}
	a.lastMu.Lock()
	a.last = snap
	a.hasLast = true
	a.lastMu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "metrics", snap)
	}
}

func (a *App) dockLoop(ctx context.Context) {
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	var st dock.State
	shown := false
	var logged time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if a.ctx == nil {
			continue
		}
		if !a.ready.Load() {
			if time.Since(a.started) > 4*time.Second {
				a.ready.Store(true)
			} else {
				continue
			}
		}
		info := desktop.Query(windowTitle)
		if len(info.Monitors) == 0 {
			if !shown {
				wruntime.WindowShow(a.ctx)
				a.hideTaskbar()
				shown = true
			}
			continue
		}
		win := a.windowRect(info)
		cur := a.settings.Get()
		in := dock.Input{
			Now:            time.Now(),
			WinX:           win.X,
			WinY:           win.Y,
			WinW:           win.W,
			WinH:           win.H,
			Monitors:       info.Monitors,
			CursorX:        info.CursorX,
			CursorY:        info.CursorY,
			CursorOK:       info.CursorOK,
			MouseDown:      info.MouseDown,
			KnowsHit:       info.KnowsHit,
			CursorOnWidget: info.CursorOnWidget,
			Interacting:    a.interact.Load(),
			EdgeHide:       cur.EdgeHide,
			AutoHide:       cur.AutoHide,
			AutoHideAfter:  time.Duration(cur.AutoHideSec) * time.Second,
			EdgeDelay:      700 * time.Millisecond,
			SnapPx:         cur.SnapPx,
			PeekPx:         cur.PeekPx,
			AllowHide:      time.Since(a.started) > 1500*time.Millisecond,
			HasSaved:       cur.HasPosition,
			SavedX:         cur.X,
			SavedY:         cur.Y,
			ForceShow:      a.forceShow.Load(),
			ForceCenter:    a.forceCenter.Load(),
		}
		var cmd dock.Command
		st, cmd = dock.Tick(st, in)
		if st.Ready {
			if in.ForceShow {
				a.forceShow.Store(false)
			}
			if in.ForceCenter {
				a.forceCenter.Store(false)
			}
		}
		if cmd.SetPos && saneMove(cmd.X, cmd.Y, win.W, win.H, info.Monitors) {
			if !a.hasLastAbs || cmd.X != a.lastAbsX || cmd.Y != a.lastAbsY {
				a.moveAbs(cmd.X, cmd.Y, info, win)
				a.hasLastAbs = true
				a.lastAbsX, a.lastAbsY = cmd.X, cmd.Y
			}
		}
		a.dockHidden.Store(st.Hidden)
		show := !st.Hidden && !info.Fullscreen
		if a.displayed.Load() != show {
			a.displayed.Store(show)
			if show {
				select {
				case a.wake <- struct{}{}:
				default:
				}
			}
		}
		if cmd.Save {
			a.settings.SavePosition(cmd.SaveX, cmd.SaveY)
		}
		a.syncTop(info.Fullscreen)
		if cmd.Raise && !info.Fullscreen {
			desktop.Raise(windowTitle, cur.AlwaysOnTop)
		}
		if !shown {
			wruntime.WindowShow(a.ctx)
			a.hideTaskbar()
			a.topKnown = false
			a.syncTop(info.Fullscreen)
			shown = true
		}
		if tray.Enabled() && time.Since(a.taskbarAt) > time.Second {
			a.hideTaskbar()
		}
		if a.debug && time.Since(logged) > time.Second {
			logged = time.Now()
			log.Printf("dock win=%+v home=%d,%d hidden=%v cursor=%d,%d ok=%v cmd=%+v", win, st.HomeX, st.HomeY, st.Hidden, info.CursorX, info.CursorY, info.CursorOK, cmd)
		}
	}
}

func (a *App) windowRect(info desktop.Info) desktop.Rect {
	if info.HasWindow && info.Window.W > 40 && info.Window.H > 20 {
		return info.Window
	}
	x, y := wruntime.WindowGetPosition(a.ctx)
	w, h := wruntime.WindowGetSize(a.ctx)
	scale := info.Scale
	if scale < 1 {
		scale = 1
	}
	return desktop.Rect{
		X: x,
		Y: y,
		W: int(math.Round(float64(w) * scale)),
		H: int(math.Round(float64(h) * scale)),
	}
}

// moveAbs converts absolute pixels into the work-area-relative position Wails expects.
func (a *App) moveAbs(absX, absY int, info desktop.Info, win desktop.Rect) {
	idx := desktop.IndexAt(info.Monitors, win.X, win.Y, win.W, win.H)
	if idx < 0 {
		wruntime.WindowSetPosition(a.ctx, absX, absY)
		return
	}
	origin := info.Monitors[idx].Work
	wruntime.WindowSetPosition(a.ctx, absX-origin.X, absY-origin.Y)
}

func saneMove(x, y, w, h int, mons []desktop.Monitor) bool {
	target := desktop.Rect{X: x, Y: y, W: w, H: h}
	for _, m := range mons {
		zone := m.Work
		padX, padY := w+80, h+80
		zone.X -= padX
		zone.Y -= padY
		zone.W += padX * 2
		zone.H += padY * 2
		if desktop.Overlaps(target, zone) {
			return true
		}
	}
	return false
}

func (a *App) fullscreen() bool {
	return desktop.Query(windowTitle).Fullscreen
}

// syncTop keeps the window above others when that option is on, and steps aside while another app is fullscreen.
func (a *App) syncTop(fullscreen bool) {
	if a.ctx == nil {
		return
	}
	want := a.settings.Get().AlwaysOnTop && !fullscreen
	if a.topKnown && a.appliedTop == want {
		return
	}
	wruntime.WindowSetAlwaysOnTop(a.ctx, want)
	a.appliedTop = want
	a.topKnown = true
}

func openDir(path string) error {
	cmd := openDirCommand(path)
	return startDetached(cmd[0], cmd[1:]...)
}

func revealFile(path string) error {
	switch runtime.GOOS {
	case "windows":
		return startDetached("explorer", "/select,"+path)
	case "darwin":
		return startDetached("open", "-R", path)
	default:
		return startDetached("xdg-open", filepath.Dir(path))
	}
}

func openDirCommand(path string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"explorer", path}
	case "darwin":
		return []string{"open", path}
	default:
		return []string{"xdg-open", path}
	}
}

func startDetached(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

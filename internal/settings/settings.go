package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var localeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,40}$`)

const (
	fileName = "settings.json"
)

// Settings is the user-editable configuration. Window coordinates are kept by the app and are not overwritten when the panel saves.
type Settings struct {
	Skin          string   `json:"skin"`
	Locale        string   `json:"locale"`
	RefreshMs     int      `json:"refreshMs"`
	AlwaysOnTop   bool     `json:"alwaysOnTop"`
	ShowHost      bool     `json:"showHost"`
	Smooth        bool     `json:"smooth"`
	Glow          bool     `json:"glow"`
	LaunchAtLogin bool     `json:"launchAtLogin"`
	Opacity       float64  `json:"opacity"`
	UiScale       float64  `json:"uiScale"`
	ProcAction    string   `json:"procAction"`
	AutoHide      bool     `json:"autoHide"`
	AutoHideSec   int      `json:"autoHideSec"`
	EdgeHide      bool     `json:"edgeHide"`
	PeekPx        int      `json:"peekPx"`
	SnapPx        int      `json:"snapPx"`
	Modules       []string `json:"modules"`
	RankOrder     []string `json:"rankOrder"`
	ProcessLimit  int      `json:"processLimit"`
	ProcessSort   string   `json:"processSort"`
	X             int      `json:"x"`
	Y             int      `json:"y"`
	HasPosition   bool     `json:"hasPosition"`
}

func Default() Settings {
	return Settings{
		Skin:         "aurora",
		Locale:       "zh-CN",
		RefreshMs:    1000,
		AlwaysOnTop:  true,
		ShowHost:     true,
		Smooth:       true,
		Glow:         true,
		Opacity:      1,
		UiScale:      1,
		ProcAction:   "off",
		AutoHide:     false,
		AutoHideSec:  6,
		EdgeHide:     true,
		PeekPx:       18,
		SnapPx:       36,
		Modules:      []string{"cpu", "mem", "net", "disk"},
		RankOrder:    []string{"cpu", "mem", "net"},
		ProcessLimit: 8,
		ProcessSort:  "cpu",
	}
}

type Store struct {
	mu   sync.Mutex
	path string
	cur  Settings
}

func Load() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	dir = filepath.Join(dir, "NetPulsar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, fileName), cur: Default()}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = s.write(s.cur)
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.cur); err != nil {
		s.cur = Default()
	}
	s.cur = sanitize(s.cur, true)
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Dir() string { return filepath.Dir(s.path) }

func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cur)
}

// Update applies panel edits and keeps the current window coordinates.
func (s *Store) Update(next Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next.X = s.cur.X
	next.Y = s.cur.Y
	next.HasPosition = s.cur.HasPosition
	next = sanitize(next, true)
	if err := s.write(next); err != nil {
		return Settings{}, err
	}
	s.cur = next
	return clone(next), nil
}

func (s *Store) SavePosition(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.X == x && s.cur.Y == y && s.cur.HasPosition {
		return
	}
	s.cur.X = x
	s.cur.Y = y
	s.cur.HasPosition = true
	_ = s.write(s.cur)
}

func (s *Store) ResetPlacement() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur.HasPosition = false
	s.cur.EdgeHide = false
	s.cur.AutoHide = false
	_ = s.write(s.cur)
	return clone(s.cur)
}

func (s *Store) write(cur Settings) error {
	raw, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o644)
}

func sanitize(in Settings, keepPos bool) Settings {
	def := Default()
	out := in
	if !keepPos {
		out.X, out.Y, out.HasPosition = 0, 0, false
	}
	if out.Skin == "" {
		out.Skin = def.Skin
	}
	if !localeID.MatchString(out.Locale) {
		out.Locale = def.Locale
	}
	if out.RefreshMs < 250 {
		out.RefreshMs = 250
	}
	if out.RefreshMs > 5000 {
		out.RefreshMs = 5000
	}
	if out.Opacity < 0.35 {
		out.Opacity = 0.35
	}
	if out.Opacity > 1 {
		out.Opacity = 1
	}
	if out.UiScale < 0.8 || out.UiScale > 1.5 {
		if out.UiScale == 0 {
			out.UiScale = 1
		} else if out.UiScale < 0.8 {
			out.UiScale = 0.8
		} else {
			out.UiScale = 1.5
		}
	}
	switch out.ProcAction {
	case "folder", "copy":
	default:
		out.ProcAction = "off"
	}
	if out.AutoHideSec < 2 {
		out.AutoHideSec = 2
	}
	if out.AutoHideSec > 120 {
		out.AutoHideSec = 120
	}
	if out.PeekPx < 8 {
		out.PeekPx = 8
	}
	if out.PeekPx > 80 {
		out.PeekPx = 80
	}
	if out.SnapPx < 8 {
		out.SnapPx = 8
	}
	if out.SnapPx > 120 {
		out.SnapPx = 120
	}
	if out.ProcessLimit < 3 {
		out.ProcessLimit = 3
	}
	if out.ProcessLimit > 16 {
		out.ProcessLimit = 16
	}
	switch out.ProcessSort {
	case "mem", "net":
	default:
		out.ProcessSort = "cpu"
	}
	out.Modules = normalizeModules(out.Modules)
	out.RankOrder = normalizeRank(out.RankOrder)
	return out
}

func normalizeModules(in []string) []string {
	allowed := map[string]bool{"cpu": true, "mem": true, "net": true, "disk": true}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, m := range in {
		if !allowed[m] || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return []string{"cpu", "mem", "net", "disk"}
	}
	return out
}

func normalizeRank(in []string) []string {
	allowed := []string{"cpu", "mem", "net"}
	ok := map[string]bool{"cpu": true, "mem": true, "net": true}
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, id := range in {
		if !ok[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range allowed {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}

func clone(s Settings) Settings {
	s.Modules = append([]string(nil), s.Modules...)
	s.RankOrder = append([]string(nil), s.RankOrder...)
	return s
}

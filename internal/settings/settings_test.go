package settings

import "testing"

func TestSanitizeClampsAndKeepsAModule(t *testing.T) {
	in := Default()
	in.RefreshMs = 10
	in.Opacity = 0
	in.Modules = []string{"nope", "net", "net"}
	got := sanitize(in, true)
	if got.RefreshMs != 250 {
		t.Fatalf("refresh %d", got.RefreshMs)
	}
	if got.Opacity != 0.35 {
		t.Fatalf("opacity %v", got.Opacity)
	}
	if len(got.Modules) != 1 || got.Modules[0] != "net" {
		t.Fatalf("modules %#v", got.Modules)
	}
	in.Locale = "../nope"
	in.ShowHost = false
	in.Smooth = false
	got = sanitize(in, true)
	if got.Locale != "zh-CN" || got.ShowHost || got.Smooth {
		t.Fatalf("locale/flags %#v", got)
	}
	in.RankOrder = []string{"net", "cpu", "net", "nope"}
	got = sanitize(in, true)
	if len(got.RankOrder) != 3 || got.RankOrder[0] != "net" || got.RankOrder[1] != "cpu" || got.RankOrder[2] != "mem" {
		t.Fatalf("rank %#v", got.RankOrder)
	}
	in.Glow = false
	in.UiScale = 0.5
	in.ProcAction = "folder"
	got = sanitize(in, true)
	if got.Glow || got.UiScale != 0.8 || got.ProcAction != "folder" {
		t.Fatalf("advanced %#v", got)
	}
	in.UiScale = 0
	in.ProcAction = "nope"
	got = sanitize(in, true)
	if got.UiScale != 1 || got.ProcAction != "off" {
		t.Fatalf("defaults %#v", got)
	}
}

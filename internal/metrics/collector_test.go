package metrics

import (
	"testing"
	"time"
)

func TestNicRatesIgnoreNewAndGlitchedInterfaces(t *testing.T) {
	prev := map[string]nicCum{
		"Ethernet": {recv: 1_000_000, sent: 500_000},
	}
	cur := map[string]nicCum{
		"Ethernet":        {recv: 1_500_000, sent: 500_000},
		"vEthernet (WSL)": {recv: 80 << 30, sent: 40 << 30},
		"Ethernet 2":      {recv: 1_000_000 + 5<<30, sent: 0},
	}
	down, up := nicRates(prev, cur, time.Second)
	if down != 500_000 || up != 0 {
		t.Fatalf("down=%v up=%v", down, up)
	}
	glitch := map[string]nicCum{"Ethernet": {recv: 1_000_000, sent: 500_000 + 8<<30}}
	_, up = nicRates(prev, glitch, time.Second)
	if up != 0 {
		t.Fatalf("glitch up=%v", up)
	}
}

func TestChooseInterfaceUsesTheRequestedAdapter(t *testing.T) {
	names := []string{"vEthernet (WSL)", "以太网", "WLAN"}
	if got := ChooseInterface(names, "WLAN"); got != "WLAN" {
		t.Fatalf("picked %s", got)
	}
	if got := ChooseInterface(names, ""); got != "以太网" {
		t.Fatalf("auto %s", got)
	}
	if got := ChooseInterface(names, "missing"); got != "" {
		t.Fatalf("missing should stay empty, got %s", got)
	}
	if got := ChooseInterface([]string{"vEthernet (Default Switch)"}, ""); got != "vEthernet (Default Switch)" {
		t.Fatalf("fallback %s", got)
	}
}

func TestBytesPerSecond(t *testing.T) {
	if got := bytesPerSecond(100, 100, time.Second); got != 0 {
		t.Fatalf("flat counter: got %v", got)
	}
	if got := bytesPerSecond(100, 50, time.Second); got != 0 {
		t.Fatalf("reset counter: got %v", got)
	}
	if got := bytesPerSecond(1000, 3000, 2*time.Second); got != 1000 {
		t.Fatalf("rate: got %v", got)
	}
}

func TestMergeTopKeepsBothSorts(t *testing.T) {
	rows := []Proc{
		{PID: 1, Name: "cpu-heavy", CPU: 40, Mem: 1},
		{PID: 2, Name: "mem-heavy", CPU: 1, Mem: 30},
		{PID: 3, Name: "quiet", CPU: 0.2, Mem: 0.2},
	}
	got := mergeTop(rows, 1)
	seen := map[int32]bool{}
	for _, p := range got {
		seen[p.PID] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("expected both leaders, got %#v", got)
	}
}

func TestNetRatesAndSort(t *testing.T) {
	prev := map[int32]netCum{7: {in: 1000, out: 100}}
	cur := map[int32]netCum{7: {in: 5000, out: 1100}}
	got := netRates(prev, cur, time.Second)
	if got[7][0] != 4000 || got[7][1] != 1000 {
		t.Fatalf("rates %#v", got)
	}
	rows := []Proc{
		{PID: 1, Name: "cpu", CPU: 20, Mem: 1},
		{PID: 7, Name: "downloader", CPU: 0.2, Mem: 0.2, Down: 4000, Up: 1000},
	}
	top := mergeTop(rows, 1)
	seen := map[int32]bool{}
	for _, p := range top {
		seen[p.PID] = true
	}
	if !seen[7] {
		t.Fatalf("net leader missing: %#v", top)
	}
	if normalizeSort("网速") != "net" {
		t.Fatal("sort")
	}
}

func TestExecutableRejectsBadPID(t *testing.T) {
	if _, err := Executable(0); err == nil {
		t.Fatal("expected error")
	}
}

func TestSampleStaysResponsive(t *testing.T) {
	c := New()
	_ = c.Sample("cpu")
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	snap := c.Sample("net")
	elapsed := time.Since(start)
	t.Logf("procs=%d elapsed=%s", len(snap.Procs), elapsed)
	if elapsed > 800*time.Millisecond {
		t.Fatalf("sample blocked for %s", elapsed)
	}
}

func TestProcNetSnapshot(t *testing.T) {
	got := procNetSnapshot()
	if got == nil {
		t.Skip("this platform has no per-process TCP counters")
	}
	if len(got) == 0 {
		t.Fatal("no per-process TCP counters")
	}
	n := 0
	for pid, c := range got {
		if c.in+c.out == 0 {
			continue
		}
		t.Logf("pid %d in %d out %d", pid, c.in, c.out)
		n++
		if n >= 3 {
			break
		}
	}
}

func TestCleanName(t *testing.T) {
	if got := cleanName("Chrome.EXE"); got != "Chrome" {
		t.Fatalf("got %q", got)
	}
	if skipProc("System Idle Process") != true {
		t.Fatal("idle should be skipped")
	}
}

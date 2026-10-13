package metrics

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Snapshot is one frame pushed to the skin. Rates are bytes per second.
type Snapshot struct {
	CPU       float64   `json:"cpu"`
	Cores     []float64 `json:"cores"`
	Mem       float64   `json:"mem"`
	MemUsed   uint64    `json:"memUsed"`
	MemTotal  uint64    `json:"memTotal"`
	Disk      float64   `json:"disk"`
	DiskUsed  uint64    `json:"diskUsed"`
	DiskTotal uint64    `json:"diskTotal"`
	DiskRead  float64   `json:"diskRead"`
	DiskWrite float64   `json:"diskWrite"`
	Down      float64   `json:"down"`
	Up        float64   `json:"up"`
	Host      string    `json:"host"`
	Uptime    uint64    `json:"uptime"`
	Procs     []Proc    `json:"procs"`
	Sort      string    `json:"sort"`
	At        int64     `json:"at"`
}

// Proc is one process row. CPU and memory are percentages of the whole machine. Down and Up are bytes per second.
type Proc struct {
	PID  int32   `json:"pid"`
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Down float64 `json:"down"`
	Up   float64 `json:"up"`
}

type counter struct {
	recv  uint64
	sent  uint64
	read  uint64
	write uint64
	at    time.Time
	ok    bool
}

// nicCum is one interface's cumulative counters. Rates are computed per interface
// so a newly appeared adapter does not dump its lifetime total into one sample.
type nicCum struct {
	recv uint64
	sent uint64
}

// Collector samples CPU, memory, disk, network, and processes. Process objects are reused;
// otherwise every CPU reading is a first sample and stays at zero.
type Collector struct {
	procs map[int32]*process.Process
	nics  map[string]nicCum
	netAt time.Time
	netOK bool
	iface string
	disk  counter
	rates rates
	host  string
	boot  uint64
	ncpu  int
	last  Snapshot

	names      map[int32]string
	ticks      map[int32]cpuTick
	hadTicks   bool
	netPrev    map[int32]netCum
	netPrevAt  time.Time
	procsReady bool
	procsAt    time.Time
	procsCache []Proc
}

func New() *Collector {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	c := &Collector{
		procs:   map[int32]*process.Process{},
		nics:    map[string]nicCum{},
		names:   map[int32]string{},
		ticks:   map[int32]cpuTick{},
		netPrev: map[int32]netCum{},
		ncpu:    n,
		host:    "local",
	}
	_, _ = cpu.Percent(0, false)
	_, _ = cpu.Percent(0, true)
	if info, err := host.Info(); err == nil && info != nil {
		if info.Hostname != "" {
			c.host = info.Hostname
		}
		c.boot = info.BootTime
	}
	return c
}

// Sample collects one frame. The returned process list keeps the leaders for CPU, memory, and network, not only sortKey.
func (c *Collector) Sample(sortKey string) Snapshot {
	now := time.Now()
	snap := Snapshot{
		Host:  c.host,
		Sort:  normalizeSort(sortKey),
		At:    now.UnixMilli(),
		Cores: []float64{},
		Procs: []Proc{},
	}
	if c.boot > 0 {
		if u := uint64(now.Unix()) - c.boot; now.Unix() > 0 && uint64(now.Unix()) >= c.boot {
			snap.Uptime = u
		}
	}

	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		snap.CPU = clampPct(pct[0])
	} else {
		snap.CPU = c.last.CPU
	}
	if cores, err := cpu.Percent(0, true); err == nil && len(cores) > 0 {
		snap.Cores = make([]float64, len(cores))
		for i, v := range cores {
			snap.Cores[i] = clampPct(v)
		}
	} else {
		snap.Cores = c.last.Cores
	}

	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		snap.Mem = clampPct(vm.UsedPercent)
		snap.MemUsed = vm.Used
		snap.MemTotal = vm.Total
	}

	if usage, err := disk.Usage(systemDisk()); err == nil && usage != nil {
		snap.Disk = clampPct(usage.UsedPercent)
		snap.DiskUsed = usage.Used
		snap.DiskTotal = usage.Total
	}

	c.sampleNetDisk(now)
	snap.Down = c.rates.down
	snap.Up = c.rates.up
	snap.DiskRead = c.rates.read
	snap.DiskWrite = c.rates.write
	snap.Procs = c.sampleProcs(now)
	sortProcs(snap.Procs, snap.Sort)

	c.last = snap
	return snap
}

type rates struct {
	down, up, read, write float64
}

// SetIface chooses the single interface whose throughput is shown. An empty name picks one automatically.
func (c *Collector) SetIface(name string) { c.iface = name }

func (c *Collector) sampleNetDisk(now time.Time) {
	if counters, err := gnet.IOCounters(true); err == nil {
		all := make(map[string]nicCum, len(counters))
		names := make([]string, 0, len(counters))
		for _, it := range counters {
			if isLoopback(it.Name) {
				continue
			}
			all[it.Name] = nicCum{recv: it.BytesRecv, sent: it.BytesSent}
			names = append(names, it.Name)
		}
		cur := map[string]nicCum{}
		if name := ChooseInterface(names, c.iface); name != "" {
			cur[name] = all[name]
		}
		if c.netOK {
			dt := now.Sub(c.netAt)
			// A wake can sample again almost immediately. Dividing a small burst by that gap looks like GB/s.
			if dt >= 100*time.Millisecond {
				c.rates.down, c.rates.up = nicRates(c.nics, cur, dt)
				c.nics = cur
				c.netAt = now
			}
		} else {
			c.nics = cur
			c.netAt = now
			c.netOK = true
		}
	}
	var read, write uint64
	if parts, err := disk.IOCounters(); err == nil {
		for _, it := range parts {
			read += it.ReadBytes
			write += it.WriteBytes
		}
	}
	if c.disk.ok {
		dt := now.Sub(c.disk.at)
		c.rates.read = bytesPerSecond(c.disk.read, read, dt)
		c.rates.write = bytesPerSecond(c.disk.write, write, dt)
	}
	c.disk = counter{read: read, write: write, at: now, ok: true}
}

// maxByteRate drops a single reading above ~16 Gbps. That is a counter glitch, not a link.
const maxByteRate = 2 * 1024 * 1024 * 1024

func nicRates(prev, cur map[string]nicCum, dt time.Duration) (down, up float64) {
	sec := dt.Seconds()
	if sec <= 0 {
		return 0, 0
	}
	for name, now := range cur {
		old, ok := prev[name]
		if !ok {
			continue
		}
		down += boundedRate(old.recv, now.recv, sec)
		up += boundedRate(old.sent, now.sent, sec)
	}
	return down, up
}

func boundedRate(prev, cur uint64, sec float64) float64 {
	if sec <= 0 || cur < prev {
		return 0
	}
	rate := float64(cur-prev) / sec
	if rate > maxByteRate {
		return 0
	}
	return rate
}

type cpuTick struct {
	cpu uint64
	at  time.Time
}

type netCum struct {
	in, out uint64
}

func (c *Collector) sampleProcs(now time.Time) []Proc {
	// The process table costs more than the machine counters, so a fast refresh reuses the previous frame.
	if c.procsReady && now.Sub(c.procsAt) < 700*time.Millisecond && len(c.procsCache) > 0 {
		return append([]Proc(nil), c.procsCache...)
	}
	rows := c.collectProcs(now)
	rows = c.attachNet(rows, now)
	rows = mergeTop(filterQuiet(rows), 12)
	if c.hadTicks {
		c.procsCache = append([]Proc(nil), rows...)
		c.procsAt = now
		c.procsReady = true
	}
	c.hadTicks = true
	return append([]Proc(nil), rows...)
}

func (c *Collector) attachNet(rows []Proc, now time.Time) []Proc {
	cur := procNetSnapshot()
	prev, prevAt := c.netPrev, c.netPrevAt
	c.netPrev = cur
	c.netPrevAt = now
	if prevAt.IsZero() || len(cur) == 0 {
		return rows
	}
	rates := netRates(prev, cur, now.Sub(prevAt))
	index := make(map[int32]int, len(rows))
	for i := range rows {
		index[rows[i].PID] = i
	}
	for pid, pair := range rates {
		down, up := pair[0], pair[1]
		if i, ok := index[pid]; ok {
			rows[i].Down = down
			rows[i].Up = up
			continue
		}
		if down+up < 256 {
			continue
		}
		name := c.names[pid]
		if name == "" {
			name = "pid"
		}
		rows = append(rows, Proc{PID: pid, Name: name, Down: down, Up: up})
	}
	return rows
}

func filterQuiet(rows []Proc) []Proc {
	out := rows[:0]
	for _, row := range rows {
		if row.CPU < 0.1 && row.Mem < 0.1 && row.Down+row.Up < 256 {
			continue
		}
		out = append(out, row)
	}
	return out
}

func netRates(prev, cur map[int32]netCum, dt time.Duration) map[int32][2]float64 {
	out := make(map[int32][2]float64)
	if dt <= 0 {
		return out
	}
	sec := dt.Seconds()
	for pid, now := range cur {
		old, ok := prev[pid]
		if !ok || now.in < old.in || now.out < old.out {
			continue
		}
		out[pid] = [2]float64{
			boundedRate(old.in, now.in, sec),
			boundedRate(old.out, now.out, sec),
		}
	}
	return out
}

func cpuShare(prev cpuTick, cur uint64, now time.Time, ncpu int) float64 {
	if prev.at.IsZero() || cur < prev.cpu || ncpu < 1 {
		return 0
	}
	dt := now.Sub(prev.at).Seconds()
	if dt < 0.05 {
		return 0
	}
	used := float64(cur-prev.cpu) / 1e7
	return clampPct(used / dt * 100 / float64(ncpu))
}

func normalizeSort(s string) string {
	switch strings.ToLower(s) {
	case "mem", "内存":
		return "mem"
	case "net", "网络", "网速":
		return "net"
	default:
		return "cpu"
	}
}

func systemDisk() string {
	if runtime.GOOS == "windows" {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		return drive + `\`
	}
	return "/"
}

func isLoopback(name string) bool {
	n := strings.ToLower(name)
	return n == "lo" || strings.Contains(n, "loopback")
}

func cleanName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name[:len(name)-4]
	}
	if len([]rune(name)) > 48 {
		r := []rune(name)
		name = string(r[:48])
	}
	return name
}

func skipProc(name string) bool {
	switch strings.ToLower(name) {
	case "", "system idle process", "idle":
		return true
	default:
		return false
	}
}

func bytesPerSecond(prev, cur uint64, dt time.Duration) float64 {
	if dt <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / dt.Seconds()
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func sortProcs(rows []Proc, key string) {
	// Insertion sort is enough for a short list.
	for i := 1; i < len(rows); i++ {
		j := i
		for j > 0 && lessProc(rows[j], rows[j-1], key) {
			rows[j], rows[j-1] = rows[j-1], rows[j]
			j--
		}
	}
}

func lessProc(a, b Proc, key string) bool {
	switch key {
	case "mem":
		if a.Mem != b.Mem {
			return a.Mem > b.Mem
		}
	case "net":
		an, bn := a.Down+a.Up, b.Down+b.Up
		if an != bn {
			return an > bn
		}
	default:
		if a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
	}
	if key != "mem" && a.Mem != b.Mem {
		return a.Mem > b.Mem
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

func mergeTop(rows []Proc, n int) []Proc {
	if n < 1 {
		n = 1
	}
	cpuRows := append([]Proc(nil), rows...)
	memRows := append([]Proc(nil), rows...)
	netRows := append([]Proc(nil), rows...)
	sortProcs(cpuRows, "cpu")
	sortProcs(memRows, "mem")
	sortProcs(netRows, "net")
	seen := map[int32]Proc{}
	order := make([]int32, 0, n*3)
	take := func(list []Proc) {
		limit := n
		if limit > len(list) {
			limit = len(list)
		}
		for i := 0; i < limit; i++ {
			id := list[i].PID
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = list[i]
			order = append(order, id)
		}
	}
	take(cpuRows)
	take(memRows)
	take(netRows)
	out := make([]Proc, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	return out
}

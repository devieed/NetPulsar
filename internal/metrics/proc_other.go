//go:build !windows

package metrics

import (
	"errors"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Executable returns the executable path of a running process.
func Executable(pid int32) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return "", err
	}
	return p.Exe()
}

func (c *Collector) collectProcs(now time.Time) []Proc {
	list, err := process.Processes()
	if err != nil {
		return nil
	}
	next := make(map[int32]*process.Process, len(list))
	names := make(map[int32]string, len(list))
	rows := make([]Proc, 0, 32)
	for _, p := range list {
		if p == nil || p.Pid <= 4 {
			continue
		}
		handle := p
		if old, ok := c.procs[p.Pid]; ok && old != nil {
			handle = old
		}
		next[p.Pid] = handle
		name, _ := handle.Name()
		name = cleanName(name)
		if skipProc(name) {
			continue
		}
		names[p.Pid] = name
		memPct, _ := handle.MemoryPercent()
		cpuPct, cpuErr := handle.CPUPercent()
		share := 0.0
		if cpuErr == nil && cpuPct > 0 {
			share = cpuPct / float64(c.ncpu)
		}
		row := Proc{
			PID:  p.Pid,
			Name: name,
			CPU:  round1(clampPct(share)),
			Mem:  round1(clampPct(float64(memPct))),
		}
		if row.CPU < 0.05 && row.Mem < 0.05 {
			continue
		}
		rows = append(rows, row)
	}
	c.procs = next
	c.names = names
	_ = now
	return rows
}

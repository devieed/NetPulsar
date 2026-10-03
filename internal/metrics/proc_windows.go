//go:build windows

package metrics

import (
	"errors"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/sys/windows"
)

const processQueryLimited = 0x1000

var (
	getProcessMemoryInfo       = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	queryFullProcessImageNameW = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
)

type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// collectProcs reads CPU and memory from one toolhelp snapshot.
// Opening every process on each tick made the desktop pointer stall.
func (c *Collector) collectProcs(now time.Time) []Proc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var total uint64
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		total = vm.Total
	}

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return nil
	}

	next := make(map[int32]cpuTick, len(c.ticks)+8)
	names := make(map[int32]string, len(c.names)+8)
	rows := make([]Proc, 0, 32)
	for {
		pid := int32(pe.ProcessID)
		name := cleanName(windows.UTF16ToString(pe.ExeFile[:]))
		if pid > 4 && !skipProc(name) {
			names[pid] = name
			cpu, rss, ok := readProc(uint32(pid))
			tick := cpuTick{cpu: cpu, at: now}
			share := 0.0
			if ok {
				if prev, seen := c.ticks[pid]; seen {
					share = cpuShare(prev, cpu, now, c.ncpu)
				}
				next[pid] = tick
			}
			memPct := 0.0
			if total > 0 && rss > 0 {
				memPct = clampPct(float64(rss) / float64(total) * 100)
			}
			if share >= 0.05 || memPct >= 0.05 {
				rows = append(rows, Proc{
					PID:  pid,
					Name: name,
					CPU:  round1(share),
					Mem:  round1(memPct),
				})
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	c.ticks = next
	c.names = names
	return rows
}

func readProc(pid uint32) (cpu uint64, rss uint64, ok bool) {
	h, err := windows.OpenProcess(processQueryLimited, false, pid)
	if err != nil {
		return 0, 0, false
	}
	defer windows.CloseHandle(h)

	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, 0, false
	}
	var pmc processMemoryCounters
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	r, _, _ := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb))
	cpu = filetime(user) + filetime(kernel)
	if r == 0 {
		return cpu, 0, true
	}
	return cpu, uint64(pmc.workingSetSize), true
}

// Executable returns the Win32 path of a running process.
func Executable(pid int32) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid pid")
	}
	h, err := windows.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, callErr := queryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return "", callErr
		}
		return "", errors.New("no path")
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func filetime(f windows.Filetime) uint64 {
	return uint64(f.HighDateTime)<<32 | uint64(uint32(f.LowDateTime))
}

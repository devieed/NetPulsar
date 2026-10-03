//go:build windows

package metrics

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                   = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTcpTable        = iphlpapi.NewProc("GetExtendedTcpTable")
	getPerTcpConnectionEStats  = iphlpapi.NewProc("GetPerTcpConnectionEStats")
	getPerTcp6ConnectionEStats = iphlpapi.NewProc("GetPerTcp6ConnectionEStats")
)

const (
	afInet              = 2
	afInet6             = 23
	tcpTableOwnerPidAll = 5
	tcpEstatsData       = 1
	tcpStateEstablished = 5
	maxEstablishedConns = 320
)

// tcpDataRod matches TCP_ESTATS_DATA_ROD_v0. The trailing 64-bit fields are aligned to 8 bytes, so the struct is 96 bytes.
type tcpDataRod struct {
	DataBytesOut      uint64
	DataSegsOut       uint64
	DataBytesIn       uint64
	DataSegsIn        uint64
	SegsOut           uint64
	SegsIn            uint64
	SoftErrors        uint32
	SoftErrorReason   uint32
	SndUna            uint32
	SndNxt            uint32
	SndMax            uint32
	ThruBytesAcked    uint64
	RcvNxt            uint32
	ThruBytesReceived uint64
}

// procNetSnapshot sums the cumulative TCP byte counters for each process.
func procNetSnapshot() map[int32]netCum {
	out := make(map[int32]netCum)
	var queried int
	if buf := tcpTable(afInet); len(buf) >= 4 {
		queried = addV4(buf, out, queried)
	}
	if queried < maxEstablishedConns {
		if buf := tcpTable(afInet6); len(buf) >= 4 {
			addV6(buf, out, queried)
		}
	}
	return out
}

func tcpTable(family uintptr) []byte {
	var size uint32
	r0, _, _ := getExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPidAll, 0)
	if r0 != 0 && r0 != 122 {
		return nil
	}
	if size < 4 || size > 8<<20 {
		return nil
	}
	buf := make([]byte, size+256)
	size = uint32(len(buf))
	r, _, _ := getExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0, family, tcpTableOwnerPidAll, 0,
	)
	if r != 0 || int(size) > len(buf) {
		return nil
	}
	return buf[:size]
}

func addV4(buf []byte, out map[int32]netCum, queried int) int {
	n := int(*(*uint32)(unsafe.Pointer(&buf[0])))
	const row = 24
	if max := (len(buf) - 4) / row; n > max {
		n = max
	}
	for i := 0; i < n && queried < maxEstablishedConns; i++ {
		p := unsafe.Pointer(&buf[4+i*row])
		if *(*uint32)(p) != tcpStateEstablished {
			continue
		}
		queried++
		pid := int32(*(*uint32)(unsafe.Pointer(uintptr(p) + 20)))
		in, outb, ok := estats(getPerTcpConnectionEStats, p)
		if ok && pid > 4 {
			addCum(out, pid, in, outb)
		}
	}
	return queried
}

func addV6(buf []byte, out map[int32]netCum, queried int) int {
	n := int(*(*uint32)(unsafe.Pointer(&buf[0])))
	const row = 56
	if max := (len(buf) - 4) / row; n > max {
		n = max
	}
	for i := 0; i < n && queried < maxEstablishedConns; i++ {
		p := unsafe.Pointer(&buf[4+i*row])
		if *(*uint32)(unsafe.Pointer(uintptr(p) + 48)) != tcpStateEstablished {
			continue
		}
		queried++
		pid := int32(*(*uint32)(unsafe.Pointer(uintptr(p) + 52)))
		in, outb, ok := estats(getPerTcp6ConnectionEStats, p)
		if ok && pid > 4 {
			addCum(out, pid, in, outb)
		}
	}
	return queried
}

func estats(proc *windows.LazyProc, row unsafe.Pointer) (in, out uint64, ok bool) {
	var rod tcpDataRod
	r, _, _ := proc.Call(
		uintptr(row),
		tcpEstatsData,
		0, 0, 0,
		0, 0, 0,
		uintptr(unsafe.Pointer(&rod)),
		0,
		unsafe.Sizeof(rod),
	)
	if r != 0 {
		return 0, 0, false
	}
	return rod.DataBytesIn, rod.DataBytesOut, true
}

func addCum(m map[int32]netCum, pid int32, in, out uint64) {
	cur := m[pid]
	cur.in += in
	cur.out += out
	m[pid] = cur
}

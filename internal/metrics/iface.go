package metrics

import (
	"sort"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// ListInterfaces returns the non-loopback adapters the user can pick.
func ListInterfaces() []string {
	counters, err := gnet.IOCounters(true)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(counters))
	for _, it := range counters {
		if it.Name == "" || isLoopback(it.Name) {
			continue
		}
		names = append(names, it.Name)
	}
	sort.Strings(names)
	return names
}

// ChooseInterface returns want when that adapter is present.
// An empty want picks a physical adapter. A missing explicit choice counts nothing.
func ChooseInterface(names []string, want string) string {
	if want != "" {
		for _, name := range names {
			if name == want {
				return name
			}
		}
		return ""
	}
	var wired, wireless, other []string
	for _, name := range names {
		if isVirtual(name) {
			continue
		}
		switch {
		case isWired(name):
			wired = append(wired, name)
		case isWireless(name):
			wireless = append(wireless, name)
		default:
			other = append(other, name)
		}
	}
	for _, group := range [][]string{wired, wireless, other} {
		if len(group) > 0 {
			sort.Strings(group)
			return group[0]
		}
	}
	if len(names) == 0 {
		return ""
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return sorted[0]
}

func isWired(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "ethernet") || strings.Contains(n, "以太网") || strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en")
}

func isWireless(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "wi-fi") || strings.Contains(n, "wifi") || strings.Contains(n, "wlan") || strings.Contains(n, "无线")
}

func isVirtual(name string) bool {
	n := strings.ToLower(name)
	for _, part := range []string{
		"vethernet", "hyper-v", "wsl", "docker", "vmware", "virtualbox", "vbox",
		"bluetooth", "蓝牙", "isatap", "teredo", "wan miniport", "pseudo", "tunnel", "npcap",
	} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

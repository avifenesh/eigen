// Package syshealth reads basic machine health (CPU load, memory, disk) for the
// working-station dashboard — the "is my machine OK at a glance" signal atrium
// surfaces. Linux-first via /proc and statfs; no external deps, no auth. Fields
// a platform can't read are left zero (the UI hides them) rather than erroring.
package syshealth

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Health is a point-in-time machine snapshot for the dashboard.
type Health struct {
	// Load1 is the 1-minute load average; LoadPerCPU normalizes it by core count
	// (≈1.0 means fully busy). CPUs is the core count.
	Load1      float64 `json:"load1"`
	LoadPerCPU float64 `json:"loadPerCpu"`
	CPUs       int     `json:"cpus"`
	// Memory in bytes: total + used (total-available). MemUsedPct is 0..100.
	MemTotal   uint64  `json:"memTotal"`
	MemUsed    uint64  `json:"memUsed"`
	MemUsedPct float64 `json:"memUsedPct"`
	// Disk for the root filesystem, bytes + percent.
	DiskTotal   uint64  `json:"diskTotal"`
	DiskUsed    uint64  `json:"diskUsed"`
	DiskUsedPct float64 `json:"diskUsedPct"`
	// Uptime in seconds (0 when unread).
	UptimeSec uint64 `json:"uptimeSec"`
}

// Read collects a machine-health snapshot. Best-effort: unreadable metrics stay
// zero so the dashboard simply omits them.
func Read() Health {
	h := Health{CPUs: runtime.NumCPU()}
	readLoadAvg(&h)
	readMemInfo(&h)
	readDisk(&h, "/")
	readUptime(&h)
	if h.CPUs > 0 {
		h.LoadPerCPU = h.Load1 / float64(h.CPUs)
	}
	if h.MemTotal > 0 {
		h.MemUsedPct = pct(h.MemUsed, h.MemTotal)
	}
	return h
}

// readLoadAvg parses /proc/loadavg (Linux). First field is the 1-min average.
func readLoadAvg(h *Health) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
			h.Load1 = v
		}
	}
}

// readMemInfo parses /proc/meminfo (Linux): used = MemTotal - MemAvailable.
func readMemInfo(h *Health) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	var total, avail uint64
	var haveAvail bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		// Values are in kB.
		kb := parseKB(v)
		switch k {
		case "MemTotal":
			total = kb
		case "MemAvailable":
			avail = kb
			haveAvail = true
		}
	}
	h.MemTotal = total
	if haveAvail && total >= avail {
		h.MemUsed = total - avail
	}
}

// readDisk fills disk usage for the filesystem containing path, via statfs.
func readDisk(h *Health, path string) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bavail * bs // available to unprivileged users
	h.DiskTotal = total
	if total >= free {
		h.DiskUsed = total - free
		h.DiskUsedPct = pct(h.DiskUsed, total)
	}
}

// readUptime parses /proc/uptime (Linux); first field is seconds since boot.
func readUptime(h *Health) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
			h.UptimeSec = uint64(v)
		}
	}
}

// parseKB pulls the leading integer (kB) from a /proc/meminfo value, → bytes.
func parseKB(s string) uint64 {
	for _, f := range strings.Fields(s) {
		if n, err := strconv.ParseUint(f, 10, 64); err == nil {
			return n * 1024
		}
	}
	return 0
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

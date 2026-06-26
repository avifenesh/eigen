// Package syshealth reads basic machine health (CPU load, memory, disk) for the
// working-station dashboard — the "is my machine OK at a glance" signal atrium
// surfaces. Linux-first via /proc and statfs; no external deps, no auth. Fields
// a platform can't read are left zero (the UI hides them) rather than erroring.
package syshealth

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
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
	// Swap in bytes + percent (0 when no swap configured).
	SwapTotal   uint64  `json:"swapTotal"`
	SwapUsed    uint64  `json:"swapUsed"`
	SwapUsedPct float64 `json:"swapUsedPct"`
	// CPUTempC is the hottest CPU/package temperature in °C (0 when unreadable).
	CPUTempC float64 `json:"cpuTempC"`
	// GPUs are per-GPU stats (NVIDIA via nvidia-smi); empty when none/unavailable.
	// The user trains models here, so GPU util/mem/temp are first-class.
	GPUs []GPU `json:"gpus,omitempty"`
	// Uptime in seconds (0 when unread).
	UptimeSec uint64 `json:"uptimeSec"`
}

// GPU is one accelerator's live stats.
type GPU struct {
	Name       string  `json:"name"`
	UtilPct    float64 `json:"utilPct"`    // compute utilization 0..100
	MemUsed    uint64  `json:"memUsed"`    // bytes
	MemTotal   uint64  `json:"memTotal"`   // bytes
	MemUsedPct float64 `json:"memUsedPct"`
	TempC      float64 `json:"tempC"`
	PowerW     float64 `json:"powerW"`     // current draw, watts (0 when unread)
}

// Read collects a machine-health snapshot. Best-effort: unreadable metrics stay
// zero so the dashboard simply omits them.
func Read() Health {
	h := Health{CPUs: runtime.NumCPU()}
	readLoadAvg(&h)
	readMemInfo(&h) // fills mem + swap
	readDisk(&h, "/")
	readUptime(&h)
	readCPUTemp(&h)
	readGPUs(&h)
	if h.CPUs > 0 {
		h.LoadPerCPU = h.Load1 / float64(h.CPUs)
	}
	if h.MemTotal > 0 {
		h.MemUsedPct = pct(h.MemUsed, h.MemTotal)
	}
	if h.SwapTotal > 0 {
		h.SwapUsedPct = pct(h.SwapUsed, h.SwapTotal)
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
	var total, avail, swapTotal, swapFree uint64
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
		case "SwapTotal":
			swapTotal = kb
		case "SwapFree":
			swapFree = kb
		}
	}
	h.MemTotal = total
	if haveAvail && total >= avail {
		h.MemUsed = total - avail
	}
	h.SwapTotal = swapTotal
	if swapTotal >= swapFree {
		h.SwapUsed = swapTotal - swapFree
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

// readCPUTemp reads the hottest CPU/package temperature from Linux thermal
// zones (/sys/class/thermal/thermal_zone*/temp, millidegrees C). Picks the max
// across zones that look CPU-ish (x86_pkg_temp / coretemp / cpu / acpitz),
// falling back to the overall max when no type matches. 0 when unreadable.
func readCPUTemp(h *Health) {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	var best, anyMax float64
	for _, z := range zones {
		raw, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			continue
		}
		c := milli / 1000.0
		if c <= 0 || c > 150 { // ignore bogus/sensor-absent readings
			continue
		}
		if c > anyMax {
			anyMax = c
		}
		t, _ := os.ReadFile(filepath.Join(z, "type"))
		typ := strings.ToLower(strings.TrimSpace(string(t)))
		if strings.Contains(typ, "pkg") || strings.Contains(typ, "core") ||
			strings.Contains(typ, "cpu") || strings.Contains(typ, "acpitz") {
			if c > best {
				best = c
			}
		}
	}
	if best > 0 {
		h.CPUTempC = best
	} else {
		h.CPUTempC = anyMax
	}
}

// readGPUs queries NVIDIA GPUs via nvidia-smi (CSV, no units). Silent no-op
// when nvidia-smi is absent (no GPU / non-NVIDIA). The user trains models here,
// so per-GPU util/mem/temp/power matter.
func readGPUs(h *Health) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 5 {
			continue
		}
		f := func(i int) float64 {
			if i >= len(fields) {
				return 0
			}
			v, _ := strconv.ParseFloat(strings.TrimSpace(fields[i]), 64)
			return v
		}
		g := GPU{
			Name:     strings.TrimSpace(fields[0]),
			UtilPct:  f(1),
			MemUsed:  uint64(f(2)) * 1024 * 1024, // MiB → bytes
			MemTotal: uint64(f(3)) * 1024 * 1024,
			TempC:    f(4),
			PowerW:   f(5),
		}
		if g.MemTotal > 0 {
			g.MemUsedPct = pct(g.MemUsed, g.MemTotal)
		}
		h.GPUs = append(h.GPUs, g)
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

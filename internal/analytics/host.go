package analytics

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	gib = 1 << 30
	mib = 1 << 20
)

// hostSnapshot is one coarse report: bands instead of exact hardware sizes, and
// never a hostname, address or installation id. Platform names are the ones the
// Node.js server reported, so the stream keeps one vocabulary.
func hostSnapshot(version string, started time.Time) map[string]any {
	platform, architecture := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if architecture == "amd64" {
		architecture = "x64"
	}
	cores := runtime.NumCPU()
	total, free := memory()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	snapshot := map[string]any{
		"app_version":           version,
		"os":                    platform,
		"architecture":          architecture,
		"cpu_cores_range":       powerOfTwoBucket(float64(cores)),
		"ram_gib_range":         powerOfTwoBucket(float64(total) / gib),
		"process_rss_mib_range": powerOfTwoBucket(float64(stats.Sys) / mib),
		"uptime_hours_range":    powerOfTwoBucket(time.Since(started).Hours()),
	}
	if total > 0 && free > 0 {
		snapshot["memory_used_percent_band"] = band((1-float64(free)/float64(total))*100, 100)
	}
	if load, ok := loadAverage(); ok && cores > 0 {
		snapshot["load_per_core_percent_band"] = band(load/float64(cores)*100, 200)
	}
	return snapshot
}

func powerOfTwoBucket(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return "unknown"
	}
	upper := math.Pow(2, math.Ceil(math.Log2(value)))
	format := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return format(upper/2) + "-" + format(upper)
}

// band rounds to the nearest ten between 0 and limit.
func band(value, limit float64) int {
	return int(min(max(math.Round(value/10)*10, 0), limit))
}

// memory is the machine's total and free bytes; free is 0 where reading it
// would need per-OS counters.
func memory() (total, free uint64) {
	if runtime.GOOS == "darwin" {
		raw, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err == nil {
			total, _ = strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		}
		return total, 0
	}
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = kib * 1024
		case "MemFree:":
			free = kib * 1024
		}
	}
	return total, free
}

func loadAverage() (float64, bool) {
	var raw []byte
	var err error
	switch runtime.GOOS {
	case "linux":
		raw, err = os.ReadFile("/proc/loadavg")
	case "darwin":
		// sysctl prints "{ 1.23 1.45 1.67 }".
		raw, err = exec.Command("sysctl", "-n", "vm.loadavg").Output()
	default:
		return 0, false
	}
	if err != nil {
		return 0, false
	}
	for _, field := range strings.Fields(strings.Trim(string(raw), "{} \n")) {
		if value, err := strconv.ParseFloat(field, 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

const tickThreadNice = -10

const tickThreadComm = "Server thread"

func (s *supervisor) runTickThreadBooster() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range 60 {
		select {
		case <-s.done():
			return
		case <-ticker.C:
		}
		tid := findThreadByComm(s.pid, tickThreadComm)
		if tid == 0 {
			continue
		}
		if err := syscall.Setpriority(syscall.PRIO_PROCESS, tid, tickThreadNice); err != nil {
			fmt.Printf("[discopanel-runtime] WARN: cannot raise tick thread priority (%v), container lacks SYS_NICE\n", err)
			return
		}
		fmt.Printf("[discopanel-runtime] tick thread priority raised (nice %d)\n", tickThreadNice)
		return
	}
}

func findThreadByComm(pid int, comm string) int {
	taskDir := filepath.Join("/proc", strconv.Itoa(pid), "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(taskDir, e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == comm {
			if tid, err := strconv.Atoi(e.Name()); err == nil {
				return tid
			}
		}
	}
	return 0
}

// How often process, cgroup, and GC telemetry is reported
const procSampleInterval = 15 * time.Second

// Kernel USER_HZ used by /proc accounting
const clockTicksPerSecond = 100

// Reads the java process's cumulative user and system CPU ticks
func cpuTimes(pid int) (int64, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	// Comm field may contain spaces, fields resume after last paren
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx < 0 {
		return 0, false
	}
	rest := strings.Fields(s[idx+1:])
	if len(rest) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseInt(rest[11], 10, 64)
	stime, err2 := strconv.ParseInt(rest[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}

// Reads the java process's resident set size in MiB
func rssMB(pid int) float64 {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return float64(pages) * float64(os.Getpagesize()) / 1024 / 1024
}

// Cumulative CFS accounting for the container cgroup
type cgroupCPUStat struct {
	periods        int64
	throttledCount int64
	throttledUsec  int64
	quotaCores     float64
	valid          bool
}

// Reads CFS throttling counters for cgroup v2 or v1
func readCgroupCPUStat() cgroupCPUStat {
	// Cgroup v2 (unified hierarchy)
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.stat"); err == nil {
		st := cgroupCPUStat{valid: true}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			v, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "nr_periods":
				st.periods = v
			case "nr_throttled":
				st.throttledCount = v
			case "throttled_usec":
				st.throttledUsec = v
			}
		}
		if max, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
			fields := strings.Fields(string(max))
			if len(fields) == 2 && fields[0] != "max" {
				quota, err1 := strconv.ParseFloat(fields[0], 64)
				period, err2 := strconv.ParseFloat(fields[1], 64)
				if err1 == nil && err2 == nil && period > 0 {
					st.quotaCores = quota / period
				}
			}
		}
		return st
	}

	// Cgroup v1
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu/cpu.stat"); err == nil {
		st := cgroupCPUStat{valid: true}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			v, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "nr_periods":
				st.periods = v
			case "nr_throttled":
				st.throttledCount = v
			case "throttled_time": // nanoseconds in v1
				st.throttledUsec = v / 1000
			}
		}
		quota := readInt64File("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
		period := readInt64File("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
		if quota > 0 && period > 0 {
			st.quotaCores = float64(quota) / float64(period)
		}
		return st
	}

	return cgroupCPUStat{}
}

// Reads the container memory limit in MiB
func readCgroupMemoryLimitMB() int64 {
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		v := strings.TrimSpace(string(data))
		if v == "max" {
			return 0
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n / 1024 / 1024
		}
		return 0
	}
	n := readInt64File("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	// Cgroup v1 reports no limit as a huge value
	if n <= 0 || n > int64(1)<<50 {
		return 0
	}
	return n / 1024 / 1024
}

func readInt64File(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// Reads the host global THP mode, empty when unreadable
func readHostTHPMode() string {
	data, err := os.ReadFile("/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil {
		return ""
	}
	return parseTHPMode(string(data))
}

// Active mode sits in brackets, like "always [madvise] never"
func parseTHPMode(s string) string {
	start := strings.IndexByte(s, '[')
	end := strings.IndexByte(s, ']')
	if start < 0 || end <= start {
		return ""
	}
	return s[start+1 : end]
}

// Reads cgroup v2 pressure files, nil without PSI
func readPSI() *agentv1.Psi {
	cpuSome, cpuOK := psiAvg10("/sys/fs/cgroup/cpu.pressure", "some")
	memSome, memOK := psiAvg10("/sys/fs/cgroup/memory.pressure", "some")
	memFull, _ := psiAvg10("/sys/fs/cgroup/memory.pressure", "full")
	ioSome, ioOK := psiAvg10("/sys/fs/cgroup/io.pressure", "some")
	if !cpuOK && !memOK && !ioOK {
		return nil
	}
	return &agentv1.Psi{
		CpuSomeAvg10: cpuSome,
		MemSomeAvg10: memSome,
		MemFullAvg10: memFull,
		IoSomeAvg10:  ioSome,
	}
}

// Pulls one avg10 percentage from a pressure file line
func psiAvg10(path, kind string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != kind {
			continue
		}
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(f, "avg10="); ok {
				parsed, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return 0, false
				}
				return parsed, true
			}
		}
	}
	return 0, false
}

// Reads the cgroup's cumulative OOM kill count
func readOOMKills() int64 {
	// Cgroup v2 (unified hierarchy)
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.events"); err == nil {
		return parseKeyedCount(string(data), "oom_kill")
	}
	// Cgroup v1, oom_kill appears in oom_control on 4.13+
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.oom_control"); err == nil {
		return parseKeyedCount(string(data), "oom_kill")
	}
	return 0
}

// Extracts one key value line from a cgroup stat file
func parseKeyedCount(data, key string) int64 {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

// Periodically reports process CPU, RSS, and GC pauses
func (s *supervisor) runProcSampler(gcTail *gcLogTail) {
	ticker := time.NewTicker(procSampleInterval)
	defer ticker.Stop()

	prevTicks, _ := cpuTimes(s.pid)
	prevCg := readCgroupCPUStat()
	prevTime := time.Now()

	for {
		select {
		case <-s.done():
			return
		case <-ticker.C:
		}

		now := time.Now()
		elapsed := now.Sub(prevTime).Seconds()
		prevTime = now
		if elapsed <= 0 {
			continue
		}

		sample := &agentv1.ProcSample{RssMb: rssMB(s.pid)}

		if ticks, ok := cpuTimes(s.pid); ok {
			delta := ticks - prevTicks
			prevTicks = ticks
			if delta >= 0 {
				sample.CpuPercent = float64(delta) / clockTicksPerSecond / elapsed * 100
			}
		}

		if cg := readCgroupCPUStat(); cg.valid {
			sample.CpuQuotaCores = cg.quotaCores
			if prevCg.valid {
				sample.CfsPeriods = cg.periods - prevCg.periods
				sample.CfsThrottledPeriods = cg.throttledCount - prevCg.throttledCount
				sample.CfsThrottledUsec = cg.throttledUsec - prevCg.throttledUsec
			}
			prevCg = cg
		}

		// Java 8 lacks gc log, MX deltas cover it
		if gcTail != nil && gcTail.enabled {
			count, totalMs, maxMs := gcTail.drain()
			sample.Gc = &agentv1.GcWindow{Count: count, TotalMs: totalMs, MaxMs: maxMs}
		}

		sample.Psi = readPSI()

		s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_ProcSample{ProcSample: sample}})
	}
}

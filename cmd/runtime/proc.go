package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// procSampleInterval is how often process/cgroup/GC telemetry is reported.
const procSampleInterval = 15 * time.Second

// clockTicksPerSecond is the kernel USER_HZ used by /proc accounting.
const clockTicksPerSecond = 100

// cpuTimes reads the java process's cumulative user+system CPU ticks.
func cpuTimes(pid int) (int64, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	// comm (field 2) can contain spaces and parens; fields resume after the
	// last ')'. utime and stime are overall fields 14 and 15, i.e. rest[11]
	// and rest[12] with rest[0] being the state (field 3).
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

// rssMB reads the java process's resident set size in MiB.
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

// cgroupCPUStat is the cumulative CFS accounting for the container cgroup.
type cgroupCPUStat struct {
	periods        int64
	throttledCount int64
	throttledUsec  int64
	quotaCores     float64
	valid          bool
}

// readCgroupCPUStat reads CFS throttling counters, handling cgroup v2 and v1
// layouts as seen from inside a container.
func readCgroupCPUStat() cgroupCPUStat {
	// cgroup v2 (unified hierarchy)
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

	// cgroup v1
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

// readCgroupMemoryLimitMB reads the container memory limit in MiB (0 when
// unlimited), handling cgroup v2 and v1 layouts.
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
	// cgroup v1 reports "no limit" as a huge page-rounded value.
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

// runProcSampler periodically reports process CPU/RSS, cgroup throttling
// deltas, and GC pauses parsed from the GC log.
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

		if gcTail != nil {
			count, totalMs, maxMs := gcTail.drain()
			sample.Gc = &agentv1.GcWindow{Count: count, TotalMs: totalMs, MaxMs: maxMs}
		}

		s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_ProcSample{ProcSample: sample}})
	}
}

package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// localAgentPort is the loopback port the telemetry javaagent connects to.
// Container-internal only, advertised to the JVM via a system property.
const localAgentPort = 25585

// agentJarPath is where the runtime image ships the telemetry javaagent.
const agentJarPath = "/opt/discopanel/agent/disco-agent.jar"

// gcLogPath is where the JVM writes its unified GC log, tailed for pause
// telemetry and kept for crash forensics (rotated and size-capped).
func gcLogPath() string {
	return filepath.Join(dataDir, runtimespec.StateDir, "gc.log")
}

// buildJavaArgs assembles the full argv for java (argv[0] = "java").
func buildJavaArgs(spec *runtimespec.LaunchSpec, agentEnabled bool) ([]string, error) {
	args := []string{"java"}

	// Heap sizing: AUTO_MEMORY derives the heap from the container memory
	// limit (the JVM reads the cgroup itself); otherwise MEMORY sets both
	// bounds and INIT_MEMORY/MAX_MEMORY override individually.
	autoMemory := envBool("AUTO_MEMORY")
	memory := strings.TrimSpace(os.Getenv("MEMORY"))
	initMem := strings.TrimSpace(os.Getenv("INIT_MEMORY"))
	maxMem := strings.TrimSpace(os.Getenv("MAX_MEMORY"))
	if initMem == "" {
		initMem = memory
	}
	if maxMem == "" {
		maxMem = memory
	}
	if autoMemory {
		args = append(args, "-XX:InitialRAMPercentage=50", "-XX:MaxRAMPercentage=75")
	} else {
		if initMem != "" {
			args = append(args, "-Xms"+initMem)
		}
		if maxMem != "" {
			args = append(args, "-Xmx"+maxMem)
		}
	}

	// Heap size estimate for heap-dependent flag selection.
	heapMB := parseMemoryMB(maxMem)
	if autoMemory {
		if limit := readCgroupMemoryLimitMB(); limit > 0 {
			heapMB = int(float64(limit) * 0.75)
		}
	}

	// GC selection: ZGC beats MeowIce beats Aikar; Aikar is the default.
	useZGC := envBool("USE_ZGC_FLAGS")
	if useZGC && spec.JavaMajor < 21 {
		fmt.Printf("[discopanel-runtime] WARN: ZGC flags need Java 21+, falling back to G1\n")
		useZGC = false
	}
	useAikar := envBool("USE_AIKAR_FLAGS")
	useMeowice := envBool("USE_MEOWICE_FLAGS")
	if os.Getenv("USE_AIKAR_FLAGS") == "" && os.Getenv("USE_MEOWICE_FLAGS") == "" {
		useAikar = true
	}
	if useZGC {
		args = append(args, zgcFlags(spec.JavaMajor)...)
	} else if useMeowice {
		args = append(args, meowiceFlags(spec.JavaMajor)...)
	} else if useAikar {
		args = append(args, aikarFlags(heapMB)...)
	}

	// A CFS quota caps usable cores below what the JVM detects from the host,
	// which oversizes GC/compiler threads and amplifies throttling stalls.
	// Pin the processor count to the quota unless the user set one.
	if !strings.Contains(os.Getenv("JVM_XX_OPTS")+os.Getenv("JVM_OPTS"), "ActiveProcessorCount") {
		if cg := readCgroupCPUStat(); cg.quotaCores > 0 {
			args = append(args, fmt.Sprintf("-XX:ActiveProcessorCount=%d", int(math.Ceil(cg.quotaCores))))
		}
	}
	if envBool("USE_FLARE_FLAGS") {
		args = append(args, "-XX:+UnlockDiagnosticVMOptions", "-XX:+DebugNonSafepoints")
	}
	if envBool("USE_SIMD_FLAGS") && spec.JavaMajor >= 16 {
		args = append(args, "--add-modules=jdk.incubator.vector")
	}

	if envBool("ENABLE_JMX") {
		jmxHost := os.Getenv("JMX_HOST")
		args = append(args,
			"-Dcom.sun.management.jmxremote",
			"-Dcom.sun.management.jmxremote.port=7091",
			"-Dcom.sun.management.jmxremote.rmi.port=7091",
			"-Dcom.sun.management.jmxremote.local.only=false",
			"-Dcom.sun.management.jmxremote.authenticate=false",
			"-Dcom.sun.management.jmxremote.ssl=false",
		)
		if jmxHost != "" {
			args = append(args, "-Djava.rmi.server.hostname="+jmxHost)
		}
	}

	// Unified GC logging (Java 11+): tailed by the supervisor for pause
	// telemetry, size-capped so it can stay on unconditionally.
	if spec.JavaMajor >= 11 {
		args = append(args, fmt.Sprintf("-Xlog:gc*:file=%s:time,uptime:filecount=2,filesize=5M", gcLogPath()))
	}

	// Log4Shell mitigation; a no-op on patched/modern versions.
	args = append(args, "-Dlog4j2.formatMsgNoLookups=true")

	// One loader-agnostic javaagent covers TPS and JVM telemetry everywhere
	if agentEnabled {
		args = append(args, fmt.Sprintf("-Ddiscopanel.agent.port=%d", localAgentPort))
		if _, err := os.Stat(agentJarPath); err == nil {
			args = append(args, "-javaagent:"+agentJarPath)
		} else {
			fmt.Printf("[discopanel-runtime] WARN: %s missing from image, JVM telemetry disabled\n", agentJarPath)
		}
	}

	if tz := os.Getenv("TZ"); tz != "" {
		args = append(args, "-Duser.timezone="+tz)
	}

	args = append(args, strings.Fields(os.Getenv("JVM_OPTS"))...)
	args = append(args, strings.Fields(os.Getenv("JVM_XX_OPTS"))...)
	for _, dd := range splitList(os.Getenv("JVM_DD_OPTS")) {
		args = append(args, "-D"+dd)
	}

	extraArgs := strings.Fields(os.Getenv("EXTRA_ARGS"))

	switch spec.Kind {
	case runtimespec.LaunchKindJar:
		if spec.Jar == "" {
			return nil, fmt.Errorf("launch spec kind=jar but no jar path set")
		}
		args = append(args, "-jar", spec.Jar)
		args = append(args, extraArgs...)
		if !spec.NoGui {
			args = append(args, "nogui")
		}
	case runtimespec.LaunchKindArgsFile:
		if spec.ArgsFile == "" {
			return nil, fmt.Errorf("launch spec kind=args-file but no args file set")
		}
		args = append(args, "@"+spec.ArgsFile)
		args = append(args, extraArgs...)
		if !spec.NoGui {
			args = append(args, "nogui")
		}
	case runtimespec.LaunchKindCustom:
		if spec.Exec == "" {
			return nil, fmt.Errorf("launch spec kind=custom but no exec command set")
		}
		args = append(args, strings.Fields(spec.Exec)...)
		args = append(args, extraArgs...)
	default:
		return nil, fmt.Errorf("unknown launch kind %q", spec.Kind)
	}

	return args, nil
}

// aikarFlags returns Aikar's G1GC tuning flags, switching to the large-heap
// variant at >= 12GB max heap (per https://docs.papermc.io/paper/aikars-flags).
func aikarFlags(heapMB int) []string {
	flags := []string{
		"-XX:+UseG1GC",
		"-XX:+ParallelRefProcEnabled",
		"-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+DisableExplicitGC",
		"-XX:+AlwaysPreTouch",
		"-XX:G1HeapWastePercent=5",
		"-XX:G1MixedGCCountTarget=4",
		"-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:G1RSetUpdatingPauseTimePercent=5",
		"-XX:SurvivorRatio=32",
		"-XX:+PerfDisableSharedMem",
		"-XX:MaxTenuringThreshold=1",
		"-Dusing.aikars.flags=https://mcflags.emc.gs",
		"-Daikars.new.flags=true",
	}
	if heapMB >= 12288 {
		flags = append(flags,
			"-XX:G1NewSizePercent=40",
			"-XX:G1MaxNewSizePercent=50",
			"-XX:G1HeapRegionSize=16M",
			"-XX:G1ReservePercent=15",
			"-XX:InitiatingHeapOccupancyPercent=20",
		)
	} else {
		flags = append(flags,
			"-XX:G1NewSizePercent=30",
			"-XX:G1MaxNewSizePercent=40",
			"-XX:G1HeapRegionSize=8M",
			"-XX:G1ReservePercent=20",
			"-XX:InitiatingHeapOccupancyPercent=15",
		)
	}
	return flags
}

// zgcFlags returns generational ZGC flags for Java 21+. ZGenerational is a
// product flag on 21/22, the default (and deprecated) on 23+, so it is only
// passed where it means something.
func zgcFlags(javaMajor int) []string {
	flags := []string{
		"-XX:+UseZGC",
		"-XX:+AlwaysPreTouch",
		"-XX:+DisableExplicitGC",
		"-XX:+PerfDisableSharedMem",
	}
	if javaMajor <= 22 {
		flags = append(flags, "-XX:+ZGenerational")
	}
	return flags
}

// meowiceFlags returns MeowIce's G1GC flag set. IgnoreUnrecognizedVMOptions is
// prepended because parts of the set are only recognized on newer JDKs or
// GraalVM; the JVM must still boot everywhere.
func meowiceFlags(javaMajor int) []string {
	flags := []string{
		"-XX:+IgnoreUnrecognizedVMOptions",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+UnlockDiagnosticVMOptions",
		"-XX:+UseG1GC",
		"-XX:MaxGCPauseMillis=200",
		"-XX:+DisableExplicitGC",
		"-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent=28",
		"-XX:G1MaxNewSizePercent=50",
		"-XX:G1HeapRegionSize=16M",
		"-XX:G1ReservePercent=15",
		"-XX:G1MixedGCCountTarget=3",
		"-XX:InitiatingHeapOccupancyPercent=20",
		"-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:SurvivorRatio=32",
		"-XX:G1HeapWastePercent=5",
		"-XX:+PerfDisableSharedMem",
		"-XX:G1SATBBufferEnqueueingThresholdPercent=30",
		"-XX:G1ConcMarkStepDurationMillis=5",
		"-XX:G1RSetUpdatingPauseTimePercent=0",
		"-XX:-DontCompileHugeMethods",
		"-XX:MaxNodeLimit=240000",
		"-XX:NodeLimitFudgeFactor=8000",
		"-XX:ReservedCodeCacheSize=400M",
		"-XX:NonNMethodCodeHeapSize=12M",
		"-XX:ProfiledCodeHeapSize=194M",
		"-XX:NonProfiledCodeHeapSize=194M",
		"-XX:+UseStringDeduplication",
		"-XX:+UseFastJNIAccessors",
		"-XX:+OptimizeStringConcat",
		"-XX:+UseCompressedOops",
		"-XX:+UseThreadPriorities",
		"-XX:+OmitStackTraceInFastThrow",
		"-XX:+RewriteBytecodes",
		"-XX:+RewriteFrequentPairs",
		"-XX:+EliminateLocks",
		"-XX:+DoEscapeAnalysis",
		"-XX:+OptimizeFill",
		"-XX:+UseFPUForSpilling",
		"-XX:+UseNewLongLShift",
		"-XX:+UseVectorCmov",
		"-XX:+UseXMMForArrayCopy",
		"-XX:+UseXmmI2D",
		"-XX:+UseXmmI2F",
		"-XX:+UseXmmLoadAndClearUpper",
		"-XX:+UseXmmRegToRegMoveAll",
		"-XX:+UseTransparentHugePages",
		"-XX:+UseNUMA",
		"-Djdk.nio.maxCachedBufferSize=262144",
	}
	if javaMajor >= 16 {
		flags = append(flags, "--add-modules=jdk.incubator.vector")
	}
	return flags
}

// parseMemoryMB parses values like "4096M", "12G", "2048" (MB assumed) to MB.
// Unparseable values return 0 with a console warning: heap-dependent flag
// selection then uses the conservative small-heap defaults.
func parseMemoryMB(s string) int {
	orig := s
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "G"):
		mult = 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		s = strings.TrimSuffix(s, "K")
		if v, err := strconv.Atoi(s); err == nil {
			return v / 1024
		}
		fmt.Printf("[discopanel-runtime] WARN: unparseable memory value %q, treating as unset\n", orig)
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		fmt.Printf("[discopanel-runtime] WARN: unparseable memory value %q, treating as unset\n", orig)
		return 0
	}
	return v * mult
}

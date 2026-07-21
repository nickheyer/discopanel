package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// Where the runtime image ships the telemetry javaagent
const agentJarPath = "/opt/discopanel/agent/disco-agent.jar"

// Where the JVM writes its unified GC log
func gcLogPath() string {
	return filepath.Join(dataDir, runtimespec.StateDir, "gc.log")
}

// Assembles the full java argv, including javaagent port
func buildJavaArgs(spec *v1.LaunchSpec, agentPort int) ([]string, error) {
	args := []string{"java"}

	// AUTO_MEMORY derives heap from container memory limit
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

	// Heap size estimate for heap-dependent flag selection
	heapMB := runtimespec.ParseMemoryMB(maxMem)
	if heapMB == 0 && maxMem != "" {
		fmt.Printf("[discopanel-runtime] WARN: unparseable memory value %q, treating as unset\n", maxMem)
	}
	if autoMemory {
		if limit := readCgroupMemoryLimitMB(); limit > 0 {
			heapMB = int(float64(limit) * 0.75)
		}
	}

	userOpts := os.Getenv("JVM_XX_OPTS") + " " + os.Getenv("JVM_OPTS")

	// ZGC beats MeowIce beats Aikar, Aikar is default
	useZGC := envBool("USE_ZGC_FLAGS")
	if useZGC && spec.JavaMajor < 21 {
		fmt.Printf("[discopanel-runtime] WARN: ZGC flags need Java 21+, falling back to G1\n")
		useZGC = false
	}
	useAikar := envBool("USE_AIKAR_FLAGS")
	useMeowice := envBool("USE_MEOWICE_FLAGS")
	if os.Getenv("USE_AIKAR_FLAGS") == "" && os.Getenv("USE_MEOWICE_FLAGS") == "" && !userSelectsGC(userOpts) {
		useAikar = true
	}
	if useZGC {
		args = append(args, zgcFlags(int(spec.JavaMajor))...)
	} else if useMeowice {
		args = append(args, meowiceFlags(int(spec.JavaMajor))...)
	} else if useAikar {
		args = append(args, aikarFlags(heapMB)...)
	}

	// Pins the processor count to the detected cgroup quota
	if !strings.Contains(userOpts, "ActiveProcessorCount") {
		if cg := readCgroupCPUStat(); cg.quotaCores > 0 {
			args = append(args, fmt.Sprintf("-XX:ActiveProcessorCount=%d", int(math.Ceil(cg.quotaCores))))
		}
	}

	// Huge pages cut TLB misses on multi GB heaps
	if !strings.Contains(userOpts, "TransparentHugePages") && !strings.Contains(userOpts, "LargePages") && !useMeowice {
		args = append(args, "-XX:+UseTransparentHugePages")
	}

	graalVariant := os.Getenv("DISCO_JVM_VARIANT") == "graal"

	// Compact headers shrink objects, skipped for unverified Graal support
	if spec.JavaMajor >= 25 && !graalVariant && !strings.Contains(userOpts, "CompactObjectHeaders") {
		args = append(args, "-XX:+UseCompactObjectHeaders")
	}

	// Community proven Graal JIT tuning for game workloads
	if graalVariant && !strings.Contains(userOpts, "EagerJVMCI") {
		args = append(args, "-XX:+EagerJVMCI", "-Djdk.graal.TuneInlinerExploration=1")
	}

	agentAttached := false
	if agentPort > 0 {
		if _, err := os.Stat(agentJarPath); err == nil {
			agentAttached = true
		} else {
			fmt.Printf("[discopanel-runtime] WARN: %s missing from image, JVM telemetry disabled\n", agentJarPath)
		}
	}

	// App CDS is unsound under class transformation
	if err := os.Remove(filepath.Join(dataDir, runtimespec.StateDir, "cds.jsa")); err == nil {
		fmt.Printf("[discopanel-runtime] removed stale class data archive\n")
	}

	// Stale gc log from the last run must never replay
	_ = os.Remove(gcLogPath())

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

	// Unified GC logging tailed by supervisor, size-capped
	if spec.JavaMajor >= 11 {
		args = append(args, fmt.Sprintf("-Xlog:gc*:file=%s:time,uptime:filecount=2,filesize=5M", gcLogPath()))
	}

	// Log4Shell mitigation, a no-op on patched versions
	args = append(args, "-Dlog4j2.formatMsgNoLookups=true")

	// One loader-agnostic javaagent covers TPS and JVM telemetry everywhere
	if agentPort > 0 {
		args = append(args, fmt.Sprintf("-Ddiscopanel.agent.port=%d", agentPort))
		if agentAttached {
			args = append(args, "-javaagent:"+agentJarPath)
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

	// Containers are always headless, jar launches get nogui
	switch spec.Kind {
	case v1.LaunchKind_LAUNCH_KIND_JAR:
		if spec.Jar == "" {
			return nil, fmt.Errorf("launch spec kind=jar but no jar path set")
		}
		args = append(args, "-jar", spec.Jar)
		args = append(args, extraArgs...)
		args = append(args, "nogui")
	case v1.LaunchKind_LAUNCH_KIND_ARGS_FILE:
		if spec.ArgsFile == "" {
			return nil, fmt.Errorf("launch spec kind=args-file but no args file set")
		}
		args = append(args, "@"+spec.ArgsFile)
		args = append(args, extraArgs...)
		args = append(args, "nogui")
	case v1.LaunchKind_LAUNCH_KIND_CUSTOM:
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

// Reports whether user jvm opts already pick a collector
func userSelectsGC(opts string) bool {
	for _, f := range strings.Fields(opts) {
		if strings.HasPrefix(f, "-XX:+Use") && strings.HasSuffix(f, "GC") {
			return true
		}
	}
	return false
}

// Returns Aikar's G1GC tuning flags for the given heap
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

// Returns generational ZGC flags for Java 21+
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

// Returns MeowIce's G1GC flag set for the JVM
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

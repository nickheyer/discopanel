// discopanel-runtime entrypoint: launches a Minecraft server prepared by the
// DiscoPanel provisioner. The data directory is provisioned panel-side; this
// program only assembles the java command line from environment variables and
// the launch spec, fixes ownership, drops privileges, and execs java as PID 1.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

const dataDir = "/data"

func main() {
	spec, err := runtimespec.ReadLaunchSpec(dataDir)
	if err != nil {
		fatal("no launch spec at %s (%v) - this container must be provisioned and started by DiscoPanel", runtimespec.LaunchPath(dataDir), err)
	}

	// Banner first: everything below can take a while on big modpacks and the
	// console must never sit silent.
	fmt.Printf("[discopanel-runtime] %s %s (%s, MC %s)\n", spec.Kind, launchTarget(spec), spec.Loader, spec.MCVersion)

	uid := getEnvInt("UID", 1000)
	gid := getEnvInt("GID", 1000)

	if os.Getuid() == 0 && uid > 0 {
		ensureOwnership(dataDir, uid, gid)
	}

	args, err := buildJavaArgs(spec)
	if err != nil {
		fatal("%v", err)
	}

	javaPath, err := exec.LookPath("java")
	if err != nil {
		fatal("java not found: %v", err)
	}

	if err := os.Chdir(dataDir); err != nil {
		fatal("failed to chdir to %s: %v", dataDir, err)
	}

	fmt.Printf("[discopanel-runtime] exec: java %s\n", strings.Join(args[1:], " "))

	if os.Getuid() == 0 && uid > 0 {
		if err := syscall.Setgroups([]int{gid}); err != nil {
			fatal("failed to set groups: %v", err)
		}
		if err := syscall.Setgid(gid); err != nil {
			fatal("failed to setgid: %v", err)
		}
		if err := syscall.Setuid(uid); err != nil {
			fatal("failed to setuid: %v", err)
		}
	}

	// Exec so java becomes PID 1 and receives SIGTERM directly on `docker stop`,
	// which triggers the server's graceful shutdown hook (world save).
	if err := syscall.Exec(javaPath, args, os.Environ()); err != nil {
		fatal("failed to exec java: %v", err)
	}
}

func launchTarget(spec *runtimespec.LaunchSpec) string {
	switch spec.Kind {
	case runtimespec.LaunchKindJar:
		return spec.Jar
	case runtimespec.LaunchKindArgsFile:
		return "@" + spec.ArgsFile
	default:
		return spec.Exec
	}
}

// buildJavaArgs assembles the full argv for java (argv[0] = "java").
func buildJavaArgs(spec *runtimespec.LaunchSpec) ([]string, error) {
	args := []string{"java"}

	// Heap sizing: MEMORY sets both bounds, INIT_MEMORY/MAX_MEMORY override individually.
	memory := strings.TrimSpace(os.Getenv("MEMORY"))
	initMem := strings.TrimSpace(os.Getenv("INIT_MEMORY"))
	maxMem := strings.TrimSpace(os.Getenv("MAX_MEMORY"))
	if initMem == "" {
		initMem = memory
	}
	if maxMem == "" {
		maxMem = memory
	}
	if initMem != "" {
		args = append(args, "-Xms"+initMem)
	}
	if maxMem != "" {
		args = append(args, "-Xmx"+maxMem)
	}

	// Aikars tuned GC is the default - mutually exclusive with meowice
	useAikar := envBool("USE_AIKAR_FLAGS")
	useMeowice := envBool("USE_MEOWICE_FLAGS")
	if os.Getenv("USE_AIKAR_FLAGS") == "" && os.Getenv("USE_MEOWICE_FLAGS") == "" {
		useAikar = true
	}
	if useMeowice {
		args = append(args, meowiceFlags(spec.JavaMajor)...)
	} else if useAikar {
		args = append(args, aikarFlags(maxMem)...)
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

	// Log4Shell mitigation; a no-op on patched/modern versions.
	args = append(args, "-Dlog4j2.formatMsgNoLookups=true")

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
func aikarFlags(maxMem string) []string {
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
	if parseMemoryMB(maxMem) >= 12288 {
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

// ensureOwnership chowns the data tree when it isn't already owned by the
// target uid, so bind-mounted files written by the panel are writable after
// the privilege drop. The walk is skipped when the root is already correct.
func ensureOwnership(dir string, uid, gid int) {
	info, err := os.Stat(dir)
	if err != nil {
		return
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid && int(st.Gid) == gid {
		return
	}
	fmt.Printf("[discopanel-runtime] fixing file ownership (%d:%d), this can take a moment on large packs...\n", uid, gid)
	start := time.Now()
	files := 0
	filepath.Walk(dir, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		os.Lchown(name, uid, gid)
		files++
		return nil
	})
	fmt.Printf("[discopanel-runtime] ownership fixed (%d files in %s)\n", files, time.Since(start).Round(time.Millisecond))
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "true" || v == "1" || v == "yes"
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// splitList splits on commas and newlines, trimming empties.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseMemoryMB parses values like "4096M", "12G", "2048" (MB assumed) to MB.
func parseMemoryMB(s string) int {
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
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v * mult
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[discopanel-runtime] FATAL: "+format+"\n", args...)
	os.Exit(1)
}

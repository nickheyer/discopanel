// discopanel-runtime: supervisor entrypoint for Minecraft server containers.
// The data directory is provisioned panel-side; this program assembles the
// java command line, fixes ownership, then runs java as a supervised child
// (dropping privileges via the child's credentials). Staying resident as PID 1
// lets it forward signals for graceful shutdown, mirror console output while
// watching for readiness and crashes, feed console commands to java stdin,
// sample process/cgroup/GC telemetry, and relay everything to the panel over
// the agent session (see agent.go). The child's exit code is propagated so
// docker restart policies keep working.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

const dataDir = "/data"

// runtimeVersion is stamped via -ldflags at build time.
var runtimeVersion = "dev"

// readyPattern matches the vanilla/Paper/Forge/NeoForge server-done line,
// e.g. `[Server thread/INFO]: Done (9.418s)! For help, type "help"`.
var readyPattern = regexp.MustCompile(`Done \([0-9.,]+ ?s(?:econds)?\)`)

func main() {
	spec, err := runtimespec.ReadLaunchSpec(dataDir)
	if err != nil {
		fatal("no launch spec at %s (%v) - this container must be provisioned and started by DiscoPanel", runtimespec.LaunchPath(dataDir), err)
	}

	// Banner first: everything below can take a while on big modpacks and the
	// console must never sit silent.
	fmt.Printf("[discopanel-runtime] %s %s (%s, MC %s)\n", spec.Kind, launchTarget(spec), spec.Loader, spec.MCVersion)

	agentSpec, err := runtimespec.ReadAgentSpec(dataDir)
	if err != nil {
		fmt.Printf("[discopanel-runtime] WARN: agent spec unreadable (%v), telemetry disabled\n", err)
		agentSpec = nil
	}
	agentEnabled := agentSpec != nil && agentSpec.Enabled && agentSpec.PanelURL != "" && agentSpec.Token != ""

	uid := getEnvInt("UID", 1000)
	gid := getEnvInt("GID", 1000)

	if os.Getuid() == 0 && uid > 0 {
		ensureOwnership(dataDir, uid, gid)
	}

	args, err := buildJavaArgs(spec, agentEnabled)
	if err != nil {
		fatal("%v", err)
	}

	javaPath, err := exec.LookPath("java")
	if err != nil {
		fatal("java not found: %v", err)
	}

	fmt.Printf("[discopanel-runtime] exec: java %s\n", strings.Join(args[1:], " "))

	cmd := exec.Command(javaPath, args[1:]...)
	cmd.Dir = dataDir
	cmd.Env = os.Environ()
	if os.Getuid() == 0 && uid > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{uint32(gid)}},
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fatal("failed to open stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fatal("failed to open stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fatal("failed to open stderr pipe: %v", err)
	}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		fatal("failed to start java: %v", err)
	}

	sup := &supervisor{
		spec:      spec,
		agentSpec: agentSpec,
		stdin:     stdin,
		startedAt: startedAt,
		pid:       cmd.Process.Pid,
	}
	sup.events = newConsoleEvents(sup)

	// Forward termination signals to java so `docker stop` still triggers the
	// server's graceful shutdown hook (world save).
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for sig := range sigCh {
			sup.mu.Lock()
			sup.stopRequested = true
			sup.mu.Unlock()
			sup.send(msgStopping())
			_ = cmd.Process.Signal(sig)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sup.mirrorConsole(stdout, os.Stdout)
	}()
	go func() {
		defer wg.Done()
		sup.mirrorConsole(stderr, os.Stderr)
	}()

	if agentEnabled {
		gcTail := newGCLogTail(gcLogPath(), spec.JavaMajor)
		go gcTail.run(sup.done())
		go sup.runProcSampler(gcTail)
		go sup.runLocalListener()
		go sup.runPanelSession()
		go sup.runRosterTicker()
	}

	waitErr := cmd.Wait()
	wg.Wait()

	exitCode := exitCodeOf(cmd, waitErr)
	sup.reportExit(exitCode)
	sup.close()

	if exitCode != 0 {
		fmt.Printf("[discopanel-runtime] server process exited with code %d\n", exitCode)
	}
	os.Exit(exitCode)
}

// supervisor holds the shared state of a running server process.
type supervisor struct {
	spec      *runtimespec.LaunchSpec
	agentSpec *runtimespec.AgentSpec
	startedAt time.Time
	pid       int
	events    *consoleEvents

	stdinMu sync.Mutex
	stdin   interface{ Write([]byte) (int, error) }

	mu            sync.Mutex
	ready         bool
	readySeconds  float64
	stopRequested bool          // termination signal was forwarded to java
	session       *panelSession // active panel stream, nil when disconnected
	closed        chan struct{} // closed once, on process exit
	closeOnce     sync.Once
}

func (s *supervisor) done() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed == nil {
		s.closed = make(chan struct{})
	}
	return s.closed
}

func (s *supervisor) close() {
	ch := s.done()
	s.closeOnce.Do(func() { close(ch) })
}

// writeConsole feeds one command line to the java process stdin.
func (s *supervisor) writeConsole(line string) error {
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	_, err := s.stdin.Write([]byte(line + "\n"))
	return err
}

// broadcastChat shows a chat line in game via tellraw (any loader, 1.7.2+).
func (s *supervisor) broadcastChat(sender, message string) error {
	component, err := json.Marshal(map[string]string{"text": "<" + sender + "> " + message})
	if err != nil {
		return err
	}
	return s.writeConsole("tellraw @a " + string(component))
}

// mirrorConsole copies child output to the container console line by line,
// feeding each line to the event parser. Overlong lines are dropped from
// parsing (never from output) so memory stays bounded.
func (s *supervisor) mirrorConsole(r interface{ Read([]byte) (int, error) }, w *os.File) {
	buf := make([]byte, 64*1024)
	var line []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = w.Write(chunk)
			line = append(line, chunk...)
			for {
				idx := bytes.IndexByte(line, '\n')
				if idx < 0 {
					if len(line) > 512*1024 {
						line = line[:0]
					}
					break
				}
				s.events.handleLine(string(line[:idx]))
				line = line[idx+1:]
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *supervisor) isReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// sendRoster pushes the authoritative online player list to the panel.
func (s *supervisor) sendRoster() {
	s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Roster{
		Roster: &agentv1.Roster{OnlinePlayers: s.events.roster()},
	}})
}

// runRosterTicker keeps the panel-side roster freshness window alive.
func (s *supervisor) runRosterTicker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done():
			return
		case <-ticker.C:
			s.sendRoster()
		}
	}
}


// markReady records readiness (from the console Done line or the mod's
// lifecycle hook, whichever fires first) and notifies the panel.
func (s *supervisor) markReady(startupSeconds float64) {
	s.mu.Lock()
	if s.ready {
		s.mu.Unlock()
		return
	}
	s.ready = true
	s.readySeconds = startupSeconds
	s.mu.Unlock()
	s.send(msgReady(startupSeconds))
}

// reportExit sends the exit report (with crash forensics) to the panel while
// the session is still alive.
func (s *supervisor) reportExit(exitCode int) {
	s.mu.Lock()
	requested := s.stopRequested
	s.mu.Unlock()
	reportPath, excerpt := findCrashReport(s.startedAt)
	crashed := isCrash(exitCode, requested, reportPath)
	s.send(msgExited(exitCode, crashed, reportPath, excerpt))
	// Give the sender goroutine a moment to flush before the process exits.
	time.Sleep(500 * time.Millisecond)
}

// Decides crash, requested stops never count without a crash report
func isCrash(exitCode int, stopRequested bool, reportPath string) bool {
	if reportPath != "" {
		return true
	}
	return exitCode != 0 && !stopRequested
}

// findCrashReport locates the newest crash report written after start and
// returns its data-dir-relative path plus a capped excerpt.
func findCrashReport(since time.Time) (string, string) {
	dir := filepath.Join(dataDir, "crash-reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	var newest string
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = e.Name()
		}
	}
	if newest == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(dir, newest))
	if err != nil {
		return filepath.Join("crash-reports", newest), ""
	}
	const maxExcerpt = 4096
	if len(data) > maxExcerpt {
		data = data[:maxExcerpt]
	}
	return filepath.Join("crash-reports", newest), string(data)
}

func exitCodeOf(cmd *exec.Cmd, waitErr error) int {
	if waitErr == nil {
		return 0
	}
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return cmd.ProcessState.ExitCode()
	}
	return 1
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

// ensureOwnership chowns the data tree when it isn't already owned by the
// target uid, so bind-mounted files written by the panel are writable by the
// java child. The walk is skipped when the root is already correct, and the
// chown calls are spread over a small worker pool for large modpacks.
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

	paths := make(chan string, 1024)
	var files int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range paths {
				_ = os.Lchown(name, uid, gid)
			}
		}()
	}
	_ = filepath.WalkDir(dir, func(name string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		paths <- name
		files++
		return nil
	})
	close(paths)
	wg.Wait()
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

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[discopanel-runtime] FATAL: "+format+"\n", args...)
	os.Exit(1)
}

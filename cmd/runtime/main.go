// Supervises the java server process as container PID 1
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"google.golang.org/protobuf/encoding/protojson"
)

const dataDir = "/data"

// Stamped via -ldflags at build time
var runtimeVersion = "dev"

// Matches the server Done line on every version and loader
var readyPattern = regexp.MustCompile(`Done \([0-9.,]+ ?n?s(?:econds)?\)`)

// Byte cap shared by report and console excerpts
const maxExcerpt = 4096

func main() {
	spec, err := runtimespec.ReadLaunchSpec(dataDir)
	if err != nil {
		fatal("no launch spec at %s (%v) - this container must be provisioned and started by DiscoPanel", runtimespec.LaunchPath(dataDir), err)
	}
	if spec.Version > runtimespec.LaunchSpecVersion {
		fmt.Printf("[discopanel-runtime] WARN: launch spec version %d is newer than this runtime understands (%d), update the runtime image\n",
			spec.Version, runtimespec.LaunchSpecVersion)
	}

	// Print banner first so console never sits silent
	fmt.Printf("[discopanel-runtime] %s %s (%s, MC %s)\n", spec.Kind, launchTarget(spec), spec.Loader, spec.MCVersion)

	agentSpec, err := runtimespec.ReadAgentSpec(dataDir)
	if err != nil {
		fmt.Printf("[discopanel-runtime] WARN: agent spec unreadable (%v), telemetry disabled\n", err)
		agentSpec = nil
	}
	if agentSpec != nil && agentSpec.Version > runtimespec.AgentSpecVersion {
		fmt.Printf("[discopanel-runtime] WARN: agent spec version %d is newer than this runtime understands (%d), update the runtime image\n",
			agentSpec.Version, runtimespec.AgentSpecVersion)
	}
	agentEnabled := agentSpec != nil && agentSpec.Enabled && agentSpec.PanelURL != "" && agentSpec.Token != ""

	uid := getEnvInt("UID", 1000)
	gid := getEnvInt("GID", 1000)

	if os.Getuid() == 0 && uid > 0 {
		ensureOwnership(dataDir, uid, gid)
		protectAgentSpec()
	}

	// Bind loopback port early so javaagent learns real port
	var agentListener net.Listener
	agentPort := 0
	if agentEnabled {
		if ln, lerr := net.Listen("tcp", "127.0.0.1:0"); lerr == nil {
			agentListener = ln
			agentPort = ln.Addr().(*net.TCPAddr).Port
		} else {
			fmt.Printf("[discopanel-runtime] WARN: local agent listener failed (%v), JVM telemetry disabled\n", lerr)
		}
	}

	args, err := buildJavaArgs(spec, agentPort)
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
		spec:        spec,
		agentSpec:   agentSpec,
		stdin:       stdin,
		startedAt:   startedAt,
		pid:         cmd.Process.Pid,
		proc:        cmd.Process,
		oomBaseline: readOOMKills(),
		pendingExit: readExitReport(dataDir),
	}
	sup.events = newConsoleEvents(sup)

	go sup.runTickThreadBooster()
	go sup.watchCrashReports()

	// Forward termination signals to java for graceful shutdown
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
		if agentListener != nil {
			go sup.runLocalListener(agentListener)
		}
		if mgmt := readMgmtConfig(dataDir); mgmt != nil {
			go sup.runManagementClient(mgmt)
		}
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

// Holds the shared state of a running server process
type supervisor struct {
	spec        *runtimespec.LaunchSpec
	agentSpec   *runtimespec.AgentSpec
	startedAt   time.Time
	pid         int
	events      *consoleEvents
	oomBaseline int64 // Cgroup oom_kill count at process start

	lastOutputAt atomic.Int64 // Unix nanos of last console output

	stdinMu sync.Mutex
	stdin   interface{ Write([]byte) (int, error) }

	mu               sync.Mutex
	ready            bool
	readyAt          time.Time // Set once alongside ready
	readySeconds     float64
	stopRequested    bool                // Termination signal was forwarded to java
	session          *panelSession       // Active panel stream, nil when disconnected
	fatal            *agentv1.FatalError // Best structured JVM fatal error
	captureArmed     bool                // Log watcher confirmed at least one hook
	bootFailedAt     time.Time           // First boot failure signal, zero while healthy
	bootFailed       bool                // Watchdog ended a hung boot-failed JVM
	survivedReportAt time.Time           // Newest crash report the JVM outlived
	pendingExit      *exitReport         // Unacked exit report, replayed until acked
	lagDebtMs        float64             // Debt from the newest lag line
	lagAt            time.Time           // When the newest lag line printed
	lagSpacing       time.Duration       // Gap between the last two lag lines
	proc             *os.Process         // The supervised java process
	closed           chan struct{}       // Closed once, on process exit
	closeOnce        sync.Once
}

func (s *supervisor) setFatalError(fatal *agentv1.FatalError) {
	if fatal == nil {
		return
	}
	s.mu.Lock()
	if !fatal.GetUncaught() && s.ready {
		s.mu.Unlock()
		return
	}
	if s.fatal == nil || len(fatal.GetFailedMods()) > 0 || len(s.fatal.GetFailedMods()) == 0 {
		s.fatal = fatal
	}
	ready := s.ready
	s.mu.Unlock()
	if !ready && len(fatal.GetFailedMods()) > 0 {
		s.armBootFailure()
	}
}

func (s *supervisor) fatalError() *agentv1.FatalError {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatal
}

// Prints one line so capture misses stay diagnosable
func (s *supervisor) markCaptureArmed(contexts int32) {
	s.mu.Lock()
	first := !s.captureArmed
	s.captureArmed = true
	s.mu.Unlock()
	if first {
		fmt.Printf("[discopanel-runtime] crash capture armed (%d log contexts hooked)\n", contexts)
	}
}

const (
	bootFailGrace     = 30 * time.Second
	killEscalation    = 20 * time.Second
	deathWatchHorizon = 5 * time.Minute
	consoleIdleWindow = 60 * time.Second
	cpuIdleWindow     = 30 * time.Second
	cpuIdleCores      = 0.02
	evidenceInterval  = 5 * time.Second
)

func (s *supervisor) armBootFailure() {
	s.mu.Lock()
	if s.ready || !s.bootFailedAt.IsZero() {
		s.mu.Unlock()
		return
	}
	s.bootFailedAt = time.Now()
	s.mu.Unlock()
	fmt.Printf("[discopanel-runtime] fatal boot error, ending the JVM once it goes idle\n")
	go s.endBootFailedAfterGrace()
}

// Crash reports arm a death watch, survivors stay watched
func (s *supervisor) watchCrashReports() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var watermark time.Time
	for {
		select {
		case <-s.done():
			return
		case <-ticker.C:
		}
		since := s.startedAt
		readyAt := s.readyTime()
		if !readyAt.IsZero() && readyAt.After(since) {
			since = readyAt
		}
		if watermark.After(since) {
			since = watermark
		}
		reportPath, _, reportAt := findCrashReport(since)
		if reportPath == "" {
			continue
		}
		watermark = reportAt
		if readyAt.IsZero() {
			s.armBootFailure()
			continue
		}
		if !s.endWedgedCrash() {
			s.markReportSurvived(reportAt)
		}
	}
}

// Time since the java process last wrote console output
func (s *supervisor) consoleIdleFor() time.Duration {
	last := s.lastOutputAt.Load()
	if last == 0 {
		return time.Since(s.startedAt)
	}
	return time.Since(time.Unix(0, last))
}

// Waits for console silence plus CPU idle, false means survival
func (s *supervisor) awaitDeathEvidence() bool {
	type cpuPoint struct {
		at    time.Time
		ticks int64
	}
	var window []cpuPoint
	deadline := time.Now().Add(deathWatchHorizon)
	ticker := time.NewTicker(evidenceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done():
			return false
		case <-ticker.C:
		}
		now := time.Now()
		if now.After(deadline) {
			return false
		}
		if ticks, ok := cpuTimes(s.pid); ok {
			window = append(window, cpuPoint{at: now, ticks: ticks})
		}
		for len(window) > 0 && now.Sub(window[0].at) > cpuIdleWindow+evidenceInterval {
			window = window[1:]
		}
		if s.consoleIdleFor() < consoleIdleWindow || len(window) < 2 {
			continue
		}
		span := window[len(window)-1].at.Sub(window[0].at)
		if span < cpuIdleWindow {
			continue
		}
		cores := float64(window[len(window)-1].ticks-window[0].ticks) / clockTicksPerSecond / span.Seconds()
		if cores < cpuIdleCores {
			return true
		}
	}
}

// Ends a crashed JVM only when it stops showing life
func (s *supervisor) endWedgedCrash() bool {
	fmt.Printf("[discopanel-runtime] crash report written, watching the JVM for death evidence\n")
	if !s.awaitDeathEvidence() {
		if !s.exiting() {
			fmt.Printf("[discopanel-runtime] server survived its crash report, leaving it running\n")
		}
		return false
	}
	s.mu.Lock()
	proc := s.proc
	s.mu.Unlock()
	if proc == nil {
		return false
	}
	fmt.Printf("[discopanel-runtime] crashed JVM is wedged (console and CPU idle), ending it\n")
	_ = proc.Signal(syscall.SIGTERM)
	select {
	case <-s.done():
	case <-time.After(killEscalation):
		_ = proc.Kill()
	}
	return true
}

// Keeps a survived report from tainting the eventual exit
func (s *supervisor) markReportSurvived(at time.Time) {
	s.mu.Lock()
	if at.After(s.survivedReportAt) {
		s.survivedReportAt = at
	}
	s.mu.Unlock()
}

func (s *supervisor) endBootFailedAfterGrace() {
	select {
	case <-s.done():
		return
	case <-time.After(bootFailGrace):
	}
	if s.isReady() {
		return
	}
	if !s.awaitDeathEvidence() || s.isReady() {
		return
	}

	s.mu.Lock()
	s.bootFailed = true
	proc := s.proc
	s.mu.Unlock()
	if proc == nil {
		return
	}
	fmt.Printf("[discopanel-runtime] boot failed and the JVM went idle, shutting it down\n")
	_ = proc.Signal(syscall.SIGTERM)

	select {
	case <-s.done():
	case <-time.After(killEscalation):
		_ = proc.Kill()
	}
}

func (s *supervisor) wasBootFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootFailed
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

// Feeds one command line to the java process stdin
func (s *supervisor) writeConsole(line string) error {
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	_, err := s.stdin.Write([]byte(line + "\n"))
	return err
}

// Shows a chat line in game via tellraw
func (s *supervisor) broadcastChat(sender, message string) error {
	component, err := json.Marshal(map[string]string{"text": "<" + sender + "> " + message})
	if err != nil {
		return err
	}
	return s.writeConsole("tellraw @a " + string(component))
}

// Copies child output to console, feeds lines to parser
func (s *supervisor) mirrorConsole(r interface{ Read([]byte) (int, error) }, w *os.File) {
	buf := make([]byte, 64*1024)
	var line []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			s.lastOutputAt.Store(time.Now().UnixNano())
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

// Pushes the authoritative online player list to panel
func (s *supervisor) sendRoster() {
	s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Roster{
		Roster: &agentv1.Roster{OnlinePlayers: s.events.roster()},
	}})
}

// Keeps the panel-side roster freshness window alive
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

// Records readiness from whichever signal fires first
func (s *supervisor) markReady(startupSeconds float64) {
	s.mu.Lock()
	if s.ready {
		s.mu.Unlock()
		return
	}
	s.ready = true
	s.readyAt = time.Now()
	s.readySeconds = startupSeconds
	s.mu.Unlock()
	s.send(msgReady(startupSeconds))
}

func (s *supervisor) readyTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyAt
}

// Assembles, persists, and sends the exit report
func (s *supervisor) reportExit(exitCode int) {
	s.mu.Lock()
	requested := s.stopRequested
	blameSince := s.survivedReportAt
	s.mu.Unlock()
	if blameSince.Before(s.startedAt) {
		blameSince = s.startedAt
	}
	reportPath, excerpt, _ := findCrashReport(blameSince)
	oomKilled := readOOMKills()-s.oomBaseline > 0 && exitCode != 0
	crashed := isCrash(exitCode, requested, reportPath) || oomKilled
	report := &exitReport{
		ExitCode:       exitCode,
		Crashed:        crashed,
		OomKilled:      oomKilled,
		BootFailed:     crashed && s.wasBootFailed(),
		WasReady:       s.isReady(),
		ReportPath:     reportPath,
		Excerpt:        excerpt,
		ExitedAtUnixMs: time.Now().UnixMilli(),
	}
	// A stale background error means nothing on a clean exit
	if crashed {
		report.setFatal(s.fatalError())
	}
	writeExitReport(dataDir, report)
	s.mu.Lock()
	s.pendingExit = report
	s.mu.Unlock()
	if oomKilled {
		fmt.Printf("[discopanel-runtime] server was killed by the kernel OOM killer (out of memory)\n")
	}
	s.send(msgExited(report))
	// Gives the sender goroutine a moment to flush
	time.Sleep(500 * time.Millisecond)
}

// Decides crash, requested stops never count without a crash report
func isCrash(exitCode int, stopRequested bool, reportPath string) bool {
	if reportPath != "" {
		return true
	}
	return exitCode != 0 && !stopRequested
}

// Persisted copy of the last process exit
type exitReport struct {
	ExitCode       int             `json:"exit_code"`
	Crashed        bool            `json:"crashed"`
	OomKilled      bool            `json:"oom_killed"`
	BootFailed     bool            `json:"boot_failed,omitempty"`
	WasReady       bool            `json:"was_ready,omitempty"`
	ReportPath     string          `json:"crash_report_path,omitempty"`
	Excerpt        string          `json:"crash_report_excerpt,omitempty"`
	ExitedAtUnixMs int64           `json:"exited_at_unix_ms"`
	FatalError     json.RawMessage `json:"fatal_error,omitempty"`
}

// Stores the structured fatal error as protojson
func (r *exitReport) setFatal(fatal *agentv1.FatalError) {
	if fatal == nil {
		return
	}
	if data, err := protojson.Marshal(fatal); err == nil {
		r.FatalError = data
	}
}

// Decodes the persisted fatal error, nil when absent
func (r *exitReport) fatal() *agentv1.FatalError {
	if len(r.FatalError) == 0 {
		return nil
	}
	var fatal agentv1.FatalError
	if protojson.Unmarshal(r.FatalError, &fatal) != nil {
		return nil
	}
	return &fatal
}

func exitReportPath(dir string) string {
	return filepath.Join(dir, runtimespec.StateDir, "last-exit.json")
}

// Loads the previous run's exit report, nil if absent
func readExitReport(dir string) *exitReport {
	data, err := os.ReadFile(exitReportPath(dir))
	if err != nil {
		return nil
	}
	var r exitReport
	if json.Unmarshal(data, &r) != nil || r.ExitedAtUnixMs == 0 {
		return nil
	}
	return &r
}

func writeExitReport(dir string, r *exitReport) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = os.WriteFile(exitReportPath(dir), data, 0644)
}

// Locates the newest crash report and a capped excerpt
func findCrashReport(since time.Time) (string, string, time.Time) {
	dir := filepath.Join(dataDir, "crash-reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", time.Time{}
	}
	var newest string
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().After(since) {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = e.Name()
		}
	}
	if newest == "" {
		return "", "", time.Time{}
	}
	data, err := os.ReadFile(filepath.Join(dir, newest))
	if err != nil {
		return filepath.Join("crash-reports", newest), "", newestTime
	}
	if len(data) > maxExcerpt {
		data = data[:maxExcerpt]
	}
	return filepath.Join("crash-reports", newest), string(data), newestTime
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

// Chowns only entries not already owned by target uid
func ensureOwnership(dir string, uid, gid int) {
	start := time.Now()

	type entry struct {
		name string
		ent  os.DirEntry
	}
	entries := make(chan entry, 1024)
	var files int64
	var fixed int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range entries {
				info, err := e.ent.Info()
				if err != nil {
					continue
				}
				if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid && int(st.Gid) == gid {
					continue
				}
				if os.Lchown(e.name, uid, gid) == nil {
					atomic.AddInt64(&fixed, 1)
				}
			}
		}()
	}
	_ = filepath.WalkDir(dir, func(name string, ent os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		entries <- entry{name: name, ent: ent}
		files++
		return nil
	})
	close(entries)
	wg.Wait()
	if fixed > 0 {
		fmt.Printf("[discopanel-runtime] fixed ownership of %d of %d files (%d:%d) in %s\n",
			fixed, files, uid, gid, time.Since(start).Round(time.Millisecond))
	}
}

// Keeps the panel token unreadable by the game process
func protectAgentSpec() {
	path := runtimespec.AgentPath(dataDir)
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = os.Chown(path, 0, 0)
	_ = os.Chmod(path, 0600)
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

// Splits on commas and newlines, trims empties
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

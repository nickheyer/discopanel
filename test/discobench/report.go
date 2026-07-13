package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Every number one benchmark pass produced
type IterationResult struct {
	ColdSLPSeconds  float64 `json:"cold_slp_seconds"`
	ColdDoneSeconds float64 `json:"cold_done_seconds"`
	WarmDoneSeconds float64 `json:"warm_done_seconds"`
	StopSeconds     float64 `json:"stop_seconds"`
	StopExitCode    int     `json:"stop_exit_code"`
	IdleRSSMB       float64 `json:"idle_rss_mb"`

	TPSMedian  float64 `json:"tps_median"`
	TPSP5      float64 `json:"tps_p5"`
	TPSMin     float64 `json:"tps_min"`
	TPSMean    float64 `json:"tps_mean"`
	TPSSamples int     `json:"tps_samples"`

	LoadCPUMeanPercent float64 `json:"load_cpu_mean_percent"`
	LoadCPUMaxPercent  float64 `json:"load_cpu_max_percent"`
	LoadRSSMaxMB       float64 `json:"load_rss_max_mb"`

	PeakJoined   int64 `json:"peak_joined"`
	Reconnects   int64 `json:"reconnects"`
	JoinFailures int64 `json:"join_failures"`

	Error string `json:"error,omitempty"`
}

// One scenario and contender pairing across iterations
type CellResult struct {
	Scenario    string            `json:"scenario"`
	Contender   string            `json:"contender"`
	Image       string            `json:"image"`
	PullSeconds float64           `json:"pull_seconds"`
	ImageSizeMB float64           `json:"image_size_mb"`
	Iterations  []IterationResult `json:"iterations"`
}

// The full benchmark output
type Report struct {
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Host       string       `json:"host"`
	Config     *Config      `json:"config"`
	Cells      []CellResult `json:"cells"`
}

// Persists results.json and report.md into outDir
func (r *Report) write(outDir string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "results.json"), data, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "report.md"), []byte(r.markdown()), 0644)
}

// One line of the per-scenario comparison table
type metricRow struct {
	label  string
	better string
	pick   func(IterationResult) float64
}

var metricRows = []metricRow{
	{"Cold start to first ping (s)", "lower", func(i IterationResult) float64 { return i.ColdSLPSeconds }},
	{"Cold start to Done (s)", "lower", func(i IterationResult) float64 { return i.ColdDoneSeconds }},
	{"Warm restart to Done (s)", "lower", func(i IterationResult) float64 { return i.WarmDoneSeconds }},
	{"Graceful stop (s)", "lower", func(i IterationResult) float64 { return i.StopSeconds }},
	{"Idle RSS (MB)", "lower", func(i IterationResult) float64 { return i.IdleRSSMB }},
	{"TPS median under load", "higher", func(i IterationResult) float64 { return i.TPSMedian }},
	{"TPS p5 under load", "higher", func(i IterationResult) float64 { return i.TPSP5 }},
	{"TPS worst sample", "higher", func(i IterationResult) float64 { return i.TPSMin }},
	{"CPU mean under load (%)", "lower", func(i IterationResult) float64 { return i.LoadCPUMeanPercent }},
	{"RSS max under load (MB)", "lower", func(i IterationResult) float64 { return i.LoadRSSMaxMB }},
}

// Renders comparison tables, one per scenario
func (r *Report) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# discobench report\n\n")
	fmt.Fprintf(&b, "Started %s, finished %s, host `%s`.\n\n",
		r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339), r.Host)
	fmt.Fprintf(&b, "%d iteration(s) per cell, %d bots, %s load phase (first %s discarded as ramp), %d MB container / %d MB heap.\n\n",
		r.Config.Iterations, r.Config.Bots, r.Config.LoadDuration, r.Config.RampSkip, r.Config.MemoryMB, r.Config.HeapMB)
	fmt.Fprintf(&b, "Cells report the median across iterations with the min-max range in brackets. TPS is measured externally from world-age deltas observed by a bot, so no contender needs a mod installed. Methodology notes are in the README.\n")

	scenarios := []string{}
	seen := map[string]bool{}
	for _, c := range r.Cells {
		if !seen[c.Scenario] {
			seen[c.Scenario] = true
			scenarios = append(scenarios, c.Scenario)
		}
	}

	for _, scn := range scenarios {
		var cells []CellResult
		for _, c := range r.Cells {
			if c.Scenario == scn {
				cells = append(cells, c)
			}
		}
		fmt.Fprintf(&b, "\n## %s\n\n", scn)

		// Header
		fmt.Fprintf(&b, "| Metric |")
		for _, c := range cells {
			fmt.Fprintf(&b, " %s |", c.Contender)
		}
		fmt.Fprintf(&b, "\n|---|")
		for range cells {
			fmt.Fprintf(&b, "---|")
		}
		fmt.Fprintf(&b, "\n")

		fmt.Fprintf(&b, "| Image size (MB) |")
		for _, c := range cells {
			fmt.Fprintf(&b, " %.0f |", c.ImageSizeMB)
		}
		fmt.Fprintf(&b, "\n")

		for _, row := range metricRows {
			fmt.Fprintf(&b, "| %s |", row.label)
			for _, c := range cells {
				var vals []float64
				for _, it := range c.Iterations {
					if it.Error != "" {
						vals = append(vals, math.NaN())
						continue
					}
					vals = append(vals, row.pick(it))
				}
				med, lo, hi, ok := medianOf(vals)
				if !ok {
					fmt.Fprintf(&b, " failed |")
					continue
				}
				fmt.Fprintf(&b, " %.1f [%.1f-%.1f] |", med, lo, hi)
			}
			fmt.Fprintf(&b, "\n")
		}

		// Stability line covers the bot-side view of the run
		fmt.Fprintf(&b, "| Bot reconnects + join failures |")
		for _, c := range cells {
			var rec, fail int64
			errs := 0
			for _, it := range c.Iterations {
				rec += it.Reconnects
				fail += it.JoinFailures
				if it.Error != "" {
					errs++
				}
			}
			cell := fmt.Sprintf(" %d + %d |", rec, fail)
			if errs > 0 {
				cell = fmt.Sprintf(" %d + %d (%d failed runs) |", rec, fail, errs)
			}
			b.WriteString(cell)
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

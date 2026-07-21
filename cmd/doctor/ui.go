package main

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed assets/inter.woff2
var interFont []byte

var pageTmpl = template.Must(template.New("").ParseFS(templateFS, "templates/*.tmpl"))

// Serves the app's Inter font from the binary
func handleFont(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(interFont)
}

func (d *doctor) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	d.mu.RLock()
	rows := d.rows
	lastErr := d.lastErr
	sweep := d.lastSweep
	logs := d.logs
	d.mu.RUnlock()

	// Newest log lines read best on top
	rev := make([]string, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		rev = append(rev, logs[i])
	}

	watching, running, incidents, held := 0, 0, 0, 0
	for i := range rows {
		if rows[i].Enabled {
			watching++
		}
		if rows[i].Running {
			running++
		}
		if rows[i].Incident != nil {
			incidents++
		}
		held += len(rows[i].Excludes)
	}

	data := map[string]any{
		"Rows":      rows,
		"Logs":      rev,
		"Version":   doctorVersion,
		"Watching":  watching,
		"Running":   running,
		"Incidents": incidents,
		"Held":      held,
		"Mode":      d.defMode,
		"Poll":      d.poll.String(),
	}
	if !sweep.IsZero() {
		data["Sweep"] = ago(sweep)
	}
	if lastErr != nil {
		data["Error"] = lastErr.Error()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "index.tmpl", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

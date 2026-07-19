package main

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*
var templateFS embed.FS

var pageTmpl = template.Must(template.New("").ParseFS(templateFS, "templates/*.tmpl"))

func (d *doctor) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	d.mu.RLock()
	rows := d.rows
	lastErr := d.lastErr
	logs := d.logs
	d.mu.RUnlock()

	// Newest log lines read best on top
	rev := make([]string, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		rev = append(rev, logs[i])
	}

	data := map[string]any{
		"Rows":    rows,
		"Logs":    rev,
		"Version": doctorVersion,
	}
	if lastErr != nil {
		data["Error"] = lastErr.Error()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "index.tmpl", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

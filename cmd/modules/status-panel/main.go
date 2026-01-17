package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorcon/rcon"
)

type ServerStatus struct {
	Online       bool      `json:"online"`
	Players      int       `json:"players"`
	MaxPlayers   int       `json:"max_players"`
	TPS          float64   `json:"tps"`
	MOTD         string    `json:"motd"`
	Version      string    `json:"version"`
	LastUpdated  time.Time `json:"last_updated"`
	ErrorMessage string    `json:"error,omitempty"`
}

var (
	status     ServerStatus
	statusLock sync.RWMutex
)

func main() {
	rconHost := os.Getenv("RCON_HOST")
	rconPort := os.Getenv("RCON_PORT")
	rconPass := os.Getenv("RCON_PASSWORD")
	refreshInterval := getEnvInt("REFRESH_INTERVAL", 5)
	port := getEnvInt("PORT", 8080)

	if rconHost == "" {
		rconHost = os.Getenv("MC_SERVER_HOST")
	}
	if rconPort == "" {
		rconPort = "25575"
	}

	rconAddr := fmt.Sprintf("%s:%s", rconHost, rconPort)
	log.Printf("Status Panel starting - RCON: %s, Refresh: %ds", rconAddr, refreshInterval)

	go pollStatus(rconAddr, rconPass, time.Duration(refreshInterval)*time.Second)

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/status", handleAPIStatus)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Listening on :%d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func pollStatus(addr, password string, interval time.Duration) {
	for {
		s := queryServer(addr, password)
		statusLock.Lock()
		status = s
		statusLock.Unlock()
		time.Sleep(interval)
	}
}

func queryServer(addr, password string) ServerStatus {
	s := ServerStatus{LastUpdated: time.Now()}

	conn, err := rcon.Dial(addr, password)
	if err != nil {
		s.ErrorMessage = fmt.Sprintf("RCON connection failed: %v", err)
		return s
	}
	defer conn.Close()

	s.Online = true

	// Get player list
	if resp, err := conn.Execute("list"); err == nil {
		s.Players, s.MaxPlayers = parsePlayerList(resp)
	}

	// Try to get TPS (works on Paper/Spigot)
	if resp, err := conn.Execute("tps"); err == nil {
		s.TPS = parseTPS(resp)
	}

	return s
}

func parsePlayerList(resp string) (current, max int) {
	// Format: "There are X of a max of Y players online: ..."
	resp = strings.ToLower(resp)
	if strings.Contains(resp, "there are") {
		parts := strings.Fields(resp)
		for i, p := range parts {
			if p == "are" && i+1 < len(parts) {
				current, _ = strconv.Atoi(parts[i+1])
			}
			if p == "max" && i+2 < len(parts) {
				max, _ = strconv.Atoi(parts[i+2])
			}
		}
	}
	return
}

func parseTPS(resp string) float64 {
	// Format varies, look for numbers like "20.0" or "*20.0"
	resp = strings.ReplaceAll(resp, "*", "")
	parts := strings.Fields(resp)
	for _, p := range parts {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(p, ","), 64); err == nil && f > 0 && f <= 20 {
			return f
		}
	}
	return 0
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	statusLock.RLock()
	s := status
	statusLock.RUnlock()

	tmpl.Execute(w, s)
}

func handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	statusLock.RLock()
	s := status
	statusLock.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

var tmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html>
<head>
	<title>Server Status</title>
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta http-equiv="refresh" content="10">
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
		.card { background: #16213e; border-radius: 16px; padding: 2rem; width: 90%; max-width: 400px; box-shadow: 0 8px 32px rgba(0,0,0,0.3); }
		h1 { font-size: 1.5rem; margin-bottom: 1.5rem; text-align: center; color: #0f3460; background: linear-gradient(135deg, #e94560, #0f3460); -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
		.status { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 1rem; }
		.dot { width: 12px; height: 12px; border-radius: 50%; }
		.online { background: #00ff88; box-shadow: 0 0 10px #00ff88; }
		.offline { background: #ff4757; box-shadow: 0 0 10px #ff4757; }
		.stats { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
		.stat { background: #0f3460; padding: 1rem; border-radius: 8px; text-align: center; }
		.stat-value { font-size: 2rem; font-weight: bold; color: #e94560; }
		.stat-label { font-size: 0.75rem; color: #888; margin-top: 0.25rem; }
		.error { color: #ff4757; font-size: 0.875rem; margin-top: 1rem; text-align: center; }
		.updated { text-align: center; font-size: 0.75rem; color: #666; margin-top: 1rem; }
	</style>
</head>
<body>
	<div class="card">
		<h1>Minecraft Server</h1>
		<div class="status">
			<div class="dot {{if .Online}}online{{else}}offline{{end}}"></div>
			<span>{{if .Online}}Online{{else}}Offline{{end}}</span>
		</div>
		<div class="stats">
			<div class="stat">
				<div class="stat-value">{{.Players}}/{{.MaxPlayers}}</div>
				<div class="stat-label">Players</div>
			</div>
			<div class="stat">
				<div class="stat-value">{{if .TPS}}{{printf "%.1f" .TPS}}{{else}}--{{end}}</div>
				<div class="stat-label">TPS</div>
			</div>
		</div>
		{{if .ErrorMessage}}<div class="error">{{.ErrorMessage}}</div>{{end}}
		<div class="updated">Updated: {{.LastUpdated.Format "15:04:05"}}</div>
	</div>
</body>
</html>`))

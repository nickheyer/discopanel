package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
)

// Builds, signs, and delivers HTTP webhooks for server events
// NOTE: Concurrency is a scheduler concern, this is sync!

// Controls a single webhook delivery - built from json config blob on webhook task
type Config struct {
	URL             string            `json:"url"`
	Secret          string            `json:"secret"`
	PayloadTemplate string            `json:"payload_template"`
	Headers         map[string]string `json:"headers"`
	MaxRetries      int               `json:"max_retries"`
	RetryDelayMs    int               `json:"retry_delay_ms"`
	TimeoutMs       int               `json:"timeout_ms"`
}

// A single delivery attempt outcome
type Result struct {
	Success      bool
	ResponseCode int
	ResponseBody string
	ErrorMessage string
	DurationMs   int64
	Attempts     int
}

// Canonical event data fed into payload templates
type Payload struct {
	Event     string         `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	Server    *ServerPayload `json:"server,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Server snapshot embedded in a webhook payload
type ServerPayload struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	MCVersion  string `json:"mc_version"`
	ModLoader  string `json:"mod_loader"`
	Players    int    `json:"players_online"`
	MaxPlayers int    `json:"max_players"`
	Port       int    `json:"port"`
}

// Renders the payload, signs it, POSTs it, and retries on failure - returned res reflects final attempt made
func Deliver(ctx context.Context, cfg Config, payload *Payload) Result {
	start := time.Now()
	maxAttempts := cfg.MaxRetries
	if maxAttempts < 1 {
		maxAttempts = 1
	} else {
		maxAttempts++ // initial attempt + retries
	}
	retryBaseMs := cfg.RetryDelayMs
	if retryBaseMs <= 0 {
		retryBaseMs = 1000
	}

	var last Result
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		last = deliverOnce(ctx, cfg, payload)
		last.Attempts = attempt
		if last.Success {
			break
		}
		if attempt == maxAttempts {
			break
		}
		// Exponential backoff: base * 2^(attempt-1)
		delay := time.Duration(retryBaseMs) * time.Millisecond * time.Duration(1<<(attempt-1))
		select {
		case <-ctx.Done():
			last.ErrorMessage = ctx.Err().Error()
			last.DurationMs = time.Since(start).Milliseconds()
			return last
		case <-time.After(delay):
		}
	}
	last.DurationMs = time.Since(start).Milliseconds()
	return last
}

func deliverOnce(ctx context.Context, cfg Config, payload *Payload) Result {
	start := time.Now()
	result := Result{}

	body, err := renderBody(cfg, payload)
	if err != nil {
		result.ErrorMessage = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", cfg.URL, bytes.NewReader(body))
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("request error: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DiscoPanel-Webhook/1.0")
	req.Header.Set("X-DiscoPanel-Event", payload.Event)
	req.Header.Set("X-DiscoPanel-Delivery", uuid.New().String())

	if cfg.Secret != "" {
		req.Header.Set("X-DiscoPanel-Signature", "sha256="+sign(body, cfg.Secret))
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	result.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.ResponseCode = resp.StatusCode
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.ResponseBody = string(bodyBytes)
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !result.Success {
		result.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, result.ResponseBody)
	}
	return result
}

func renderBody(cfg Config, payload *Payload) ([]byte, error) {
	if cfg.PayloadTemplate != "" {
		return renderTemplate(cfg.PayloadTemplate, payload)
	}
	return json.Marshal(payload)
}

func sign(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Builds a flat map of variables available to payload templates
// TODO: Use the alias package!!!
func templateData(p *Payload) map[string]any {
	titles := map[string]string{
		"test":           "Webhook Test",
		"server_start":   "Server Started",
		"server_stop":    "Server Stopped",
		"server_restart": "Server Restarted",
	}
	colors := map[string]int{
		"test":           0x5865F2,
		"server_start":   0x57F287,
		"server_stop":    0xED4245,
		"server_restart": 0xFEE75C,
	}

	title := titles[p.Event]
	if title == "" {
		title = p.Event
	}
	color := colors[p.Event]
	if color == 0 {
		color = 0x5865F2
	}

	data := map[string]any{
		"event":     p.Event,
		"timestamp": p.Timestamp.Format(time.RFC3339),
		"title":     title,
		"color":     color,
		"player":    "",
	}
	if p.Server != nil {
		data["server_id"] = p.Server.ID
		data["server_name"] = p.Server.Name
		data["server_status"] = p.Server.Status
		data["server_mc_version"] = p.Server.MCVersion
		data["server_mod_loader"] = p.Server.ModLoader
		data["server_players"] = p.Server.Players
		data["server_max_players"] = p.Server.MaxPlayers
		data["server_port"] = p.Server.Port
	} else {
		data["server_id"] = ""
		data["server_name"] = ""
		data["server_status"] = ""
		data["server_mc_version"] = ""
		data["server_mod_loader"] = ""
		data["server_players"] = 0
		data["server_max_players"] = 0
		data["server_port"] = 0
	}
	for k, v := range p.Data {
		data[k] = v
	}
	return data
}

func renderTemplate(tmplStr string, p *Payload) ([]byte, error) {
	tmpl, err := template.New("payload").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("invalid template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData(p)); err != nil {
		return nil, fmt.Errorf("template execution failed: %w", err)
	}
	out := buf.Bytes()
	if !json.Valid(out) {
		return nil, fmt.Errorf("template produced invalid JSON")
	}
	return out, nil
}

// Verifies a template string parses and produces valid JSON when rendered with sample data
func ValidateTemplate(tmplStr string) error {
	if strings.TrimSpace(tmplStr) == "" {
		return nil
	}
	sample := &Payload{
		Event:     "test",
		Timestamp: time.Now().UTC(),
		Server: &ServerPayload{
			ID: "test-id", Name: "Test Server", Status: "running",
			MCVersion: "1.21", ModLoader: "vanilla",
			Players: 0, MaxPlayers: 20, Port: 25565,
		},
	}
	_, err := renderTemplate(tmplStr, sample)
	return err
}

// Assembles a Payload for the given event and server
func BuildPayload(event string, server *storage.Server, data map[string]any) *Payload {
	var sp *ServerPayload
	if server != nil {
		sp = &ServerPayload{
			ID:         server.ID,
			Name:       server.Name,
			Status:     string(server.Status),
			MCVersion:  server.MCVersion,
			ModLoader:  string(server.ModLoader),
			Players:    server.PlayersOnline,
			MaxPlayers: server.MaxPlayers,
			Port:       server.Port,
		}
	}
	return &Payload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Server:    sp,
		Data:      data,
	}
}

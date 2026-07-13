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
	"github.com/nickheyer/discopanel/internal/alias"
	storage "github.com/nickheyer/discopanel/internal/db"
)

// Builds, signs, and delivers HTTP webhooks for server events
// Concurrency is a scheduler concern, this stays sync

// Controls one delivery, built from task config json
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

	vars map[string]any
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

// Renders, signs, POSTs, and retries one delivery
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
		// Exponential backoff doubles the base per attempt
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

// Flattens alias paths into template vars, one shared vocabulary
func templateVars(event string, timestamp time.Time, server *storage.Server, data map[string]any) map[string]any {
	vars := map[string]any{
		"event":       event,
		"is_" + event: true,
		"timestamp":   timestamp.Format(time.RFC3339),
		"title":       humanizeEvent(event),
		"player":      "",
	}
	rctx := alias.NewContext()
	rctx.Server = server
	for k, v := range alias.GetResolvedAliases(rctx) {
		path := strings.TrimSuffix(strings.TrimPrefix(k, "{{"), "}}")
		if strings.HasPrefix(path, "server.config.") {
			continue
		}
		if strings.HasPrefix(path, "server.") || strings.HasPrefix(path, "host.") {
			vars[strings.ReplaceAll(path, ".", "_")] = v
		}
	}
	for k, v := range data {
		vars[k] = v
	}
	return vars
}

// Turns event keys into readable titles
func humanizeEvent(event string) string {
	words := strings.Split(event, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func renderTemplate(tmplStr string, p *Payload) ([]byte, error) {
	tmpl, err := template.New("payload").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("invalid template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p.vars); err != nil {
		return nil, fmt.Errorf("template execution failed: %w", err)
	}
	out := buf.Bytes()
	if !json.Valid(out) {
		return nil, fmt.Errorf("template produced invalid JSON")
	}
	return out, nil
}

// Verifies a template renders valid JSON from samples
func ValidateTemplate(tmplStr string) error {
	if strings.TrimSpace(tmplStr) == "" {
		return nil
	}
	sample := BuildPayload("test", &storage.Server{
		ID: "test-id", Name: "Test Server", Status: storage.StatusRunning,
		MCVersion: "1.21", ModLoader: "vanilla",
		MaxPlayers: 20, Port: 25565,
	}, nil)
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
	p := &Payload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Server:    sp,
		Data:      data,
	}
	p.vars = templateVars(event, p.Timestamp, server, data)
	return p
}

// Package webhook builds, signs, and delivers HTTP webhooks for server events.
// It exposes payload building, template rendering, and a synchronous Deliver
// function. Concurrency and retry orchestration live in the scheduler.
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

// Config controls a single webhook delivery. The scheduler builds this from
// the JSON config blob stored on a webhook task.
type Config struct {
	URL             string            `json:"url"`
	Secret          string            `json:"secret"`
	Format          string            `json:"format"`
	PayloadTemplate string            `json:"payload_template"`
	Headers         map[string]string `json:"headers"`
	MaxRetries      int               `json:"max_retries"`
	RetryDelayMs    int               `json:"retry_delay_ms"`
	TimeoutMs       int               `json:"timeout_ms"`
}

// Format names recognised when no custom payload template is set.
const (
	FormatGeneric = "generic"
	FormatDiscord = "discord"
	FormatSlack   = "slack"
	FormatTeams   = "teams"
	FormatNtfy    = "ntfy"
)

// Result describes a single delivery attempt outcome.
type Result struct {
	Success      bool
	ResponseCode int
	ResponseBody string
	ErrorMessage string
	DurationMs   int64
	Attempts     int
}

// Payload is the canonical event data fed into payload templates.
type Payload struct {
	Event     string         `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	Server    *ServerPayload `json:"server,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// ServerPayload is the server snapshot embedded in a webhook payload.
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

// Deliver renders the payload, signs it, POSTs it, and retries on failure
// using exponential backoff. The returned Result reflects the final attempt.
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
	// Pick a format. In order: explicit non-generic format → URL-based
	// auto-detection → generic JSON marshal. Auto-detection covers the
	// common case where a webhook config was saved with format="generic"
	// but the URL is clearly a Discord/Slack/Teams/ntfy endpoint.
	format := cfg.Format
	if format == "" || format == FormatGeneric {
		if detected := detectFormatFromURL(cfg.URL); detected != "" {
			format = detected
		}
	}
	if tmpl, ok := TemplatePresets()[format]; ok && format != FormatGeneric {
		return renderTemplate(tmpl, payload)
	}
	return json.Marshal(payload)
}

// detectFormatFromURL guesses a payload format from the destination URL.
// Returns "" when nothing recognisable matches.
func detectFormatFromURL(url string) string {
	switch {
	case strings.Contains(url, "discord.com/api/webhooks"),
		strings.Contains(url, "discordapp.com/api/webhooks"):
		return FormatDiscord
	case strings.Contains(url, "hooks.slack.com"):
		return FormatSlack
	case strings.Contains(url, ".webhook.office.com"),
		strings.Contains(url, "outlook.office.com/webhook"):
		return FormatTeams
	case strings.Contains(url, "ntfy.sh"):
		return FormatNtfy
	}
	return ""
}

func sign(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// DefaultGenericTemplate is the built-in generic JSON template
const DefaultGenericTemplate = `{
  "event": "{{.event}}",
  "timestamp": "{{.timestamp}}",
  "server": {
    "id": "{{.server_id}}",
    "name": "{{.server_name}}",
    "status": "{{.server_status}}",
    "mc_version": "{{.server_mc_version}}",
    "mod_loader": "{{.server_mod_loader}}",
    "players_online": {{.server_players}},
    "max_players": {{.server_max_players}},
    "port": {{.server_port}}
  }
}`

// DefaultDiscordTemplate is the built-in Discord embed template
const DefaultDiscordTemplate = `{
  "embeds": [{
    "title": "{{.title}}",
    "description": "**{{.server_name}}** - {{.server_status}}",
    "color": {{.color}},
    "timestamp": "{{.timestamp}}",
    "fields": [
      {"name": "Version", "value": "{{.server_mc_version}}", "inline": true},
      {"name": "Players", "value": "{{.server_players}}/{{.server_max_players}}", "inline": true},
      {"name": "Mod Loader", "value": "{{.server_mod_loader}}", "inline": true}
    ],
    "footer": {"text": "DiscoPanel"}
  }]
}`

// DefaultSlackTemplate is the built-in Slack Block Kit template
const DefaultSlackTemplate = `{
  "blocks": [
    {
      "type": "header",
      "text": {"type": "plain_text", "text": "{{.title}}"}
    },
    {
      "type": "section",
      "text": {"type": "mrkdwn", "text": "*{{.server_name}}* — {{.server_status}}"}
    },
    {
      "type": "section",
      "fields": [
        {"type": "mrkdwn", "text": "*Version:*\n{{.server_mc_version}}"},
        {"type": "mrkdwn", "text": "*Players:*\n{{.server_players}}/{{.server_max_players}}"},
        {"type": "mrkdwn", "text": "*Mod Loader:*\n{{.server_mod_loader}}"},
        {"type": "mrkdwn", "text": "*Port:*\n{{.server_port}}"}
      ]
    },
    {
      "type": "context",
      "elements": [{"type": "mrkdwn", "text": "DiscoPanel • {{.timestamp}}"}]
    }
  ]
}`

// DefaultTeamsTemplate is the built-in Microsoft Teams Adaptive Card template
const DefaultTeamsTemplate = `{
  "type": "message",
  "attachments": [{
    "contentType": "application/vnd.microsoft.card.adaptive",
    "content": {
      "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
      "type": "AdaptiveCard",
      "version": "1.4",
      "body": [
        {
          "type": "TextBlock",
          "size": "medium",
          "weight": "bolder",
          "text": "{{.title}}"
        },
        {
          "type": "TextBlock",
          "text": "**{{.server_name}}** — {{.server_status}}",
          "wrap": true
        },
        {
          "type": "FactSet",
          "facts": [
            {"title": "Version", "value": "{{.server_mc_version}}"},
            {"title": "Players", "value": "{{.server_players}}/{{.server_max_players}}"},
            {"title": "Mod Loader", "value": "{{.server_mod_loader}}"},
            {"title": "Port", "value": "{{.server_port}}"}
          ]
        },
        {
          "type": "TextBlock",
          "text": "DiscoPanel • {{.timestamp}}",
          "size": "small",
          "isSubtle": true
        }
      ]
    }
  }]
}`

// DefaultNtfyTemplate is the built-in ntfy.sh template
const DefaultNtfyTemplate = `{
  "topic": "discopanel",
  "title": "{{.title}}",
  "message": "{{.server_name}} — {{.server_status}}",
  "tags": ["video_game"],
  "priority": 3
}`

// TemplatePresets returns all built-in template presets keyed by format name.
func TemplatePresets() map[string]string {
	return map[string]string{
		FormatGeneric: DefaultGenericTemplate,
		FormatDiscord: DefaultDiscordTemplate,
		FormatSlack:   DefaultSlackTemplate,
		FormatTeams:   DefaultTeamsTemplate,
		FormatNtfy:    DefaultNtfyTemplate,
	}
}

// templateData builds a flat map of variables available to payload templates.
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

// ValidateTemplate verifies a template string parses and produces valid JSON when rendered with sample data.
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

// BuildPayload assembles a Payload for the given event and server.
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

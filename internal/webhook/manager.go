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
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// Manager handles webhook dispatching and delivery
type Manager struct {
	store      *storage.Store
	log        *logger.Logger
	httpClient *http.Client
	workQueue  chan *deliveryJob
	wg         sync.WaitGroup
	workers    int
	stopCh     chan struct{}
	running    bool
	mu         sync.RWMutex
}

// NewManager creates a new webhook manager
func NewManager(store *storage.Store, log *logger.Logger, workers int) *Manager {
	if workers <= 0 {
		workers = 5
	}
	return &Manager{
		store:      store,
		log:        log,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		workQueue:  make(chan *deliveryJob, 1000),
		workers:    workers,
		stopCh:     make(chan struct{}),
	}
}

// Start begins the webhook worker pool
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.stopCh = make(chan struct{})
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
	m.running = true
	m.log.Info("Webhook manager started with %d workers", m.workers)
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()
	m.log.Info("Webhook manager stopped")
}

// Dispatch queues webhooks for delivery based on an event
func (m *Manager) Dispatch(ctx context.Context, serverID string, eventType storage.WebhookEventType, payload *Payload) error {
	m.mu.RLock()
	running := m.running
	m.mu.RUnlock()

	if !running {
		return fmt.Errorf("webhook manager not running")
	}

	webhooks, err := m.store.GetWebhooksForEvent(ctx, serverID, eventType)
	if err != nil {
		return fmt.Errorf("failed to get webhooks: %w", err)
	}

	for _, webhook := range webhooks {
		if !webhook.Enabled {
			continue
		}

		job := &deliveryJob{
			webhook:   webhook,
			eventType: eventType,
			serverID:  serverID,
			payload:   payload,
			attempt:   1,
		}

		select {
		case m.workQueue <- job:
		default:
			m.log.Warn("Webhook queue full, dropping delivery for webhook %s", webhook.ID)
		}
	}

	return nil
}

// TestWebhook sends a test payload to a webhook and returns the result
func (m *Manager) TestWebhook(ctx context.Context, webhook *storage.Webhook, server *storage.Server) *TestResult {
	payload := &Payload{
		Event:     "test",
		Timestamp: time.Now().UTC(),
		Server: &ServerPayload{
			ID:         server.ID,
			Name:       server.Name,
			Status:     string(server.Status),
			MCVersion:  server.MCVersion,
			ModLoader:  string(server.ModLoader),
			Players:    server.PlayersOnline,
			MaxPlayers: server.MaxPlayers,
			Port:       server.Port,
		},
		Data: map[string]interface{}{
			"message": "This is a test webhook delivery from DiscoPanel",
		},
	}

	return m.deliverSync(webhook, payload)
}

// Payload represents the standard webhook payload
type Payload struct {
	Event     string                 `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Server    *ServerPayload         `json:"server,omitempty"`
	Player    *PlayerPayload         `json:"player,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// ServerPayload represents server information in the webhook payload
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

// PlayerPayload represents player information in the webhook payload
type PlayerPayload struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

// TestResult represents the result of a test webhook delivery
type TestResult struct {
	Success      bool
	ResponseCode int
	ResponseBody string
	ErrorMessage string
	DurationMs   int64
}

// deliveryJob represents a job in the work queue
type deliveryJob struct {
	webhook   *storage.Webhook
	eventType storage.WebhookEventType
	serverID  string
	payload   *Payload
	attempt   int
}

// worker processes delivery jobs from the queue
func (m *Manager) worker(id int) {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopCh:
			return
		case job := <-m.workQueue:
			m.deliver(job)
		}
	}
}

// deliver makes the HTTP request and handles retries
func (m *Manager) deliver(job *deliveryJob) {
	result := m.deliverSync(job.webhook, job.payload)

	if !result.Success {
		m.log.Error("Webhook delivery failed for %s (attempt %d): %s", job.webhook.Name, job.attempt, result.ErrorMessage)

		// Retry if attempts remaining
		if job.attempt < job.webhook.MaxRetries {
			job.attempt++
			delay := time.Duration(job.webhook.RetryDelayMs) * time.Millisecond * time.Duration(1<<(job.attempt-1)) // Exponential backoff

			time.AfterFunc(delay, func() {
				select {
				case m.workQueue <- job:
				default:
					m.log.Warn("Retry queue full for webhook %s", job.webhook.ID)
				}
			})
		}
	}
}

// deliverSync performs a synchronous delivery and returns the result
func (m *Manager) deliverSync(webhook *storage.Webhook, payload *Payload) *TestResult {
	start := time.Now()
	result := &TestResult{}

	var payloadBytes []byte
	var err error

	// If a custom payload template is set, render it
	if webhook.PayloadTemplate != "" {
		payloadBytes, err = renderTemplate(webhook.PayloadTemplate, payload)
	} else if webhook.Format == storage.WebhookFormatDiscord {
		// Fallback: use built-in Discord format for webhooks without a template
		payloadBytes, err = renderTemplate(DefaultDiscordTemplate, payload)
	} else {
		// Fallback: use built-in generic JSON format
		payloadBytes, err = json.Marshal(payload)
	}

	if err != nil {
		result.ErrorMessage = fmt.Sprintf("template render error: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Create request with timeout
	timeout := time.Duration(webhook.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", webhook.URL, bytes.NewReader(payloadBytes))
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("request error: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DiscoPanel-Webhook/1.0")
	req.Header.Set("X-DiscoPanel-Event", string(payload.Event))
	req.Header.Set("X-DiscoPanel-Delivery", uuid.New().String())

	// Add HMAC signature if secret is configured
	if webhook.Secret != "" {
		signature := m.signPayload(payloadBytes, webhook.Secret)
		req.Header.Set("X-DiscoPanel-Signature", "sha256="+signature)
	}

	// Add custom headers
	for k, v := range webhook.Headers {
		req.Header.Set(k, v)
	}

	// Make request
	resp, err := m.httpClient.Do(req)
	result.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.ResponseCode = resp.StatusCode

	// Read response body (limited to 4KB)
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.ResponseBody = string(bodyBytes)

	// Check success (2xx status codes)
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !result.Success {
		result.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, result.ResponseBody)
	}

	return result
}

// signPayload creates HMAC-SHA256 signature
func (m *Manager) signPayload(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
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

// TemplatePresets returns all built-in template presets
func TemplatePresets() map[string]string {
	return map[string]string{
		"generic": DefaultGenericTemplate,
		"discord": DefaultDiscordTemplate,
		"slack":   DefaultSlackTemplate,
		"teams":   DefaultTeamsTemplate,
		"ntfy":    DefaultNtfyTemplate,
	}
}

// templateData builds a flat map of variables available to payload templates
func templateData(p *Payload) map[string]interface{} {
	// Event title and color mappings
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

	data := map[string]interface{}{
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

	// Merge any extra data
	for k, v := range p.Data {
		data[k] = v
	}

	return data
}

// renderTemplate renders a Go text/template with payload data and returns JSON bytes
func renderTemplate(tmplStr string, p *Payload) ([]byte, error) {
	tmpl, err := template.New("payload").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("invalid template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData(p)); err != nil {
		return nil, fmt.Errorf("template execution failed: %w", err)
	}

	// Validate that the output is valid JSON
	result := buf.Bytes()
	if !json.Valid(result) {
		return nil, fmt.Errorf("template produced invalid JSON")
	}

	return result, nil
}

// ValidateTemplate checks that a template string is parseable and produces valid JSON with test data
func ValidateTemplate(tmplStr string) error {
	if strings.TrimSpace(tmplStr) == "" {
		return nil
	}

	testPayload := &Payload{
		Event:     "test",
		Timestamp: time.Now().UTC(),
		Server: &ServerPayload{
			ID:         "test-id",
			Name:       "Test Server",
			Status:     "running",
			MCVersion:  "1.21",
			ModLoader:  "vanilla",
			Players:    0,
			MaxPlayers: 20,
			Port:       25565,
		},
	}

	_, err := renderTemplate(tmplStr, testPayload)
	return err
}

// BuildServerPayload creates a server payload from a Server model
func BuildServerPayload(server *storage.Server) *ServerPayload {
	return &ServerPayload{
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

// BuildPayload creates a full webhook payload
func BuildPayload(event string, server *storage.Server, player *PlayerPayload, data map[string]interface{}) *Payload {
	payload := &Payload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	if server != nil {
		payload.Server = BuildServerPayload(server)
	}

	if player != nil {
		payload.Player = player
	}

	return payload
}

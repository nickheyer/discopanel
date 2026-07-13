package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// Mojang's management protocol gives exact join and leave signals

// Request ids used for connect-time status and players calls
const (
	mgmtRequestStatus  = int64(1)
	mgmtRequestPlayers = int64(2)
)

const mgmtReconnectDelay = 5 * time.Second

type mgmtConfig struct {
	host   string
	port   int
	secret string
}

// Loads management endpoint, nil if disabled or unconfigured
func readMgmtConfig(dir string) *mgmtConfig {
	data, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		return nil
	}
	props := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if props["management-server-enabled"] != "true" {
		return nil
	}
	if props["management-server-tls-enabled"] == "true" {
		return nil
	}
	port, err := strconv.Atoi(props["management-server-port"])
	if err != nil || port <= 0 {
		return nil
	}
	host := props["management-server-host"]
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return &mgmtConfig{host: host, port: port, secret: props["management-server-secret"]}
}

// Tolerant JSON-RPC 2.0 envelope for all message types
type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The management protocol's player object
type mgmtPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Holds a management protocol session open, reconnecting quietly
func (s *supervisor) runManagementClient(cfg *mgmtConfig) {
	connectedOnce := false
	for {
		select {
		case <-s.done():
			return
		default:
		}
		_ = s.managementSessionOnce(cfg, &connectedOnce)
		select {
		case <-s.done():
			return
		case <-time.After(mgmtReconnectDelay):
		}
	}
}

func (s *supervisor) managementSessionOnce(cfg *mgmtConfig, connectedOnce *bool) error {
	endpoint := fmt.Sprintf("ws://%s:%d", cfg.host, cfg.port)
	// The origin must match the provisioned management-server-allowed-origins
	wsCfg, err := websocket.NewConfig(endpoint, "http://127.0.0.1")
	if err != nil {
		return err
	}
	if wsCfg.Header == nil {
		wsCfg.Header = http.Header{}
	}
	if cfg.secret != "" {
		wsCfg.Header.Set("Authorization", "Bearer "+cfg.secret)
	}
	wsCfg.Dialer = &net.Dialer{Timeout: 5 * time.Second}

	conn, err := websocket.DialConfig(wsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Unblocks the read loop when the java process exits
	sessionDone := make(chan struct{})
	defer close(sessionDone)
	go func() {
		select {
		case <-s.done():
			_ = conn.Close()
		case <-sessionDone:
		}
	}()

	request := func(id int64, method string) error {
		msg, err := json.Marshal(jsonRPCMessage{JSONRPC: "2.0", ID: &id, Method: method})
		if err != nil {
			return err
		}
		return websocket.Message.Send(conn, string(msg))
	}
	// Syncs ready flag and roster at connect time
	if err := request(mgmtRequestStatus, "minecraft:server/status"); err != nil {
		return err
	}
	if err := request(mgmtRequestPlayers, "minecraft:players"); err != nil {
		return err
	}

	if !*connectedOnce {
		*connectedOnce = true
		fmt.Printf("[discopanel-runtime] management protocol connected (%s)\n", endpoint)
	}

	for {
		var raw string
		if err := websocket.Message.Receive(conn, &raw); err != nil {
			return err
		}
		var msg jsonRPCMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		s.handleMgmtMessage(&msg)
	}
}

// Routes one management protocol message to its handler
func (s *supervisor) handleMgmtMessage(msg *jsonRPCMessage) {
	if msg.ID != nil {
		if msg.Error != nil {
			fmt.Printf("[discopanel-runtime] management protocol request failed: %s\n", msg.Error.Message)
			return
		}
		switch *msg.ID {
		case mgmtRequestStatus:
			var status struct {
				Started bool `json:"started"`
			}
			if decodeMgmtResult(msg.Result, "status", &status) && status.Started {
				s.markReady(time.Since(s.startedAt).Seconds())
			}
		case mgmtRequestPlayers:
			var players []mgmtPlayer
			if decodeMgmtResult(msg.Result, "players", &players) {
				s.events.syncRoster(players)
			}
		}
		return
	}

	switch msg.Method {
	case "minecraft:notification/players/joined":
		for _, p := range decodeMgmtPlayers(msg.Params) {
			s.events.setUUID(p.Name, p.ID)
			s.events.playerChange(p.Name, true)
		}
	case "minecraft:notification/players/left":
		for _, p := range decodeMgmtPlayers(msg.Params) {
			s.events.playerChange(p.Name, false)
		}
	case "minecraft:notification/server/started":
		s.markReady(time.Since(s.startedAt).Seconds())
	case "minecraft:notification/server/stopping":
		s.send(msgStopping())
	}
}

// Unwraps a JSON-RPC result wrapped or bare
func decodeMgmtResult(result json.RawMessage, key string, out any) bool {
	if len(result) == 0 {
		return false
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(result, &wrapper); err == nil {
		if inner, ok := wrapper[key]; ok {
			return json.Unmarshal(inner, out) == nil
		}
	}
	return json.Unmarshal(result, out) == nil
}

// Accepts notification params as one player or an array
func decodeMgmtPlayers(params json.RawMessage) []mgmtPlayer {
	if len(params) == 0 {
		return nil
	}
	var one mgmtPlayer
	if err := json.Unmarshal(params, &one); err == nil && one.Name != "" {
		return []mgmtPlayer{one}
	}
	var many []mgmtPlayer
	if err := json.Unmarshal(params, &many); err == nil {
		return many
	}
	return nil
}

package lifecycle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// writeAgentSpec provisions the runtime agent connection file before each
// start: panel endpoint plus a fresh per-start bearer token (only its hash is
// stored). Disabled servers get an explicit disabled spec so a previously
// enabled agent shuts off.
func (m *Manager) writeAgentSpec(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig) error {
	enabled := cfg == nil || cfg.EnableAgent == nil || *cfg.EnableAgent
	if !enabled {
		return runtimespec.WriteAgentSpec(server.DataPath, &runtimespec.AgentSpec{Version: 1, Enabled: false})
	}

	panelURL := m.cfg.Docker.AgentURL
	if panelURL == "" {
		url, err := m.docker.PanelAgentURL(ctx, m.cfg.Server.Port)
		if err != nil {
			return fmt.Errorf("cannot resolve panel URL for the agent: %w", err)
		}
		panelURL = url
	}

	// Rotate the token every start; the container reads the plaintext from
	// its data dir and the panel keeps only the hash.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("failed to generate agent token: %w", err)
	}
	token := "dpa_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	server.AgentTokenHash = hex.EncodeToString(sum[:])
	if err := m.store.UpdateServer(ctx, server); err != nil {
		return fmt.Errorf("failed to persist agent token hash: %w", err)
	}

	return runtimespec.WriteAgentSpec(server.DataPath, &runtimespec.AgentSpec{
		Version:  1,
		Enabled:  true,
		PanelURL: panelURL,
		Token:    token,
		ServerID: server.ID,
	})
}

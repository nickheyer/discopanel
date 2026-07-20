package services

import (
	"testing"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Locks the reflection contract onto injected proto tags
func TestPropertyCategoriesFromProtoModel(t *testing.T) {
	mem := "2048M"
	cats, err := buildPropertyCategories(&v1.ServerProperties{InitMemory: &mem})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	keys := map[string]string{}
	count := 0
	for _, cat := range cats {
		for _, p := range cat.Properties {
			keys[p.Key] = p.Value
			count++
		}
	}
	if count < 100 {
		t.Fatalf("only %d properties surfaced, metadata tags missing", count)
	}
	if got := keys["initMemory"]; got != "2048M" {
		t.Fatalf("initMemory = %q, want 2048M (keys must stay camelCase)", got)
	}
	for _, banned := range []string{"id", "server_id", "updated_at"} {
		if _, ok := keys[banned]; ok {
			t.Fatalf("bookkeeping column %s leaked into properties", banned)
		}
	}

	// Updates land through legacy keys
	cfg := &v1.ServerProperties{}
	if err := applyPropertyUpdates(cfg, map[string]string{"maxPlayers": "42", "enableJmx": "true"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if cfg.MaxPlayers == nil || *cfg.MaxPlayers != 42 {
		t.Fatalf("maxPlayers not applied: %+v", cfg.MaxPlayers)
	}
	if cfg.EnableJmx == nil || !*cfg.EnableJmx {
		t.Fatal("enableJmx not applied")
	}
}

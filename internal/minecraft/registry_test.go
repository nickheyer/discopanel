package minecraft

import (
	"testing"

	models "github.com/nickheyer/discopanel/internal/db"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Rows must match the proto enum exactly
func TestRegistryCoversProtoEnum(t *testing.T) {
	for val, name := range v1.ModLoader_name {
		p := v1.ModLoader(val)
		if p == v1.ModLoader_MOD_LOADER_UNSPECIFIED {
			continue
		}
		if _, ok := LoaderFromProto(p); !ok {
			t.Errorf("enum value %s has no registry row", name)
		}
	}
	if len(registry) != len(v1.ModLoader_name)-1 {
		t.Errorf("registry has %d rows, enum has %d values", len(registry), len(v1.ModLoader_name)-1)
	}
}

func TestRegistryRowsConsistent(t *testing.T) {
	seenLoader := map[models.ModLoader]bool{}
	seenProto := map[v1.ModLoader]bool{}
	for _, row := range registry {
		if row.Loader == "" {
			t.Errorf("row %s has no loader name", row.DisplayName)
		}
		if seenLoader[row.Loader] {
			t.Errorf("loader %s declared twice", row.Loader)
		}
		seenLoader[row.Loader] = true
		if row.Proto == v1.ModLoader_MOD_LOADER_UNSPECIFIED {
			t.Errorf("loader %s maps to unspecified proto", row.Loader)
		}
		if seenProto[row.Proto] {
			t.Errorf("proto value %s mapped twice", row.Proto)
		}
		seenProto[row.Proto] = true
		if row.DisplayName == "" || row.Description == "" || row.Category == "" {
			t.Errorf("loader %s misses display facts", row.Loader)
		}
		for _, d := range row.Dialects {
			if definingLoader(d) == nil {
				t.Errorf("loader %s reads unknown dialect %q", row.Loader, d)
			}
		}
		if len(row.Dialects) > 0 && row.ModsDirectory == "" {
			t.Errorf("loader %s reads mods but stores none", row.Loader)
		}
		defining := len(row.Dialects) > 0 && row.Dialects[0] == string(row.Loader)
		if !defining && (len(row.Builtins) > 0 || len(row.Facets) > 0 || len(row.Markers) > 0 || row.MavenRanges) {
			t.Errorf("loader %s carries format facts without defining one", row.Loader)
		}
		if defining && (len(row.Facets) == 0 || len(row.Markers) == 0) {
			t.Errorf("defining loader %s misses facets or markers", row.Loader)
		}
	}
}

func TestLoaderProtoRoundTrip(t *testing.T) {
	for _, row := range registry {
		back, ok := LoaderFromProto(ProtoFor(row.Loader))
		if !ok || back != row.Loader {
			t.Errorf("round trip broke for %s", row.Loader)
		}
	}
	if got := ProtoFor(models.ModLoader("nonsense")); got != v1.ModLoader_MOD_LOADER_UNSPECIFIED {
		t.Errorf("unknown loader mapped to %v", got)
	}
}

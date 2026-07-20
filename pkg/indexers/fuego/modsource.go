package fuego

import (
	"context"
	"fmt"

	"github.com/nickheyer/discopanel/pkg/indexers"
)

// Implements ModSourcer
var _ indexers.ModSourcer = (*FuegoIndexer)(nil)

// Caps fuzzy search hits tried per query
const maxSourceHits = 3

// Finds candidate jars for a mod id
// Exact slug lookup first, fuzzy search covers slug drift
func (f *FuegoIndexer) SourceMod(ctx context.Context, q indexers.ModQuery) ([]indexers.ModCandidate, error) {
	lt := queryLoaderType(q.Loaders)
	if lt == ModLoaderAny {
		return nil, fmt.Errorf("no curseforge loader filter matches %v", q.Loaders)
	}

	var mods []Modpack
	if mod, err := f.client.GetModBySlug(ctx, q.ModID, ModsClassID); err == nil {
		mods = append(mods, *mod)
	}
	if len(mods) == 0 {
		resp, err := f.client.SearchProjects(ctx, q.ModID, ModsClassID, q.McVersion, lt, 0, maxSourceHits)
		if err != nil {
			return nil, err
		}
		mods = resp.Data
	}

	var out []indexers.ModCandidate
	for i := range mods {
		c, err := f.modCandidate(ctx, &mods[i], q.McVersion, lt)
		if err != nil || c == nil {
			continue
		}
		out = append(out, *c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no curseforge mod offers %q for MC %s", q.ModID, q.McVersion)
	}
	return out, nil
}

// First mappable loader facet wins
func queryLoaderType(loaders []string) ModLoaderType {
	for _, l := range loaders {
		if lt := loaderType(l); lt != ModLoaderAny {
			return lt
		}
	}
	return ModLoaderAny
}

// Builds one candidate from a mod's newest matching file
func (f *FuegoIndexer) modCandidate(ctx context.Context, mod *Modpack, mcVersion string, lt ModLoaderType) (*indexers.ModCandidate, error) {
	files, err := f.client.GetModpackFiles(ctx, mod.ID, mcVersion, lt)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	newest := &files[0]
	for i := range files {
		if files[i].FileDate.After(newest.FileDate) {
			newest = &files[i]
		}
	}
	dlURL, err := f.client.ResolveDownloadURL(ctx, mod.ID, newest)
	if err != nil {
		return nil, err
	}
	algo, sum := newest.BestHash()
	return &indexers.ModCandidate{
		Origin:   "curseforge mod " + mod.Slug,
		FileName: newest.FileName,
		URL:      dlURL,
		HashAlgo: algo,
		HashSum:  sum,
	}, nil
}

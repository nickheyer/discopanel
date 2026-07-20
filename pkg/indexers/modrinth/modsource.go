package modrinth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nickheyer/discopanel/pkg/indexers"
)

// Implements ModSourcer
var _ indexers.ModSourcer = (*ModrinthIndexer)(nil)

// Caps fuzzy search hits tried per query
const maxSourceHits = 3

// Finds candidate jars for a mod id
// Direct project lookup first, fuzzy search covers slug drift
func (m *ModrinthIndexer) SourceMod(ctx context.Context, q indexers.ModQuery) ([]indexers.ModCandidate, error) {
	direct, err := m.projectCandidate(ctx, q.ModID, q)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	if direct != nil {
		return []indexers.ModCandidate{*direct}, nil
	}

	resp, err := m.client.SearchProjects(ctx, q.ModID, "mod", q.Loaders, []string{q.McVersion}, 0, maxSourceHits)
	if err != nil {
		return nil, err
	}
	var out []indexers.ModCandidate
	for _, hit := range resp.Hits {
		if hit.ProjectID == "" {
			continue
		}
		c, err := m.projectCandidate(ctx, hit.ProjectID, q)
		if err != nil || c == nil {
			continue
		}
		c.Origin = "modrinth project " + hit.Slug
		out = append(out, *c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s project offers %q for MC %s", strings.Join(q.Loaders, "/"), q.ModID, q.McVersion)
	}
	return out, nil
}

// Builds one candidate from a project's best matching version
func (m *ModrinthIndexer) projectCandidate(ctx context.Context, projectID string, q indexers.ModQuery) (*indexers.ModCandidate, error) {
	versions, err := m.client.GetProjectVersionsFiltered(ctx, projectID, q.Loaders, []string{q.McVersion})
	if err != nil {
		return nil, err
	}
	var pick *Version
	for i := range versions {
		if versions[i].VersionType == "release" {
			pick = &versions[i]
			break
		}
	}
	if pick == nil && len(versions) > 0 {
		pick = &versions[0]
	}
	if pick == nil {
		return nil, nil
	}
	file := primaryVersionFile(pick)
	if file == nil {
		return nil, nil
	}
	algo, sum := "", ""
	switch {
	case file.Hashes.SHA512 != "":
		algo, sum = "sha512", file.Hashes.SHA512
	case file.Hashes.SHA1 != "":
		algo, sum = "sha1", file.Hashes.SHA1
	}
	return &indexers.ModCandidate{
		Origin:   "modrinth project " + projectID,
		FileName: file.Filename,
		URL:      file.URL,
		HashAlgo: algo,
		HashSum:  sum,
	}, nil
}

// Primary file wins, first file is the fallback
func primaryVersionFile(v *Version) *File {
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	if len(v.Files) > 0 {
		return &v.Files[0]
	}
	return nil
}

// Reports whether an error is an indexer 404
func isNotFound(err error) bool {
	var ie *indexers.IndexerError
	return errors.As(err, &ie) && ie.Kind == indexers.ErrNotFound
}

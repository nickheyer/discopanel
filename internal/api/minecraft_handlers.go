package api

import (
	"net/http"

	"github.com/RandomTechrate/discopanel-fork/internal/db"
	"github.com/RandomTechrate/discopanel-fork/internal/minecraft"
)

func (s *Server) handleGetMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	modloader := r.URL.Query().Get("modloader")

	var versions []string
	if modloader != "" {
		versions = minecraft.GetVersionsForModloader(db.ModLoader(modloader))
	} else {
		var err error
		versions, err = minecraft.GetVersions(string(db.ModLoaderPaper))
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
		"latest":   minecraft.GetLatestVersion(),
	})
}

func (s *Server) handleGetModLoaders(w http.ResponseWriter, r *http.Request) {
	modLoaders := minecraft.GetAllModLoaders()

	s.respondJSON(w, http.StatusOK, map[string]any{
		"modloaders": modLoaders,
	})
}

func (s *Server) handleGetDockerImages(w http.ResponseWriter, r *http.Request) {
	dockerImages := s.docker.GetDockerImages()

	s.respondJSON(w, http.StatusOK, map[string]any{
		"images": dockerImages,
	})
}

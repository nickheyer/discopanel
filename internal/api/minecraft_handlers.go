package api

import (
	"net/http"

	"github.com/nickheyer/discopanel/internal/minecraft"
)

func (s *Server) handleGetMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	versions := minecraft.GetVersions()

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

func (s *Server) handleGetImages(w http.ResponseWriter, r *http.Request) {
	containerImages := minecraft.GetContainerImages()

	s.respondJSON(w, http.StatusOK, map[string]any{
		"images": containerImages,
	})
}

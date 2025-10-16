package api

import (
    "net/http"

    "github.com/gorilla/mux"
)

// GET /mcjars/types
func (s *Server) handleMCJarsTypes(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    tr, err := s.mcjars.GetTypes(ctx)
    if err != nil {
        s.log.Error("mcjars GetTypes: %v", err)
        s.respondError(w, http.StatusInternalServerError, "failed to fetch types")
        return
    }
    s.respondJSON(w, http.StatusOK, tr)
}

// GET /mcjars/builds/{type}
func (s *Server) handleMCJarsBuildsByType(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    vars := mux.Vars(r)
    typ := vars["type"]
    builds, err := s.mcjars.GetBuildsByType(ctx, typ)
    if err != nil {
        s.log.Error("mcjars GetBuildsByType: %v", err)
        s.respondError(w, http.StatusInternalServerError, "failed to fetch builds")
        return
    }
    s.respondJSON(w, http.StatusOK, builds)
}
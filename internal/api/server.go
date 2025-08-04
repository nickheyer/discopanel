package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
)

type Server struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	log    *logger.Logger
	router *mux.Router
}

func NewServer(store *storage.Store, docker *docker.Client, cfg *config.Config, log *logger.Logger) *Server {
	s := &Server{
		store:  store,
		docker: docker,
		config: cfg,
		log:    log,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) setupRoutes() {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api/v1").Subrouter()

	// Minecraft version and mod loader endpoints
	api.HandleFunc("/minecraft/versions", s.handleGetMinecraftVersions).Methods("GET")
	api.HandleFunc("/minecraft/modloaders", s.handleGetModLoaders).Methods("GET")
	api.HandleFunc("/minecraft/docker-images", s.handleGetDockerImages).Methods("GET")

	// Server management
	api.HandleFunc("/servers", s.handleListServers).Methods("GET")
	api.HandleFunc("/servers", s.handleCreateServer).Methods("POST")
	api.HandleFunc("/servers/{id}", s.handleGetServer).Methods("GET")
	api.HandleFunc("/servers/{id}", s.handleUpdateServer).Methods("PUT")
	api.HandleFunc("/servers/{id}", s.handleDeleteServer).Methods("DELETE")
	api.HandleFunc("/servers/{id}/start", s.handleStartServer).Methods("POST")
	api.HandleFunc("/servers/{id}/stop", s.handleStopServer).Methods("POST")
	api.HandleFunc("/servers/{id}/restart", s.handleRestartServer).Methods("POST")
	api.HandleFunc("/servers/{id}/logs", s.handleGetServerLogs).Methods("GET")
	api.HandleFunc("/servers/{id}/command", s.handleSendCommand).Methods("POST")

	// Server configuration
	api.HandleFunc("/servers/{id}/config", s.handleGetServerConfig).Methods("GET")
	api.HandleFunc("/servers/{id}/config", s.handleUpdateServerConfig).Methods("PUT")
	
	// Global settings
	api.HandleFunc("/settings", s.handleGetGlobalSettings).Methods("GET")
	api.HandleFunc("/settings", s.handleUpdateGlobalSettings).Methods("PUT")
	
	// Indexed modpacks
	api.HandleFunc("/modpacks", s.handleSearchModpacks).Methods("GET")
	api.HandleFunc("/modpacks/sync", s.handleSyncModpacks).Methods("POST")
	api.HandleFunc("/modpacks/favorites", s.handleListFavorites).Methods("GET")
	api.HandleFunc("/modpacks/{id}", s.handleGetModpack).Methods("GET")
	api.HandleFunc("/modpacks/{id}/config", s.handleGetModpackConfig).Methods("GET")
	api.HandleFunc("/modpacks/{id}/favorite", s.handleToggleFavorite).Methods("POST")
	api.HandleFunc("/modpacks/{id}/files/sync", s.handleSyncModpackFiles).Methods("POST")
	api.HandleFunc("/modpacks/{id}/files", s.handleGetModpackFiles).Methods("GET")

	// Mod management
	api.HandleFunc("/servers/{id}/mods", s.handleListMods).Methods("GET")
	api.HandleFunc("/servers/{id}/mods", s.handleUploadMod).Methods("POST")
	api.HandleFunc("/servers/{id}/mods/{modId}", s.handleGetMod).Methods("GET")
	api.HandleFunc("/servers/{id}/mods/{modId}", s.handleUpdateMod).Methods("PUT")
	api.HandleFunc("/servers/{id}/mods/{modId}", s.handleDeleteMod).Methods("DELETE")

	// File management
	api.HandleFunc("/servers/{id}/files", s.handleListFiles).Methods("GET")
	api.HandleFunc("/servers/{id}/files", s.handleUploadFile).Methods("POST")
	api.HandleFunc("/servers/{id}/files/{path:.*}", s.handleGetFile).Methods("GET")
	api.HandleFunc("/servers/{id}/files/{path:.*}", s.handleUpdateFile).Methods("PUT")
	api.HandleFunc("/servers/{id}/files/{path:.*}", s.handleDeleteFile).Methods("DELETE")

	// Serve SvelteKit build output
	// The SvelteKit app should be built and output to web/build
	webRoot := filepath.Join("web", "build")

	// Serve static files from the SvelteKit build
	fileServer := http.FileServer(http.Dir(webRoot))

	// Handle all non-API routes by serving the SvelteKit app
	r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the requested file exists
		path := filepath.Join(webRoot, r.URL.Path)
		if _, err := os.Stat(path); err == nil {
			// File exists, serve it
			fileServer.ServeHTTP(w, r)
		} else {
			// File doesn't exist, serve the SvelteKit app's index.html for client-side routing
			http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
		}
	}))

	// Middleware
	r.Use(s.loggingMiddleware)
	r.Use(s.corsMiddleware)

	s.router = r
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.log.Info("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]string{"error": message})
}

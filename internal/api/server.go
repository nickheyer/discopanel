package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	web "github.com/nickheyer/discopanel/web/discopanel"
)

type Server struct {
	store        *storage.Store
	docker       *docker.Client
	config       *config.Config
	log          *logger.Logger
	router       *mux.Router
	proxyManager *proxy.Manager
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

func (s *Server) SetProxyManager(pm *proxy.Manager) {
	s.proxyManager = pm
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
	api.HandleFunc("/servers/next-port", s.handleGetNextAvailablePort).Methods("GET")
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
	
	// Proxy endpoints
	api.HandleFunc("/proxy/routes", s.handleGetProxyRoutes).Methods("GET")
	api.HandleFunc("/proxy/status", s.handleGetProxyStatus).Methods("GET")
	api.HandleFunc("/proxy/config", s.handleUpdateProxyConfig).Methods("PUT")
	api.HandleFunc("/proxy/listeners", s.handleGetProxyListeners).Methods("GET")
	api.HandleFunc("/proxy/listeners", s.handleCreateProxyListener).Methods("POST")
	api.HandleFunc("/proxy/listeners/{id}", s.handleUpdateProxyListener).Methods("PUT")
	api.HandleFunc("/proxy/listeners/{id}", s.handleDeleteProxyListener).Methods("DELETE")
	api.HandleFunc("/servers/{id}/routing", s.handleGetServerRouting).Methods("GET")
	api.HandleFunc("/servers/{id}/routing", s.handleUpdateServerRouting).Methods("PUT")
	
	// Indexed modpacks
	api.HandleFunc("/modpacks", s.handleSearchModpacks).Methods("GET")
	api.HandleFunc("/modpacks/sync", s.handleSyncModpacks).Methods("POST")
	api.HandleFunc("/modpacks/upload", s.handleUploadModpack).Methods("POST")
	api.HandleFunc("/modpacks/status", s.handleGetIndexerStatus).Methods("GET")
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

	// Serve frontend - try embedded first, fall back to filesystem for development
	var fileServer http.Handler
	var embeddedFS fs.FS
	useEmbedded := false
	
	// Try to use embedded frontend
	if buildFS, err := web.BuildFS(); err == nil {
		s.log.Info("Using embedded frontend")
		embeddedFS = buildFS
		fileServer = http.FileServer(http.FS(embeddedFS))
		useEmbedded = true
	} else {
		// Fall back to filesystem for development
		webRoot := filepath.Join("web", "discopanel", "build")
		if _, err := os.Stat(webRoot); err == nil {
			s.log.Info("Using filesystem frontend from %s", webRoot)
			fileServer = http.FileServer(http.Dir(webRoot))
		} else {
			s.log.Warn("No frontend found - API only mode")
		}
	}

	// Handle all non-API routes by serving the SvelteKit app
	if fileServer != nil {
		r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Clean the path
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			
			// Try to serve the file
			if useEmbedded {
				// Using embedded filesystem
				if file, err := embeddedFS.Open(path); err == nil {
					file.Close()
					fileServer.ServeHTTP(w, r)
				} else {
					// Serve index.html for client-side routing
					r.URL.Path = "/index.html"
					fileServer.ServeHTTP(w, r)
				}
			} else {
				// Using regular filesystem
				webRoot := filepath.Join("web", "discopanel", "build")
				fullPath := filepath.Join(webRoot, path)
				if _, err := os.Stat(fullPath); err == nil {
					fileServer.ServeHTTP(w, r)
				} else {
					// Serve index.html for client-side routing
					http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
				}
			}
		}))
	}

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

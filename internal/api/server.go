package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/auth"
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
	authManager  *auth.Manager
	authMiddleware *auth.Middleware
}

func NewServer(store *storage.Store, docker *docker.Client, cfg *config.Config, log *logger.Logger) *Server {
	// Initialize auth manager
	authManager := auth.NewManager(store)
	authMiddleware := auth.NewMiddleware(authManager, store)
	
	// Initialize auth on startup
	if err := authManager.InitializeAuth(context.Background()); err != nil {
		log.Error("Failed to initialize authentication: %v", err)
	}
	
	s := &Server{
		store:  store,
		docker: docker,
		config: cfg,
		log:    log,
		authManager: authManager,
		authMiddleware: authMiddleware,
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

	// Public auth endpoints (no authentication required)
	api.HandleFunc("/auth/status", s.handleGetAuthStatus).Methods("GET")
	api.HandleFunc("/auth/login", s.handleLogin).Methods("POST")
	api.HandleFunc("/auth/logout", s.handleLogout).Methods("POST")
	api.HandleFunc("/auth/register", s.handleRegister).Methods("POST")
	api.HandleFunc("/auth/reset-password", s.handleResetPassword).Methods("POST")

	// Protected auth endpoints (require authentication)
	authRouter := api.PathPrefix("/auth").Subrouter()
	authRouter.Use(s.authMiddleware.RequireAuth(storage.RoleViewer))
	authRouter.HandleFunc("/me", s.handleGetCurrentUser).Methods("GET")
	authRouter.HandleFunc("/change-password", s.handleChangePassword).Methods("POST")

	// User management endpoints (admin only)
	userRouter := api.PathPrefix("/users").Subrouter()
	userRouter.Use(s.authMiddleware.RequireAuth(storage.RoleAdmin))
	userRouter.HandleFunc("", s.handleListUsers).Methods("GET")
	userRouter.HandleFunc("", s.handleCreateUser).Methods("POST")
	userRouter.HandleFunc("/{id}", s.handleUpdateUser).Methods("PUT")
	userRouter.HandleFunc("/{id}", s.handleDeleteUser).Methods("DELETE")

	// Auth config endpoints - use OptionalAuth to work both with and without auth
	api.Handle("/auth/config", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleGetAuthConfig))).Methods("GET")
	api.Handle("/auth/config", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleUpdateAuthConfig))).Methods("PUT")

	// Protected API routes (require at least viewer role)
	// All server-related endpoints require authentication when enabled
	protectedAPI := api.NewRoute().Subrouter()
	protectedAPI.Use(s.authMiddleware.RequireAuth(storage.RoleViewer))

	// Minecraft version and mod loader endpoints (read-only, viewer access)
	protectedAPI.HandleFunc("/minecraft/versions", s.handleGetMinecraftVersions).Methods("GET")
	protectedAPI.HandleFunc("/minecraft/modloaders", s.handleGetModLoaders).Methods("GET")
	protectedAPI.HandleFunc("/minecraft/docker-images", s.handleGetDockerImages).Methods("GET")

	// Server management - viewer can read, editor can modify
	protectedAPI.HandleFunc("/servers", s.handleListServers).Methods("GET")
	protectedAPI.HandleFunc("/servers/next-port", s.handleGetNextAvailablePort).Methods("GET")
	protectedAPI.HandleFunc("/servers/{id}", s.handleGetServer).Methods("GET")
	protectedAPI.HandleFunc("/servers/{id}/logs", s.handleGetServerLogs).Methods("GET")
	
	// Server modification - requires editor role
	editorAPI := api.NewRoute().Subrouter()
	editorAPI.Use(s.authMiddleware.RequireAuth(storage.RoleEditor))
	editorAPI.HandleFunc("/servers", s.handleCreateServer).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}", s.handleUpdateServer).Methods("PUT")
	editorAPI.HandleFunc("/servers/{id}", s.handleDeleteServer).Methods("DELETE")
	editorAPI.HandleFunc("/servers/{id}/start", s.handleStartServer).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}/stop", s.handleStopServer).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}/restart", s.handleRestartServer).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}/command", s.handleSendCommand).Methods("POST")

	// Server configuration
	protectedAPI.HandleFunc("/servers/{id}/config", s.handleGetServerConfig).Methods("GET")
	editorAPI.HandleFunc("/servers/{id}/config", s.handleUpdateServerConfig).Methods("PUT")
	
	// Global settings - admin only when auth is enabled, accessible when auth is disabled
	api.Handle("/settings", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleGetGlobalSettings))).Methods("GET")
	api.Handle("/settings", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleUpdateGlobalSettings))).Methods("PUT")
	
	// Proxy endpoints - admin only when auth is enabled
	adminAPI := api.NewRoute().Subrouter()
	adminAPI.Use(s.authMiddleware.RequireAuth(storage.RoleAdmin))
	adminAPI.HandleFunc("/proxy/routes", s.handleGetProxyRoutes).Methods("GET")
	adminAPI.HandleFunc("/proxy/status", s.handleGetProxyStatus).Methods("GET")
	adminAPI.HandleFunc("/proxy/config", s.handleUpdateProxyConfig).Methods("PUT")
	adminAPI.HandleFunc("/proxy/listeners", s.handleGetProxyListeners).Methods("GET")
	adminAPI.HandleFunc("/proxy/listeners", s.handleCreateProxyListener).Methods("POST")
	adminAPI.HandleFunc("/proxy/listeners/{id}", s.handleUpdateProxyListener).Methods("PUT")
	adminAPI.HandleFunc("/proxy/listeners/{id}", s.handleDeleteProxyListener).Methods("DELETE")
	protectedAPI.HandleFunc("/servers/{id}/routing", s.handleGetServerRouting).Methods("GET")
	editorAPI.HandleFunc("/servers/{id}/routing", s.handleUpdateServerRouting).Methods("PUT")
	
	// Indexed modpacks - viewers can browse, editors can sync/upload
	protectedAPI.HandleFunc("/modpacks", s.handleSearchModpacks).Methods("GET")
	protectedAPI.HandleFunc("/modpacks/status", s.handleGetIndexerStatus).Methods("GET")
	protectedAPI.HandleFunc("/modpacks/favorites", s.handleListFavorites).Methods("GET")
	protectedAPI.HandleFunc("/modpacks/{id}", s.handleGetModpack).Methods("GET")
	protectedAPI.HandleFunc("/modpacks/{id}/config", s.handleGetModpackConfig).Methods("GET")
	protectedAPI.HandleFunc("/modpacks/{id}/files", s.handleGetModpackFiles).Methods("GET")
	
	editorAPI.HandleFunc("/modpacks/sync", s.handleSyncModpacks).Methods("POST")
	editorAPI.HandleFunc("/modpacks/upload", s.handleUploadModpack).Methods("POST")
	editorAPI.HandleFunc("/modpacks/{id}/favorite", s.handleToggleFavorite).Methods("POST")
	editorAPI.HandleFunc("/modpacks/{id}/files/sync", s.handleSyncModpackFiles).Methods("POST")

	// Mod management - viewers can list, editors can modify
	protectedAPI.HandleFunc("/servers/{id}/mods", s.handleListMods).Methods("GET")
	protectedAPI.HandleFunc("/servers/{id}/mods/{modId}", s.handleGetMod).Methods("GET")
	
	editorAPI.HandleFunc("/servers/{id}/mods", s.handleUploadMod).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}/mods/{modId}", s.handleUpdateMod).Methods("PUT")
	editorAPI.HandleFunc("/servers/{id}/mods/{modId}", s.handleDeleteMod).Methods("DELETE")

	// File management - viewers can read, editors can modify
	protectedAPI.HandleFunc("/servers/{id}/files", s.handleListFiles).Methods("GET")
	protectedAPI.HandleFunc("/servers/{id}/files/{path:.*}", s.handleGetFile).Methods("GET")
	
	editorAPI.HandleFunc("/servers/{id}/files", s.handleUploadFile).Methods("POST")
	editorAPI.HandleFunc("/servers/{id}/files/{path:.*}", s.handleUpdateFile).Methods("PUT")
	editorAPI.HandleFunc("/servers/{id}/files/{path:.*}", s.handleDeleteFile).Methods("DELETE")

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
	r.Use(s.authMiddleware.CheckAuthStatus()) // Add auth status to all responses

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

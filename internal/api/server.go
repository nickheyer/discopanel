package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	store          *storage.Store
	docker         *docker.Client
	config         *config.Config
	log            *logger.Logger
	router         *mux.Router
	proxyManager   *proxy.Manager
	authManager    *auth.Manager
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
		store:          store,
		docker:         docker,
		config:         cfg,
		log:            log,
		authManager:    authManager,
		authMiddleware: authMiddleware,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) setupRoutes() {
	r := mux.NewRouter()

	// Apply global middleware
	r.Use(s.loggingMiddleware)
	r.Use(s.corsMiddleware)
	r.Use(s.authMiddleware.CheckAuthStatus())

	// Setup API routes
	api := r.PathPrefix("/api/v1").Subrouter()
	s.setupAuthRoutes(api)
	s.setupUserRoutes(api)
	s.setupServerRoutes(api)
	s.setupProxyRoutes(api)
	s.setupModpackRoutes(api)
	s.setupSettingsRoutes(api)

	// Setup frontend serving
	s.setupFrontend(r)

	s.router = r
}

func (s *Server) setupAuthRoutes(api *mux.Router) {
	// Public auth endpoints
	api.HandleFunc("/auth/status", s.handleGetAuthStatus).Methods("GET")
	api.HandleFunc("/auth/login", s.handleLogin).Methods("POST")
	api.HandleFunc("/auth/logout", s.handleLogout).Methods("POST")
	api.HandleFunc("/auth/register", s.handleRegister).Methods("POST")
	api.HandleFunc("/auth/reset-password", s.handleResetPassword).Methods("POST")

	// Auth config (optional auth)
	api.Handle("/auth/config", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleGetAuthConfig))).Methods("GET")
	api.Handle("/auth/config", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleUpdateAuthConfig))).Methods("PUT")

	// Protected auth endpoints
	auth := api.PathPrefix("/auth").Subrouter()
	auth.Use(s.authMiddleware.RequireAuth(storage.RoleViewer))
	auth.HandleFunc("/me", s.handleGetCurrentUser).Methods("GET")
	auth.HandleFunc("/change-password", s.handleChangePassword).Methods("POST")
}

func (s *Server) setupUserRoutes(api *mux.Router) {
	users := api.PathPrefix("/users").Subrouter()
	users.Use(s.authMiddleware.RequireAuth(storage.RoleAdmin))
	users.HandleFunc("", s.handleListUsers).Methods("GET")
	users.HandleFunc("", s.handleCreateUser).Methods("POST")
	users.HandleFunc("/{id}", s.handleUpdateUser).Methods("PUT")
	users.HandleFunc("/{id}", s.handleDeleteUser).Methods("DELETE")
}

func (s *Server) setupServerRoutes(api *mux.Router) {
	// Viewer-level server routes
	viewer := api.NewRoute().Subrouter()
	viewer.Use(s.authMiddleware.RequireAuth(storage.RoleViewer))

	// Minecraft info
	viewer.HandleFunc("/minecraft/versions", s.handleGetMinecraftVersions).Methods("GET")
	viewer.HandleFunc("/minecraft/modloaders", s.handleGetModLoaders).Methods("GET")
	viewer.HandleFunc("/minecraft/docker-images", s.handleGetDockerImages).Methods("GET")

	// Server read operations
	viewer.HandleFunc("/servers", s.handleListServers).Methods("GET")
	viewer.HandleFunc("/servers/next-port", s.handleGetNextAvailablePort).Methods("GET")
	viewer.HandleFunc("/servers/{id}", s.handleGetServer).Methods("GET")
	viewer.HandleFunc("/servers/{id}/logs", s.handleGetServerLogs).Methods("GET")
	viewer.HandleFunc("/servers/{id}/config", s.handleGetServerConfig).Methods("GET")
	viewer.HandleFunc("/servers/{id}/routing", s.handleGetServerRouting).Methods("GET")

	// File read operations
	viewer.HandleFunc("/servers/{id}/files", s.handleListFiles).Methods("GET")
	viewer.HandleFunc("/servers/{id}/files/{path:.*}", s.handleGetFile).Methods("GET")

	// Mod read operations
	viewer.HandleFunc("/servers/{id}/mods", s.handleListMods).Methods("GET")
	viewer.HandleFunc("/servers/{id}/mods/{modId}", s.handleGetMod).Methods("GET")

	// Editor-level server routes
	editor := api.NewRoute().Subrouter()
	editor.Use(s.authMiddleware.RequireAuth(storage.RoleEditor))

	// Server write operations
	editor.HandleFunc("/servers", s.handleCreateServer).Methods("POST")
	editor.HandleFunc("/servers/{id}", s.handleUpdateServer).Methods("PUT")
	editor.HandleFunc("/servers/{id}", s.handleDeleteServer).Methods("DELETE")
	editor.HandleFunc("/servers/{id}/start", s.handleStartServer).Methods("POST")
	editor.HandleFunc("/servers/{id}/stop", s.handleStopServer).Methods("POST")
	editor.HandleFunc("/servers/{id}/restart", s.handleRestartServer).Methods("POST")
	editor.HandleFunc("/servers/{id}/command", s.handleSendCommand).Methods("POST")
	editor.HandleFunc("/servers/{id}/config", s.handleUpdateServerConfig).Methods("PUT")
	editor.HandleFunc("/servers/{id}/routing", s.handleUpdateServerRouting).Methods("PUT")

	// File write operations
	editor.HandleFunc("/servers/{id}/files", s.handleUploadFile).Methods("POST")
	editor.HandleFunc("/servers/{id}/files/{path:.*}", s.handleUpdateFile).Methods("PUT")
	editor.HandleFunc("/servers/{id}/files/{path:.*}", s.handleDeleteFile).Methods("DELETE")

	// Mod write operations
	editor.HandleFunc("/servers/{id}/mods", s.handleUploadMod).Methods("POST")
	editor.HandleFunc("/servers/{id}/mods/{modId}", s.handleUpdateMod).Methods("PUT")
	editor.HandleFunc("/servers/{id}/mods/{modId}", s.handleDeleteMod).Methods("DELETE")
}

func (s *Server) setupProxyRoutes(api *mux.Router) {
	proxy := api.PathPrefix("/proxy").Subrouter()
	proxy.Use(s.authMiddleware.RequireAuth(storage.RoleAdmin))
	proxy.HandleFunc("/routes", s.handleGetProxyRoutes).Methods("GET")
	proxy.HandleFunc("/status", s.handleGetProxyStatus).Methods("GET")
	proxy.HandleFunc("/config", s.handleUpdateProxyConfig).Methods("PUT")
	proxy.HandleFunc("/listeners", s.handleGetProxyListeners).Methods("GET")
	proxy.HandleFunc("/listeners", s.handleCreateProxyListener).Methods("POST")
	proxy.HandleFunc("/listeners/{id}", s.handleUpdateProxyListener).Methods("PUT")
	proxy.HandleFunc("/listeners/{id}", s.handleDeleteProxyListener).Methods("DELETE")
}

func (s *Server) setupModpackRoutes(api *mux.Router) {
	// Viewer-level modpack routes
	viewer := api.PathPrefix("/modpacks").Subrouter()
	viewer.Use(s.authMiddleware.RequireAuth(storage.RoleViewer))
	viewer.HandleFunc("", s.handleSearchModpacks).Methods("GET")
	viewer.HandleFunc("/status", s.handleGetIndexerStatus).Methods("GET")
	viewer.HandleFunc("/favorites", s.handleListFavorites).Methods("GET")
	viewer.HandleFunc("/{id}", s.handleGetModpack).Methods("GET")
	viewer.HandleFunc("/{id}/config", s.handleGetModpackConfig).Methods("GET")
	viewer.HandleFunc("/{id}/files", s.handleGetModpackFiles).Methods("GET")

	// Editor-level modpack routes
	editor := api.PathPrefix("/modpacks").Subrouter()
	editor.Use(s.authMiddleware.RequireAuth(storage.RoleEditor))
	editor.HandleFunc("/sync", s.handleSyncModpacks).Methods("POST")
	editor.HandleFunc("/upload", s.handleUploadModpack).Methods("POST")
	editor.HandleFunc("/{id}/favorite", s.handleToggleFavorite).Methods("POST")
	editor.HandleFunc("/{id}/files/sync", s.handleSyncModpackFiles).Methods("POST")
}

func (s *Server) setupSettingsRoutes(api *mux.Router) {
	api.Handle("/settings", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleGetGlobalSettings))).Methods("GET")
	api.Handle("/settings", s.authMiddleware.OptionalAuth()(http.HandlerFunc(s.handleUpdateGlobalSettings))).Methods("PUT")
}

func (s *Server) setupFrontend(r *mux.Router) {
	// Get front end source (embedded or otherwise)
	fs := s.getFrontendFS()
	if fs == nil {
		s.log.Warn("No frontend found - API only mode")
		return
	}

	// Serve frontend for all non-API routes
	r.PathPrefix("/").Handler(s.createFrontendHandler(fs))
}

func (s *Server) getFrontendFS() http.FileSystem {
	// Try embedded frontend first
	if buildFS, err := web.BuildFS(); err == nil {
		s.log.Info("Using embedded frontend")
		return http.FS(buildFS)
	}

	return nil
}

func (s *Server) createFrontendHandler(fs http.FileSystem) http.HandlerFunc {
	fileServer := http.FileServer(fs)

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to open the requested file
		file, err := fs.Open(path)
		if err == nil {
			file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html
		indexFile, err := fs.Open("index.html")
		if err == nil {
			indexFile.Close()

			// Serve index.html but preserve original path
			newReq := r.Clone(r.Context())
			newReq.URL.Path = "/index.html"
			fileServer.ServeHTTP(w, newReq)
			return
		}

		http.NotFound(w, r)
	}
}

func (s *Server) SetProxyManager(pm *proxy.Manager) {
	s.proxyManager = pm
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

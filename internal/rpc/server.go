package rpc

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/internal/rpc/services"
	"github.com/nickheyer/discopanel/internal/scheduler"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	web "github.com/nickheyer/discopanel/web/discopanel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// Server represents the Connect RPC server
type Server struct {
	store          *storage.Store
	docker         *docker.Client
	config         *config.Config
	log            *logger.Logger
	handler        http.Handler
	proxyManager   *proxy.Manager
	authManager    *auth.Manager
	authMiddleware *auth.Middleware
	logStreamer    *logger.LogStreamer
	scheduler      *scheduler.Scheduler
}

// Creates new Connect RPC server
func NewServer(store *storage.Store, docker *docker.Client, cfg *config.Config, proxyManager *proxy.Manager, sched *scheduler.Scheduler, log *logger.Logger) *Server {
	// Initialize auth manager
	authManager := auth.NewManager(store)
	authMiddleware := auth.NewMiddleware(authManager, store)

	// Initialize auth on startup
	if err := authManager.InitializeAuth(context.Background()); err != nil {
		log.Error("Failed to initialize authentication: %v", err)
	}

	// Initialize log streamer
	logStreamer := logger.NewLogStreamer(docker.GetDockerClient(), log, 10000)

	s := &Server{
		store:          store,
		docker:         docker,
		config:         cfg,
		log:            log,
		proxyManager:   proxyManager,
		authManager:    authManager,
		authMiddleware: authMiddleware,
		logStreamer:    logStreamer,
		scheduler:      sched,
	}

	s.setupHandler()
	return s
}

// Setup all Connect RPC handlers
func (s *Server) setupHandler() {
	mux := http.NewServeMux()

	// Configure Connect options
	interceptors := []connect.Interceptor{
		s.loggingInterceptor(),
		s.authInterceptor(),
	}

	opts := []connect.HandlerOption{
		connect.WithInterceptors(interceptors...),
		// Enable gRPC, gRPC-Web, and Connect protocols
		connect.WithHandlerOptions(
			connect.WithCompression("gzip", nil, nil),
		),
	}

	// Register all service handlers
	s.registerServices(mux, opts)

	// Add reflection for gRPC clients
	reflector := grpcreflect.NewStaticReflector(
		discopanelv1connect.AuthServiceName,
		discopanelv1connect.ConfigServiceName,
		discopanelv1connect.FileServiceName,
		discopanelv1connect.MinecraftServiceName,
		discopanelv1connect.ModServiceName,
		discopanelv1connect.ModpackServiceName,
		discopanelv1connect.ProxyServiceName,
		discopanelv1connect.ServerServiceName,
		discopanelv1connect.SupportServiceName,
		discopanelv1connect.TaskServiceName,
		discopanelv1connect.UserServiceName,
	)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// Serve frontend for non-RPC routes
	s.setupFrontend(mux)

	// Use h2c for HTTP/2 cleartext (for development)
	// In production, use TLS
	s.handler = h2c.NewHandler(mux, &http2.Server{})
}

// Registers all Connect RPC service handlers
func (s *Server) registerServices(mux *http.ServeMux, opts []connect.HandlerOption) {
	// Create service instances
	authService := services.NewAuthService(s.store, s.authManager, s.log)
	configService := services.NewConfigService(s.store, s.config, s.docker, s.log)
	fileService := services.NewFileService(s.store, s.docker, s.log)
	minecraftService := services.NewMinecraftService(s.store, s.docker, s.log)
	modService := services.NewModService(s.store, s.docker, s.log)
	modpackService := services.NewModpackService(s.store, s.config, s.log)
	proxyService := services.NewProxyService(s.store, s.proxyManager, s.config, s.log)
	serverService := services.NewServerService(s.store, s.docker, s.config, s.proxyManager, s.logStreamer, s.log)
	supportService := services.NewSupportService(s.store, s.docker, s.config, s.log)
	taskService := services.NewTaskService(s.store, s.scheduler, s.log)
	userService := services.NewUserService(s.store, s.authManager, s.log)

	// Register service handlers
	authPath, authHandler := discopanelv1connect.NewAuthServiceHandler(authService, opts...)
	mux.Handle(authPath, authHandler)

	configPath, configHandler := discopanelv1connect.NewConfigServiceHandler(configService, opts...)
	mux.Handle(configPath, configHandler)

	filePath, fileHandler := discopanelv1connect.NewFileServiceHandler(fileService, opts...)
	mux.Handle(filePath, fileHandler)

	minecraftPath, minecraftHandler := discopanelv1connect.NewMinecraftServiceHandler(minecraftService, opts...)
	mux.Handle(minecraftPath, minecraftHandler)

	modPath, modHandler := discopanelv1connect.NewModServiceHandler(modService, opts...)
	mux.Handle(modPath, modHandler)

	modpackPath, modpackHandler := discopanelv1connect.NewModpackServiceHandler(modpackService, opts...)
	mux.Handle(modpackPath, modpackHandler)

	proxyPath, proxyHandler := discopanelv1connect.NewProxyServiceHandler(proxyService, opts...)
	mux.Handle(proxyPath, proxyHandler)

	serverPath, serverHandler := discopanelv1connect.NewServerServiceHandler(serverService, opts...)
	mux.Handle(serverPath, serverHandler)

	supportPath, supportHandler := discopanelv1connect.NewSupportServiceHandler(supportService, opts...)
	mux.Handle(supportPath, supportHandler)

	taskPath, taskHandler := discopanelv1connect.NewTaskServiceHandler(taskService, opts...)
	mux.Handle(taskPath, taskHandler)

	userPath, userHandler := discopanelv1connect.NewUserServiceHandler(userService, opts...)
	mux.Handle(userPath, userHandler)
}

// The HTTP handler for the server
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Creates a Connect interceptor for logging
func (s *Server) loggingInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Skip logging for polling endpoints
			if !s.isPollingProcedure(req.Spec().Procedure) {
				s.log.Info("RPC %s %s", req.Peer().Addr, req.Spec().Procedure)
			}
			return next(ctx, req)
		}
	}
}

// Creates a Connect interceptor for authentication
func (s *Server) authInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Extract token from auth header
			token := ""
			if authHeader := req.Header().Get("Authorization"); authHeader != "" {
				token, _ = strings.CutPrefix(authHeader, "Bearer ")
				token, _ = strings.CutPrefix(token, "bearer ")
			}

			// Validate user session or return valid anon user/session if auth disabled
			user, err := s.authManager.ValidateSession(ctx, token)
			if err == nil && user != nil {
				ctx = context.WithValue(ctx, auth.UserContextKey, user)
			} else if err != nil {
				s.log.Debug("Auth: Token validation failed for %s: %v", req.Spec().Procedure, err)
			}

			return next(ctx, req)
		}
	}
}

// Checks if a procedure is a polling endpoint
func (s *Server) isPollingProcedure(procedure string) bool {
	pollingProcedures := []string{
		"/discopanel.v1.AuthService/GetAuthStatus",
		"/discopanel.v1.ServerService/ListServers",
		"/discopanel.v1.ServerService/GetServer",
		"/discopanel.v1.ProxyService/GetProxyStatus",
	}

	return slices.Contains(pollingProcedures, procedure)
}

// Frontend serving
func (s *Server) setupFrontend(mux *http.ServeMux) {
	// Get frontend source
	fs := s.getFrontendFS()
	if fs == nil {
		s.log.Warn("No frontend found - API only mode")
		return
	}

	// Serve frontend for root path
	mux.Handle("/", s.createFrontendHandler(fs))
}

// Get frontend fs
func (s *Server) getFrontendFS() http.FileSystem {
	// Try embedded frontend first
	if buildFS, err := web.BuildFS(); err == nil {
		s.log.Info("Using embedded frontend")
		return http.FS(buildFS)
	}
	return nil
}

// Create frontend handler
func (s *Server) createFrontendHandler(fs http.FileSystem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only serve frontend for non-Connect paths
		if isConnectPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		file, err := fs.Open(path)
		if err == nil {
			defer file.Close()
			stat, _ := file.Stat()
			http.ServeContent(w, r, path, stat.ModTime(), file)
			return
		}

		// Serve index.html for client-side routing
		indexFile, err := fs.Open("/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer indexFile.Close()

		stat, _ := indexFile.Stat()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "/index.html", stat.ModTime(), indexFile)
	}
}

// Checks if a path is a Connect RPC path
func isConnectPath(path string) bool {
	// Connect paths start with service names
	connectPrefixes := []string{
		"/discopanel.v1.",
		"/grpc.reflection.",
		"/connect.",
	}

	for _, prefix := range connectPrefixes {
		if len(path) > len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// Starts log streaming for a container
func (s *Server) StartLogStreaming(containerID string) error {
	return s.logStreamer.StartStreaming(containerID)
}

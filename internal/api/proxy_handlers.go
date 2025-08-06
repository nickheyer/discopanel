package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	storage "github.com/nickheyer/discopanel/internal/db"
)

func (s *Server) handleGetProxyRoutes(w http.ResponseWriter, r *http.Request) {
	if s.proxyManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Proxy not enabled")
		return
	}

	routes := s.proxyManager.GetRoutes()

	// Convert to response format
	type RouteResponse struct {
		ServerID    string `json:"server_id"`
		Hostname    string `json:"hostname"`
		BackendHost string `json:"backend_host"`
		BackendPort int    `json:"backend_port"`
		Active      bool   `json:"active"`
	}

	routeList := make([]RouteResponse, 0, len(routes))
	for _, route := range routes {
		routeList = append(routeList, RouteResponse{
			ServerID:    route.ServerID,
			Hostname:    route.Hostname,
			BackendHost: route.BackendHost,
			BackendPort: route.BackendPort,
			Active:      route.Active,
		})
	}

	s.respondJSON(w, http.StatusOK, routeList)
}

func (s *Server) handleGetProxyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Load proxy config from database
	proxyConfig, _, err := s.store.GetProxyConfig(ctx)
	if err != nil {
		s.log.Error("Failed to load proxy configuration: %v", err)
		proxyConfig = &storage.ProxyConfig{
			Enabled: s.config.Proxy.Enabled,
			BaseURL: s.config.Proxy.BaseURL,
		}
	}

	// Get listeners
	listeners, err := s.store.GetProxyListeners(ctx)
	if err != nil {
		s.log.Error("Failed to load proxy listeners: %v", err)
		listeners = []*storage.ProxyListener{}
	}

	// Convert listeners to ports array for backward compatibility
	listenPorts := make([]int, len(listeners))
	for i, l := range listeners {
		listenPorts[i] = l.Port
	}

	status := map[string]any{
		"enabled":      proxyConfig.Enabled,
		"base_url":     proxyConfig.BaseURL,
		"listen_ports": listenPorts,
		"listeners":    listeners,
	}

	if len(listenPorts) > 0 {
		status["listen_port"] = listenPorts[0] // Primary port for backward compatibility
	}

	if s.proxyManager != nil {
		status["running"] = s.proxyManager.IsRunning()

		// Get route count
		routes := s.proxyManager.GetRoutes()
		status["active_routes"] = len(routes)
	} else {
		status["running"] = false
		status["active_routes"] = 0
	}

	s.respondJSON(w, http.StatusOK, status)
}

func (s *Server) handleUpdateProxyConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Enabled bool   `json:"enabled"`
		BaseURL string `json:"base_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Save to database
	proxyConfig := &storage.ProxyConfig{
		ID:      "default",
		Enabled: req.Enabled,
		BaseURL: req.BaseURL,
	}

	if err := s.store.SaveProxyConfig(ctx, proxyConfig); err != nil {
		s.log.Error("Failed to save proxy configuration: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save proxy configuration")
		return
	}

	// Update in-memory configuration
	s.config.Proxy.Enabled = req.Enabled
	s.config.Proxy.BaseURL = req.BaseURL

	s.log.Info("Proxy configuration saved to database: enabled=%v, base_url=%v", req.Enabled, req.BaseURL)

	// Return updated status
	s.handleGetProxyStatus(w, r)
}

// Proxy Listener management
func (s *Server) handleGetProxyListeners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	listeners, err := s.store.GetProxyListeners(ctx)
	if err != nil {
		s.log.Error("Failed to get proxy listeners: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get proxy listeners")
		return
	}

	// Add server count for each listener
	type ListenerResponse struct {
		*storage.ProxyListener
		ServerCount int `json:"server_count"`
	}

	response := make([]ListenerResponse, len(listeners))
	for i, listener := range listeners {
		// Count servers using this listener
		servers, _ := s.store.ListServers(ctx)
		count := 0
		for _, server := range servers {
			if server.ProxyListenerID == listener.ID {
				count++
			}
		}

		response[i] = ListenerResponse{
			ProxyListener: listener,
			ServerCount:   count,
		}
	}

	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateProxyListener(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req storage.ProxyListener
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate port
	if req.Port < 1 || req.Port > 65535 {
		s.respondError(w, http.StatusBadRequest, "Invalid port number")
		return
	}

	// Check if port is already in use
	existing, _ := s.store.GetProxyListenerByPort(ctx, req.Port)
	if existing != nil {
		s.respondError(w, http.StatusBadRequest, "Port already in use by another listener")
		return
	}

	// Check if port is used by a non-proxied server
	servers, _ := s.store.ListServers(ctx)
	for _, server := range servers {
		if server.ProxyHostname == "" && server.Port == req.Port {
			s.respondError(w, http.StatusBadRequest, "Port already in use by a non-proxied server")
			return
		}
	}

	if err := s.store.CreateProxyListener(ctx, &req); err != nil {
		s.log.Error("Failed to create proxy listener: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create proxy listener")
		return
	}

	s.respondJSON(w, http.StatusCreated, req)
}

func (s *Server) handleUpdateProxyListener(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	listener, err := s.store.GetProxyListener(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Listener not found")
		return
	}

	var req storage.ProxyListener
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	listener.Name = req.Name
	listener.Description = req.Description
	listener.Enabled = req.Enabled
	listener.IsDefault = req.IsDefault

	// If setting as default, unset other defaults
	if req.IsDefault {
		listeners, _ := s.store.GetProxyListeners(ctx)
		for _, l := range listeners {
			if l.ID != id && l.IsDefault {
				l.IsDefault = false
				s.store.UpdateProxyListener(ctx, l)
			}
		}
	}

	if err := s.store.UpdateProxyListener(ctx, listener); err != nil {
		s.log.Error("Failed to update proxy listener: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update proxy listener")
		return
	}

	s.respondJSON(w, http.StatusOK, listener)
}

func (s *Server) handleDeleteProxyListener(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	if err := s.store.DeleteProxyListener(ctx, id); err != nil {
		if strings.Contains(err.Error(), "servers are using it") {
			s.respondError(w, http.StatusBadRequest, err.Error())
		} else {
			s.log.Error("Failed to delete proxy listener: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to delete proxy listener")
		}
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleGetServerRouting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Get suggested hostname if not set
	suggestedHostname := ""
	if server.ProxyHostname == "" && s.config.Proxy.BaseURL != "" {
		suggestedHostname = strings.ToLower(strings.ReplaceAll(server.Name, " ", "-")) + "." + s.config.Proxy.BaseURL
	}

	// Check if proxy is enabled and get current route
	var currentRoute *struct {
		Hostname string `json:"hostname"`
		Active   bool   `json:"active"`
	}

	if s.proxyManager != nil && s.config.Proxy.Enabled {
		routes := s.proxyManager.GetRoutes()
		for hostname, route := range routes {
			if route.ServerID == server.ID {
				currentRoute = &struct {
					Hostname string `json:"hostname"`
					Active   bool   `json:"active"`
				}{
					Hostname: hostname,
					Active:   route.Active,
				}
				break
			}
		}
	}

	response := map[string]any{
		"proxy_enabled":      s.config.Proxy.Enabled,
		"proxy_hostname":     server.ProxyHostname,
		"suggested_hostname": suggestedHostname,
		"base_url":           s.config.Proxy.BaseURL,
		"listen_port":        s.config.Proxy.ListenPort,
		"current_route":      currentRoute,
	}

	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) handleUpdateServerRouting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	var req struct {
		ProxyHostname string `json:"proxy_hostname"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate hostname
	hostname := strings.TrimSpace(strings.ToLower(req.ProxyHostname))
	if hostname != "" {
		// Basic hostname validation
		if strings.Contains(hostname, " ") || strings.Contains(hostname, "://") {
			s.respondError(w, http.StatusBadRequest, "Invalid hostname format")
			return
		}

		// Check for conflicts with other servers
		servers, err := s.store.ListServers(ctx)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to check hostname conflicts")
			return
		}

		for _, srv := range servers {
			if srv.ID != server.ID && srv.ProxyHostname == hostname {
				s.respondError(w, http.StatusConflict, "Hostname already in use by another server")
				return
			}
		}
	}

	// Update server hostname
	server.ProxyHostname = hostname
	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to update server")
		return
	}

	// Update proxy route if server is running
	if s.proxyManager != nil && s.config.Proxy.Enabled && server.Status == "running" {
		if err := s.proxyManager.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
			// Not critical, continue
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"status":   "Routing updated successfully",
		"hostname": hostname,
	})
}

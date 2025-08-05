package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
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
	status := map[string]interface{}{
		"enabled": s.config.Proxy.Enabled,
	}
	
	if s.proxyManager != nil {
		status["running"] = s.proxyManager.IsRunning()
		status["listen_port"] = s.config.Proxy.ListenPort
		status["base_url"] = s.config.Proxy.BaseURL
		
		// Get route count
		routes := s.proxyManager.GetRoutes()
		status["active_routes"] = len(routes)
	} else {
		status["running"] = false
	}
	
	s.respondJSON(w, http.StatusOK, status)
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

	response := map[string]interface{}{
		"proxy_enabled":      s.config.Proxy.Enabled,
		"proxy_hostname":     server.ProxyHostname,
		"suggested_hostname": suggestedHostname,
		"base_url":          s.config.Proxy.BaseURL,
		"listen_port":       s.config.Proxy.ListenPort,
		"current_route":     currentRoute,
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
		"status": "Routing updated successfully",
		"hostname": hostname,
	})
}
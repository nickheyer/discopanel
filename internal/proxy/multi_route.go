package proxy

// ProtocolBackend defines a backend for a specific protocol
type ProtocolBackend struct {
	ModuleID    string // Associated module ID (if any)
	BackendHost string
	BackendPort int
	Active      bool
}

// Note: The Route struct in proxy.go is extended with an HTTPBackend field
// This allows backward compatibility while supporting multi-protocol routing
//
// The flow is:
// 1. Protocol detection determines if traffic is HTTP or Minecraft
// 2. For Minecraft: Use the existing BackendHost/BackendPort
// 3. For HTTP: Use HTTPBackend if configured
//
// When a module is created with HTTP protocol, it registers itself as
// the HTTPBackend for the server's route (since modules inherit the
// server's hostname).

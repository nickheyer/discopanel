package network

import (
	"net"
	"testing"
)

func TestGetHostIP(t *testing.T) {
	ip := GetHostIP()
	
	// The test should pass even if no IP is found (e.g., in some CI environments)
	// but if an IP is found, it should be valid
	if ip == "" {
		t.Log("No host IP found - this is acceptable in some environments")
		return
	}

	// Validate that the returned IP is a valid IPv4 address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		t.Fatalf("GetHostIP returned invalid IP: %s", ip)
	}

	if parsedIP.To4() == nil {
		t.Fatalf("GetHostIP returned non-IPv4 address: %s", ip)
	}

	// Verify it's not a loopback address
	if parsedIP.IsLoopback() {
		t.Fatalf("GetHostIP returned loopback address: %s", ip)
	}

	t.Logf("GetHostIP returned: %s", ip)
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"192.168.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"127.0.0.1", false},
		{"172.15.255.255", false},
		{"172.32.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse IP: %s", tt.ip)
			}
			result := isPrivateIP(ip)
			if result != tt.expected {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestCompareIPs(t *testing.T) {
	tests := []struct {
		a        string
		b        string
		expected int
	}{
		{"10.0.0.1", "10.0.0.1", 0},
		{"10.0.0.1", "10.0.0.2", -1},
		{"10.0.0.2", "10.0.0.1", 1},
		{"192.168.1.1", "192.168.1.255", -1},
		{"192.168.2.1", "192.168.1.255", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a := net.ParseIP(tt.a)
			b := net.ParseIP(tt.b)
			result := compareIPs(a, b)
			if result != tt.expected {
				t.Errorf("compareIPs(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

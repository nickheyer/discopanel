package network

import (
	"net"
	"sort"
)

// GetHostIP returns the best available non-loopback IP address of the host.
// It prioritizes private network addresses and returns the first suitable one.
// Returns empty string if no suitable address is found.
func GetHostIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	// Collect all valid IPs and categorize them
	var privateIPs []string
	var publicIPs []string

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// Skip loopback addresses
		if ipNet.IP.IsLoopback() {
			continue
		}

		// Only consider IPv4 addresses for simplicity
		if ipNet.IP.To4() == nil {
			continue
		}

		ipStr := ipNet.IP.String()

		// Categorize by private vs public
		if isPrivateIP(ipNet.IP) {
			privateIPs = append(privateIPs, ipStr)
		} else {
			publicIPs = append(publicIPs, ipStr)
		}
	}

	// Sort to ensure consistent ordering (lexicographic, not numerical)
	// This provides deterministic selection when multiple IPs are available
	sort.Strings(privateIPs)
	sort.Strings(publicIPs)

	// Prefer private IPs (typically for local network scenarios)
	if len(privateIPs) > 0 {
		return privateIPs[0]
	}

	// Fall back to public IPs if no private IPs found
	if len(publicIPs) > 0 {
		return publicIPs[0]
	}

	return ""
}

// isPrivateIP checks if an IP address is in a private range
func isPrivateIP(ip net.IP) bool {
	// Define private IP ranges
	privateRanges := []struct {
		min net.IP
		max net.IP
	}{
		{net.ParseIP("10.0.0.0"), net.ParseIP("10.255.255.255")},
		{net.ParseIP("172.16.0.0"), net.ParseIP("172.31.255.255")},
		{net.ParseIP("192.168.0.0"), net.ParseIP("192.168.255.255")},
	}

	for _, r := range privateRanges {
		if inRange(ip, r.min, r.max) {
			return true
		}
	}

	return false
}

// inRange checks if an IP is within a given range
func inRange(ip, min, max net.IP) bool {
	return compareIPs(ip, min) >= 0 && compareIPs(ip, max) <= 0
}

// compareIPs compares two IP addresses
func compareIPs(a, b net.IP) int {
	a4 := a.To4()
	b4 := b.To4()

	if a4 == nil || b4 == nil {
		return 0
	}

	for i := 0; i < len(a4); i++ {
		if a4[i] < b4[i] {
			return -1
		}
		if a4[i] > b4[i] {
			return 1
		}
	}

	return 0
}

package runtimespec

import (
	"strconv"
	"strings"
)

// Parses memory strings like 4096M or 12G to MB
func ParseMemoryMB(s string) int {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "G"):
		mult = 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		s = strings.TrimSuffix(s, "K")
		if v, err := strconv.Atoi(s); err == nil {
			return v / 1024
		}
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v * mult
}

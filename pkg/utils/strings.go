package utils

func DeduplicateStrings(strings []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, s := range strings {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

package minecraft

import (
	"regexp"
	"strconv"
	"strings"

	models "github.com/nickheyer/discopanel/internal/db"
)

// ParseTPSFromOutput parses TPS value from various server command outputs
func ParseTPSFromOutput(output string) float64 {
	// Remove color codes and clean the output
	output = stripMinecraftColors(output)

	// Try different parsing patterns
	patterns := []struct {
		regex   *regexp.Regexp
		extract func([]string) float64
	}{
		// NeoForge/Forge with Overall format: "Overall: 20.000 TPS"
		// This should be checked first as it's the most reliable when present
		{
			regex: regexp.MustCompile(`Overall:\s*([\d.]+)\s*TPS`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		// Vanilla/Paper/Spigot format: "TPS from last 1m, 5m, 15m: 20.0, 20.0, 20.0"
		{
			regex: regexp.MustCompile(`TPS from last .*?:\s*([\d.]+)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		// Forge format: "Dim 0 (overworld): Mean tick time: 2.333 ms. Mean TPS: 20.0"
		{
			regex: regexp.MustCompile(`Mean TPS:\s*([\d.]+)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		// Overworld-specific format (fallback if no Overall): "Overworld: 20.000 TPS"
		{
			regex: regexp.MustCompile(`Overworld:\s*([\d.]+)\s*TPS`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		// Generic TPS format: "TPS: 20.0" or "tps: 20.0"
		{
			regex: regexp.MustCompile(`(?i)tps[\s:]+(\d+\.?\d*)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		// Just find any float number between 0 and 20 (last resort)
		{
			regex: regexp.MustCompile(`\b(\d{1,2}\.?\d*)\b`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					// TPS should be between 0 and 20
					if val > 0 && val <= 20 {
						return val
					}
				}
				return 0
			},
		},
	}

	for _, pattern := range patterns {
		matches := pattern.regex.FindStringSubmatch(output)
		if tps := pattern.extract(matches); tps > 0 {
			return tps
		}
	}

	return 0
}

// ParsePlayerListFromOutput parses player count and names from list command output
func ParsePlayerListFromOutput(output string) (int, []string) {
	output = stripMinecraftColors(output)

	// Common formats:
	// "There are X of a max Y players online: player1, player2"
	// "There are X/Y players online: player1, player2"
	// "Players online (X): player1, player2"

	var count int
	var players []string

	// Extract player count
	re := regexp.MustCompile(`(\d+)\s*(?:of|/)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		count, _ = strconv.Atoi(matches[1])
	}

	// Extract player names after colon
	colonIdx := strings.Index(output, ":")
	if colonIdx != -1 && colonIdx < len(output)-1 {
		playersPart := output[colonIdx+1:]
		names := strings.SplitSeq(playersPart, ",")
		for name := range names {
			cleaned := strings.TrimSpace(name)
			if cleaned != "" && cleaned != "None" {
				players = append(players, cleaned)
			}
		}
	}

	// If we found a count but no players, return the count
	if count > 0 && len(players) == 0 {
		return count, []string{}
	}

	// If we found players, use their count
	if len(players) > 0 {
		return len(players), players
	}

	return 0, []string{}
}

func stripMinecraftColors(text string) string {
	// Remove Minecraft color codes (§ followed by a character)
	re := regexp.MustCompile(`§.`)
	text = re.ReplaceAllString(text, "")

	// Also remove ANSI color codes
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	text = ansiRe.ReplaceAllString(text, "")

	return text
}

func GetTPSCommand(modLoader models.ModLoader) string {
	// Determine TPS command based on modloader
	switch modLoader {
	case models.ModLoaderPaper, models.ModLoaderSpigot, models.ModLoaderBukkit, models.ModLoaderPufferfish:
		return "tps"
	case models.ModLoaderForge:
		return "forge tps"
	case models.ModLoaderNeoForge:
		return "neoforge tps"
	// Fabric-based need Carpet mod for TPS
	// Hybrids might support TPS if they have Bukkit API
	case models.ModLoaderMagma, models.ModLoaderMagmaMaintained, models.ModLoaderMohist, models.ModLoaderArclight:
		return "tps"
	// Vanilla and others don't have TPS command
	case models.ModLoaderVanilla:
		return ""
	default:
		return "forge tps ?? neoforge tps ?? tps"
	}
}

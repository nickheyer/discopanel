package minecraft

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	models "github.com/nickheyer/discopanel/internal/db"
)

var (
	// RegEx for Average time per tick (f. e. "Average time per tick: 0.2ms")
	reVanillaTarget = regexp.MustCompile(`Target tick rate:\s*(\d+(?:\.\d+)?)`)

	// RegEx for Average time per tick (f. e. "Average time per tick: 0.2ms")
	reVanillaAvg     = regexp.MustCompile(`Average time per tick:\s*(\d+(?:\.\d+)?)ms`)
	reForgeOverall   = regexp.MustCompile(`Overall:.*Mean TPS:\s*(\d+(?:\.\d+)?)`)
	reForgeOverworld = regexp.MustCompile(`Dim minecraft:overworld.*Mean TPS:\s*(\d+(?:\.\d+)?)`)
	reSpigot         = regexp.MustCompile(`TPS from last 1m, 5m, 15m:\s*[*~>]?\s*(\d+(?:\.\d+)?)`)
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

// example output:
// The game is running normallyTarget tick rate: 20.0 per second.
// Average time per tick: 0.2ms (Target: 50.0ms)Percentiles: P50: 0.2ms P95: 0.4ms P99: 2.3ms. Sample: 100
func ParseTPSVanilla(output string) float64 {
	output = stripMinecraftColors(output)

	matchTarget := reVanillaTarget.FindStringSubmatch(output)
	matchAvg := reVanillaAvg.FindStringSubmatch(output)

	// value not found
	if len(matchTarget) < 2 || len(matchAvg) < 2 {
		return 0.0
	}

	targetTPS, err1 := strconv.ParseFloat(matchTarget[1], 64)
	avgTickTimeMS, err2 := strconv.ParseFloat(matchAvg[1], 64)

	if err1 != nil || err2 != nil || avgTickTimeMS <= 0 {
		return 0.0
	}

	// mathematically possible TPS
	calculatedTPS := 1000.0 / avgTickTimeMS

	// TPS can't be bigger than targetTPS
	currentTPS := math.Min(targetTPS, calculatedTPS)

	return currentTPS
}

// example output:
// Dim minecraft:overworld (minecraft:overworld): Mean tick time: 0.255 ms. Mean TPS: 20.000
// Dim minecraft:the_nether (minecraft:the_nether): Mean tick time: 0.065 ms. Mean TPS: 20.000
// Dim minecraft:the_end (minecraft:the_end): Mean tick time: 0.051 ms. Mean TPS: 20.000
// Overall: Mean tick time: 0.477 ms. Mean TPS: 20.000
func ParseTPSForge(output string) float64 {

	var tps float64
	// Priority Overall
	tps = ParseTPSRegex(output, reForgeOverall)

	if tps > 0.0 {
		return tps
	}
	// fallback overworld
	return ParseTPSRegex(output, reForgeOverworld)
}

// example output:
// TPS from last 1m, 5m, 15m: *20.0, *20.0, *20.0
// Current Memory Usage: 238/922 mb (Max: 1536 mb)
func ParseTPSSpigot(output string) float64 {
	return ParseTPSRegex(output, reSpigot)
}

func ParseTPSCustom(output string, regex string) float64 {
	output = stripMinecraftColors(output)
	return ParseTPSRegex(output, regexp.MustCompile(regex))
}

func ParseTPSRegex(output string, regex *regexp.Regexp) float64 {
	output = stripMinecraftColors(output)
	match := regex.FindStringSubmatch(output)
	if len(match) >= 2 {
		if tps, err := strconv.ParseFloat(match[1], 64); err == nil {
			return tps
		}
	}

	return 0.0
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
	case models.ModLoaderPaper, models.ModLoaderFolia, models.ModLoaderPurpur, models.ModLoaderSpigot, models.ModLoaderBukkit, models.ModLoaderPufferfish:
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

func GetTPSInfo(modLoader models.ModLoader, version string) (string, models.TPSExtractionMode) {
	// Determine TPS command based on modloader
	switch modLoader {
	case models.ModLoaderPaper, models.ModLoaderFolia, models.ModLoaderPurpur, models.ModLoaderSpigot, models.ModLoaderBukkit, models.ModLoaderPufferfish:
		return "tps", models.TPSExtractionModeSpigot
	case models.ModLoaderForge:
		return "forge tps", models.TPSExtractionModeForge
	case models.ModLoaderNeoForge:
		return "neoforge tps", models.TPSExtractionModeForge
	// Fabric-based need Carpet mod for TPS
	// Hybrids might support TPS if they have Bukkit API
	case models.ModLoaderMagma, models.ModLoaderMagmaMaintained, models.ModLoaderMohist, models.ModLoaderArclight:
		return "tps", models.TPSExtractionModeSpigot
	// Vanilla and others don't have TPS command
	case models.ModLoaderVanilla:
		if CompareMinecraftVersion(version, "1.20.3") >= 1 {
			return "tick query", models.TPSExtractionModeVanilla
		}
		return "", models.TPSExtractionModeLegacy

	default:
		return "", models.TPSExtractionModeLegacy
	}
}

// Priority-weighting Higher = Newer
var tagPriority = map[string]int{
	"snapshot": 1,
	"pre":      2,
	"rc":       3,
	"":         4, // regular release
}

type ParsedVersion struct {
	Base  []int  // f.e. [26, 2]
	Tag   string // f.e. "rc", "pre", "snapshot", or ""
	Build int    // f.e. 2 at "rc-2"
}

func parseVersion(v string) ParsedVersion {
	// delete "(Latest)"
	v = strings.TrimSpace(strings.Split(v, "(")[0])

	pv := ParsedVersion{}

	// check for tag (f.e. -rc-2)
	parts := strings.SplitN(v, "-", 2)

	// parse base version ("26.2" -> [26, 2])
	baseStrParts := strings.Split(parts[0], ".")
	for _, p := range baseStrParts {
		num, _ := strconv.Atoi(p)
		pv.Base = append(pv.Base, num)
	}

	// if tag exists ("rc-2" or "snapshot-1")
	if len(parts) > 1 {
		tagParts := strings.Split(parts[1], "-")
		pv.Tag = tagParts[0] // f.e. "rc"

		if len(tagParts) > 1 {
			pv.Build, _ = strconv.Atoi(tagParts[1]) // f.e. 2
		}
	}

	return pv
}

// CompareMinecraftVersion compares two minecraft versions.
// Returns:
//
//	-1 : v1 < v2
//	 0 : v1 == v2
//	 1 : v1 > v2
func CompareMinecraftVersion(v1, v2 string) int {
	pv1 := parseVersion(v1)
	pv2 := parseVersion(v2)

	// 1. Compare Base Version (f.e. 26.3 vs 26.2)
	maxLen := len(pv1.Base)
	if len(pv2.Base) > maxLen {
		maxLen = len(pv2.Base)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(pv1.Base) {
			n1 = pv1.Base[i]
		}
		if i < len(pv2.Base) {
			n2 = pv2.Base[i]
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	// 2. Pre-Release Tag-Hierarchy Comparison (Release > rc > pre > snapshot)
	prio1 := tagPriority[pv1.Tag]
	prio2 := tagPriority[pv2.Tag]

	if prio1 < prio2 {
		return -1
	}
	if prio1 > prio2 {
		return 1
	}

	// 3. if tag is identical (f.e. rc-1 vs rc-2), compare Build-Number
	if pv1.Build < pv2.Build {
		return -1
	}
	if pv1.Build > pv2.Build {
		return 1
	}

	return 0
}

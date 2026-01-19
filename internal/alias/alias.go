package alias

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/nickheyer/discopanel/internal/config"
	models "github.com/nickheyer/discopanel/internal/db"
)

// Category groups aliases by their source type
type Category string

const (
	CategoryServer  Category = "server"
	CategoryModule  Category = "module"
	CategorySpecial Category = "special"
)

// Info contains metadata about an available alias
type Info struct {
	Alias        string // e.g., "{{server.id}}"
	Path         string // e.g., "server.id"
	Description  string // From struct tag or generated
	Category     Category
	ExampleValue string // Resolved value when context available
	FieldType    string // Go type name
}

// Host contains host system information for alias resolution
type Host struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// Context holds the objects available for alias resolution
type Context struct {
	Server       *models.Server
	ServerConfig *models.ServerConfig
	Module       *models.Module
	Modules      map[string]*models.Module // Sibling modules by name (for inter-module references)
	Host         *Host
	Config       *config.Config
}

// NewContext creates a context with host information populated
func NewContext() *Context {
	return &Context{
		Host: &Host{
			UID: os.Getuid(),
			GID: os.Getgid(),
		},
	}
}

// GetAvailableAliases returns all available aliases with their metadata
func GetAvailableAliases(ctx *Context) []Info {
	if ctx == nil {
		ctx = NewContext()
	}

	// Always generate from zero values first to get all static aliases
	staticSources := []struct {
		prefix   string
		category Category
		zeroVal  reflect.Value
	}{
		{"host", CategorySpecial, reflect.ValueOf(Host{})},
		{"config", CategorySpecial, reflect.ValueOf(config.Config{})},
		{"server", CategoryServer, reflect.ValueOf(models.Server{})},
		{"server.config", CategoryServer, reflect.ValueOf(models.ServerConfig{})},
		{"module", CategoryModule, reflect.ValueOf(models.Module{})},
	}

	// Context sources for populating values and dynamic aliases (slices)
	contextSources := []struct {
		prefix   string
		category Category
		value    any
	}{
		{"host", CategorySpecial, ctx.Host},
		{"config", CategorySpecial, ctx.Config},
		{"server", CategoryServer, ctx.Server},
		{"server.config", CategoryServer, ctx.ServerConfig},
		{"module", CategoryModule, ctx.Module},
	}

	aliasMap := make(map[string]*Info)

	// Generate all static aliases with empty values
	for _, src := range staticSources {
		for _, a := range generateAliasesFromValue(src.zeroVal, src.prefix, src.category) {
			info := a
			aliasMap[a.Alias] = &info
		}
	}

	// Populate values from context and add dynamic aliases (from slices)
	for _, src := range contextSources {
		if v := reflect.ValueOf(src.value); v.IsValid() && !v.IsNil() {
			for _, a := range generateAliasesFromValue(v.Elem(), src.prefix, src.category) {
				if existing, ok := aliasMap[a.Alias]; ok {
					existing.ExampleValue = a.ExampleValue
				} else {
					info := a
					aliasMap[a.Alias] = &info
				}
			}
		}
	}

	aliases := make([]Info, 0, len(aliasMap))
	for _, info := range aliasMap {
		aliases = append(aliases, *info)
	}

	// Sort by category then alias name for stable ordering
	sort.Slice(aliases, func(i, j int) bool {
		if aliases[i].Category != aliases[j].Category {
			return aliases[i].Category < aliases[j].Category
		}
		return aliases[i].Alias < aliases[j].Alias
	})

	return aliases
}

// GetResolvedAliases returns all aliases with their resolved values
func GetResolvedAliases(ctx *Context) map[string]string {
	resolved := make(map[string]string)
	for _, info := range GetAvailableAliases(ctx) {
		resolved[info.Alias] = info.ExampleValue
	}
	return resolved
}

// generateAliasesFromValue walks a value tree and generates aliases for all leaf fields
func generateAliasesFromValue(val reflect.Value, prefix string, category Category) []Info {
	var aliases []Info

	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return aliases
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct:
		t := val.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			if strings.Contains(field.Tag.Get("gorm"), "-") {
				continue
			}
			jsonTag := field.Tag.Get("json")
			if jsonTag == "" || jsonTag == "-" {
				continue
			}
			jsonName := strings.Split(jsonTag, ",")[0]
			if jsonName == "" {
				continue
			}
			fieldVal := val.Field(i)
			aliases = append(aliases, generateAliasesFromValue(fieldVal, prefix+"."+jsonName, category)...)
		}
	case reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			for elem.Kind() == reflect.Pointer {
				if elem.IsNil() {
					break
				}
				elem = elem.Elem()
			}
			if elem.Kind() == reflect.Struct {
				if name := getFieldValueByJSONName(elem, "name"); name != "" {
					aliases = append(aliases, generateAliasesFromValue(elem, prefix+"."+name, category)...)
				}
			}
		}
	default:
		aliases = append(aliases, Info{
			Alias:        "{{" + prefix + "}}",
			Path:         prefix,
			Description:  generateDescription(prefix, ""),
			Category:     category,
			ExampleValue: formatValue(val),
			FieldType:    val.Type().String(),
		})
	}

	return aliases
}

// getFieldValueByJSONName finds a field by its json tag name and returns its string value
func getFieldValueByJSONName(val reflect.Value, jsonName string) string {
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return ""
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return ""
	}
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			continue
		}
		if strings.Split(jsonTag, ",")[0] == jsonName {
			return formatValue(val.Field(i))
		}
	}
	return ""
}

// formatValue converts a reflect.Value to a string representation
func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", v.Float())
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	case reflect.Pointer:
		if v.IsNil() {
			return ""
		}
		return formatValue(v.Elem())
	default:
		return ""
	}
}

// generateDescription creates a human-readable description from a field name
func generateDescription(fieldName, prefix string) string {
	// Convert CamelCase to words, handling acronyms (consecutive uppercase)
	var words []string
	var current strings.Builder

	runes := []rune(fieldName)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		isLastChar := i == len(runes)-1
		nextIsLower := !isLastChar && runes[i+1] >= 'a' && runes[i+1] <= 'z'

		if isUpper {
			// Start new word if:
			// - current word has lowercase chars (transitioning from lowercase to uppercase)
			// - OR this uppercase is followed by lowercase (end of acronym like "IDName" -> "ID", "Name")
			if current.Len() > 0 {
				lastRune := []rune(current.String())[current.Len()-1]
				lastWasLower := lastRune >= 'a' && lastRune <= 'z'
				if lastWasLower || nextIsLower {
					words = append(words, current.String())
					current.Reset()
				}
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	// Join and format
	desc := strings.Join(words, " ")
	desc = strings.ToLower(desc)

	// Capitalize first letter
	if len(desc) > 0 {
		desc = strings.ToUpper(string(desc[0])) + desc[1:]
	}

	return fmt.Sprintf("The %s's %s", prefix, desc)
}

// Substitute replaces all alias placeholders in a string with reflected/resolved values
func Substitute(input string, ctx *Context) string {
	if !strings.Contains(input, "{{") {
		return input
	}

	if ctx == nil {
		ctx = NewContext()
	}

	type subSource struct {
		prefix string
		value  any
	}

	// Order matters - longer prefixes first (server.config before server)
	sources := []subSource{
		{"host", ctx.Host},
		{"config", ctx.Config},
		{"server.config", ctx.ServerConfig},
		{"server", ctx.Server},
		{"module", ctx.Module},
	}

	result := input
	for _, src := range sources {
		if v := reflect.ValueOf(src.value); v.IsValid() && !v.IsNil() {
			result = substituteNestedPaths(result, v.Elem(), src.prefix)
		}
	}

	if len(ctx.Modules) > 0 {
		result = substituteModuleReferences(result, ctx.Modules)
	}

	return result
}

// substituteNestedPaths finds all {{prefix.*}} patterns and resolves them by walking the struct
func substituteNestedPaths(input string, val reflect.Value, prefix string) string {
	result := input
	pattern := "{{" + prefix + "."
	prefixSegments := len(strings.Split(prefix, "."))

	for strings.Contains(result, pattern) {
		start := strings.Index(result, pattern)
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		end += start + 2

		alias := result[start:end]
		path := alias[2 : len(alias)-2] // strip {{ and }}
		pathParts := strings.Split(path, ".")

		// Skip the prefix segments (e.g., "server.config" = 2 segments)
		if len(pathParts) <= prefixSegments {
			break
		}
		relativePath := pathParts[prefixSegments:] // path relative to the struct

		resolved := resolvePath(val, relativePath)
		result = strings.Replace(result, alias, resolved, 1)
	}

	return result
}

// resolvePath walks through a struct following the path segments
func resolvePath(val reflect.Value, path []string) string {
	if len(path) == 0 {
		return formatValue(val)
	}

	// Dereference pointers
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return ""
		}
		val = val.Elem()
	}

	segment := path[0]
	rest := path[1:]

	switch val.Kind() {
	case reflect.Struct:
		// Find field by json tag
		fieldVal := getFieldByJSONTag(val, segment)
		if !fieldVal.IsValid() {
			return ""
		}
		return resolvePath(fieldVal, rest)

	case reflect.Slice:
		// Find element by "name" field matching segment
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			// Dereference pointers
			for elem.Kind() == reflect.Pointer {
				if elem.IsNil() {
					break
				}
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Struct {
				continue
			}
			name := getFieldValueByJSONName(elem, "name")
			if name == segment {
				return resolvePath(elem, rest)
			}
		}
		return ""

	default:
		return formatValue(val)
	}
}

// getFieldByJSONTag finds a struct field by its json tag name and returns its value
func getFieldByJSONTag(val reflect.Value, jsonName string) reflect.Value {
	if val.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" {
			continue
		}
		name := strings.Split(jsonTag, ",")[0]
		if name == jsonName {
			return val.Field(i)
		}
	}
	return reflect.Value{}
}

// substituteModuleReferences handles {{modules.<name>.<field>}} patterns
func substituteModuleReferences(input string, modules map[string]*models.Module) string {
	result := input

	// Find all {{modules.*}} patterns
	for strings.Contains(result, "{{modules.") {
		start := strings.Index(result, "{{modules.")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		end += start + 2

		// Extract the full alias: {{modules.mysql.host}}
		alias := result[start:end]
		// Extract the path: modules.mysql.host
		path := alias[2 : len(alias)-2]
		parts := strings.SplitN(path, ".", 3)

		if len(parts) == 3 {
			moduleName := parts[1]
			fieldName := parts[2]

			if module, ok := modules[moduleName]; ok && module != nil {
				// Resolve the field value
				value := getModuleFieldValue(module, fieldName)
				result = strings.Replace(result, alias, value, 1)
				continue
			}
		}

		// If we couldn't resolve, move past this alias to avoid infinite loop
		result = result[:start] + result[end:]
	}

	return result
}

// Get a specific field value from a module for inter-module references
func getModuleFieldValue(module *models.Module, field string) string {
	// First try to resolve via reflection
	moduleVal := reflect.ValueOf(*module)
	value := getFieldValueByJSONName(moduleVal, field)
	if value != "" {
		return value
	}

	// Handle computed aliases that don't map directly to struct fields
	switch field {
	case "host":
		// Docker container name for internal networking
		return fmt.Sprintf("discopanel-module-%s", module.ID)
	case "port":
		// Return the first port's container port
		if len(module.Ports) > 0 && module.Ports[0] != nil && module.Ports[0].ContainerPort > 0 {
			return strconv.Itoa(int(module.Ports[0].ContainerPort))
		}
		return "8081"
	}

	return ""
}

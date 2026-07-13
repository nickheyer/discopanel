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

// Groups aliases by source type
type Category string

const (
	CategoryServer  Category = "server"
	CategoryModule  Category = "module"
	CategorySpecial Category = "special"
)

// Metadata about an available alias
type Info struct {
	Alias        string // Example {{server.id}}
	Path         string // Example server.id
	Description  string // From struct tag or generated
	Category     Category
	ExampleValue string // Resolved value when context available
	FieldType    string // Go type name
}

// Host system info for alias resolution
type Host struct {
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
	Hostname string `json:"hostname"`
}

// Objects available for alias resolution
type Context struct {
	Server           *models.Server
	ServerProperties *models.ServerProperties
	Module           *models.Module
	Modules          map[string]*models.Module // Sibling modules by name (for inter-module references)
	Host             *Host
	Config           *config.Config
}

// Creates context with host info populated
func NewContext() *Context {
	return &Context{
		Host: &Host{
			UID: os.Getuid(),
			GID: os.Getgid(),
		},
	}
}

// Derived from host fields
func (ctx *Context) populateComputed() {
	if ctx.Host == nil {
		ctx.Host = &Host{UID: os.Getuid(), GID: os.Getgid()}
	}
	if ctx.Host.Hostname == "" {
		if ctx.Server != nil && ctx.Server.ProxyHostname != "" {
			ctx.Host.Hostname = ctx.Server.ProxyHostname
		} else if ctx.Config != nil && ctx.Config.Server.Host != "" {
			ctx.Host.Hostname = ctx.Config.Server.Host
			if ctx.Host.Hostname == "0.0.0.0" {
				ctx.Host.Hostname = "localhost"
			}
		}
	}
}

// Returns all available aliases with metadata
func GetAvailableAliases(ctx *Context) []Info {
	if ctx == nil {
		ctx = NewContext()
	}
	ctx.populateComputed()

	// Zero values first to capture all static aliases
	staticSources := []struct {
		prefix   string
		category Category
		zeroVal  reflect.Value
	}{
		{"host", CategorySpecial, reflect.ValueOf(Host{})},
		{"config", CategorySpecial, reflect.ValueOf(config.Config{})},
		{"server", CategoryServer, reflect.ValueOf(models.Server{})},
		{"server.config", CategoryServer, reflect.ValueOf(models.ServerProperties{})},
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
		{"server.config", CategoryServer, ctx.ServerProperties},
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

// Returns all aliases with resolved values
func GetResolvedAliases(ctx *Context) map[string]string {
	resolved := make(map[string]string)
	for _, info := range GetAvailableAliases(ctx) {
		resolved[info.Alias] = info.ExampleValue
	}
	return resolved
}

// Walks value tree and generates aliases for leaf fields
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

// Finds field by json tag, returns string value
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

// Converts reflect value to string representation
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

// Creates human-readable description from field name
func generateDescription(fieldName, prefix string) string {
	// Converts CamelCase to words, handles acronyms
	var words []string
	var current strings.Builder

	runes := []rune(fieldName)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		isLastChar := i == len(runes)-1
		nextIsLower := !isLastChar && runes[i+1] >= 'a' && runes[i+1] <= 'z'

		if isUpper {
			// Starts new word on case transition or acronym end
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

// Replaces alias placeholders with reflected or resolved values
func Substitute(input string, ctx *Context) string {
	if !strings.Contains(input, "{{") {
		return input
	}

	if ctx == nil {
		ctx = NewContext()
	}
	ctx.populateComputed()

	type subSource struct {
		prefix string
		value  any
	}

	// Order matters - longer prefixes first (server.config before server)
	sources := []subSource{
		{"host", ctx.Host},
		{"config", ctx.Config},
		{"server.config", ctx.ServerProperties},
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

// Finds and resolves {{prefix.*}} patterns by walking struct
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
		path := alias[2 : len(alias)-2] // Strips {{ and }}
		pathParts := strings.Split(path, ".")

		// Skips prefix segments, e.g. server.config is 2 segments
		if len(pathParts) <= prefixSegments {
			break
		}
		relativePath := pathParts[prefixSegments:] // Path relative to struct

		resolved := resolvePath(val, relativePath)
		result = strings.Replace(result, alias, resolved, 1)
	}

	return result
}

// Walks struct following path segments
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

// Finds struct field by json tag, returns value
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

// Handles {{modules.<name>.<field>}} patterns
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

		// Extracts full alias, e.g. {{modules.mysql.host}}
		alias := result[start:end]
		// Extracts path, e.g. modules.mysql.host
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

		// Skips unresolved alias to avoid infinite loop
		result = result[:start] + result[end:]
	}

	return result
}

// Gets one field value from a module
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

// Rewrites generated pb.go struct tags from proto annotations
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// One field's computed struct tags
type fieldTags struct {
	gorm     string
	extra    string
	jsonName string
	redact   bool
}

// Walks the package and injects tags into model structs
func injectAll(pbdir string, models []*modelSpec) int {
	tagged := map[string]map[string]fieldTags{}
	for _, m := range models {
		tagged[m.name] = computeTags(m)
	}
	injected := 0
	err := filepath.Walk(pbdir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".pb.go") {
			return err
		}
		n, err := injectFile(path, tagged)
		injected += n
		return err
	})
	if err != nil {
		fatal("inject: %v", err)
	}
	return injected
}

// Builds the tag set for one model
func computeTags(m *modelSpec) map[string]fieldTags {
	tags := map[string]fieldTags{}
	for i, fs := range m.fields {
		fd := m.msg.Fields().Get(i)
		var ft fieldTags
		ft.redact = fs.redact
		if fs.skip {
			ft.gorm = "-"
			tags[fs.goName] = ft
			continue
		}
		if fs.relation {
			frags := []string{}
			if !strings.Contains(fs.tag, "foreignKey:") {
				frags = append(frags, "foreignKey:"+fs.goName+"Id")
			}
			if fs.tag != "" {
				frags = append(frags, fs.tag)
			}
			ft.gorm = strings.Join(frags, ";")
			tags[fs.goName] = ft
			continue
		}
		frags := []string{"column:" + fs.column}
		frags = append(frags, autoSerializer(fd)...)
		for _, part := range strings.Split(fs.tag, ";") {
			if part == "" || strings.HasPrefix(part, "column:") {
				continue
			}
			frags = append(frags, part)
		}
		ft.gorm = strings.Join(frags, ";")
		ft.extra = propTags(fd)
		if ft.extra != "" {
			// Property fields keep their legacy camelCase json keys
			ft.jsonName = fd.JSONName()
		}
		tags[fs.goName] = ft
	}
	return tags
}

// Picks storage serializers from the field shape
func autoSerializer(fd protoreflect.FieldDescriptor) []string {
	if fd.IsMap() || fd.IsList() {
		return []string{"serializer:json"}
	}
	if fd.Kind() == protoreflect.MessageKind {
		if fd.Message().FullName() == "google.protobuf.Timestamp" {
			return []string{"type:datetime", "serializer:tspb"}
		}
		return []string{"serializer:json"}
	}
	return nil
}

// Renders property metadata as struct tag fragments
func propTags(fd protoreflect.FieldDescriptor) string {
	ext := propField(fd)
	if ext == nil {
		return ""
	}
	var parts []string
	add := func(key, val string) {
		if val != "" {
			parts = append(parts, fmt.Sprintf("%s:%q", key, val))
		}
	}
	add("env", ext.Env)
	add("prop", ext.Prop)
	add("default", ext.DefaultValue)
	add("desc", ext.Desc)
	add("input", ext.Input)
	add("label", ext.Label)
	add("category", ext.Category)
	if ext.System {
		parts = append(parts, `system:"true"`)
	}
	if ext.Required {
		parts = append(parts, `required:"true"`)
	}
	if ext.Ephemeral {
		parts = append(parts, `ephemeral:"true"`)
	}
	return strings.Join(parts, " ")
}

// Rewrites struct tags in one generated file
func injectFile(path string, tagged map[string]map[string]fieldTags) (int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return 0, err
	}

	n := 0
	changed := false
	ast.Inspect(f, func(node ast.Node) bool {
		ts, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		ft, ok := tagged[ts.Name.Name]
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		n++
		for _, fld := range st.Fields.List {
			if len(fld.Names) != 1 || fld.Tag == nil {
				continue
			}
			spec, ok := ft[fld.Names[0].Name]
			if !ok {
				continue
			}
			raw := strings.Trim(fld.Tag.Value, "`")
			for _, key := range []string{"gorm", "env", "prop", "default", "desc", "input", "label", "category", "system", "required", "ephemeral"} {
				raw = stripTagKey(raw, key)
			}
			if spec.redact {
				raw = stripTagKey(raw, "json") + ` json:"-"`
			} else if spec.jsonName != "" {
				raw = stripTagKey(raw, "json") + ` json:"` + spec.jsonName + `"`
			}
			tag := strings.TrimSpace(raw) + ` gorm:"` + spec.gorm + `"`
			if spec.extra != "" {
				tag += " " + spec.extra
			}
			fld.Tag.Value = "`" + tag + "`"
			changed = true
		}
		return true
	})
	if !changed {
		return n, nil
	}

	var buf bytes.Buffer
	if err := (&printer.Config{Mode: printer.TabIndent, Tabwidth: 8}).Fprint(&buf, fset, f); err != nil {
		return n, err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return n, err
	}
	return n, os.WriteFile(path, formatted, 0644)
}

// Removes one key from a struct tag string
func stripTagKey(raw, key string) string {
	var kept []string
	for _, part := range splitTag(raw) {
		if !strings.HasPrefix(part, key+`:"`) {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// Splits a struct tag into key value units
func splitTag(raw string) []string {
	var parts []string
	for raw = strings.TrimSpace(raw); raw != ""; raw = strings.TrimSpace(raw) {
		colon := strings.Index(raw, `:"`)
		if colon < 0 {
			parts = append(parts, raw)
			break
		}
		end := strings.Index(raw[colon+2:], `"`)
		if end < 0 {
			parts = append(parts, raw)
			break
		}
		parts = append(parts, raw[:colon+2+end+1])
		raw = raw[colon+2+end+1:]
	}
	return parts
}

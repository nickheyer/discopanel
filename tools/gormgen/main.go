// Reads the buf image and generates every db artifact.
// The proto message is the model, annotations drive gorm
// tags, enum strings, and store crud methods.
package main

import (
	"flag"
	"os"
	"sort"
	"strings"

	"fmt"
	optionsv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/options/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// One persisted column or relation
type fieldSpec struct {
	goName     string
	column     string
	kind       protoreflect.Kind
	optional   bool
	skip       bool
	redact     bool
	relation   bool
	autoCreate bool
	autoUpdate bool
	pk         bool
	autoInc    bool
	tag        string
}

// One table backed message
type modelSpec struct {
	name    string
	msg     protoreflect.MessageDescriptor
	opts    *optionsv1.DbModel
	fields  []*fieldSpec
	pks     []*fieldSpec
	created string
	updated string
	redacts []string
}

// One generated enum
type enumSpec struct {
	name   string
	desc   protoreflect.EnumDescriptor
	opts   *optionsv1.EnumType
	file   string
	values []*enumValueSpec
}

// One enum value with resolved strings
type enumValueSpec struct {
	goConst string
	tsName  string
	number  int32
	name    string
	label   string
	help    string
}

func main() {
	image := flag.String("image", ".buf-image.binpb", "buf descriptor image")
	pbdir := flag.String("pbdir", "pkg/proto/discopanel/v1", "generated go package to rewrite")
	support := flag.String("support", "pkg/proto/discopanel/v1/gorm.gen.go", "model support output")
	enums := flag.String("enums", "pkg/proto/discopanel/v1/enums.gen.go", "go enum output")
	tsenums := flag.String("tsenums", "web/discopanel/src/lib/proto/enums.gen.ts", "typescript enum output")
	store := flag.String("store", "internal/db/store.gen.go", "store crud output")
	flag.Parse()

	data, err := os.ReadFile(*image)
	if err != nil {
		fatal("read image: %v", err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := (proto.UnmarshalOptions{Resolver: protoregistry.GlobalTypes}).Unmarshal(data, &fds); err != nil {
		fatal("unmarshal image: %v", err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		fatal("build files: %v", err)
	}

	models := collectModels(files)
	enumSpecs := collectEnums(files)

	injected := injectAll(*pbdir, models)
	writeFile(*support, renderSupport(models))
	writeFile(*enums, renderGoEnums(enumSpecs))
	writeFile(*tsenums, renderTsEnums(enumSpecs))
	writeFile(*store, renderStore(models))
	fmt.Printf("gormgen: %d models, %d enums, %d structs tagged\n", len(models), len(enumSpecs), injected)
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "gormgen: "+f+"\n", a...)
	os.Exit(1)
}

func writeFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

// Builds model specs for every db_model message
func collectModels(files *protoregistry.Files) []*modelSpec {
	byName := map[protoreflect.FullName]bool{}
	var raw []protoreflect.MessageDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Messages().Len(); i++ {
			md := fd.Messages().Get(i)
			if dbModel(md) != nil {
				byName[md.FullName()] = true
				raw = append(raw, md)
			}
		}
		return true
	})

	var models []*modelSpec
	for _, md := range raw {
		m := &modelSpec{name: goCamel(string(md.Name())), msg: md, opts: dbModel(md)}
		for i := 0; i < md.Fields().Len(); i++ {
			fd := md.Fields().Get(i)
			fs := buildField(fd, byName)
			m.fields = append(m.fields, fs)
			if fs.pk {
				m.pks = append(m.pks, fs)
			}
			if fs.autoCreate {
				m.created = fs.goName
			}
			if fs.autoUpdate {
				m.updated = fs.goName
			}
			if fs.redact {
				m.redacts = append(m.redacts, fs.goName)
			}
		}
		if len(m.pks) == 0 {
			fatal("model %s has no primary key", m.name)
		}
		models = append(models, m)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].opts.Table < models[j].opts.Table })
	return models
}

// Resolves one field's storage shape
func buildField(fd protoreflect.FieldDescriptor, models map[protoreflect.FullName]bool) *fieldSpec {
	fs := &fieldSpec{
		goName:   goCamel(string(fd.Name())),
		column:   string(fd.Name()),
		kind:     fd.Kind(),
		optional: fd.HasOptionalKeyword(),
	}
	ext := dbField(fd)
	if ext != nil {
		fs.skip = ext.Skip
		fs.redact = ext.Redact
		fs.tag = ext.Tag
	}
	if fd.Kind() == protoreflect.MessageKind && !fd.IsMap() && !fd.IsList() && models[fd.Message().FullName()] {
		fs.relation = true
		return fs
	}
	if ext != nil {
		for _, part := range strings.Split(ext.Tag, ";") {
			switch {
			case strings.HasPrefix(part, "column:"):
				fs.column = strings.TrimPrefix(part, "column:")
			case part == "autoCreateTime":
				fs.autoCreate = true
			case part == "autoUpdateTime":
				fs.autoUpdate = true
			case part == "primaryKey" || strings.HasPrefix(part, "primaryKey;"):
				fs.pk = true
			case part == "autoIncrement":
				fs.autoInc = true
			}
		}
		if strings.Contains(ext.Tag, "primaryKey") {
			fs.pk = true
		}
		if strings.Contains(ext.Tag, "autoIncrement") {
			fs.autoInc = true
		}
	}
	return fs
}

// Builds enum specs for every package enum
func collectEnums(files *protoregistry.Files) []*enumSpec {
	var enums []*enumSpec
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "discopanel.v1" {
			return true
		}
		for i := 0; i < fd.Enums().Len(); i++ {
			ed := fd.Enums().Get(i)
			es := &enumSpec{
				name: string(ed.Name()),
				desc: ed,
				opts: enumType(ed),
				file: strings.TrimSuffix(fd.Path(), ".proto"),
			}
			prefix := screamingPrefix(string(ed.Name()))
			for j := 0; j < ed.Values().Len(); j++ {
				vd := ed.Values().Get(j)
				ev := &enumValueSpec{
					goConst: string(ed.Name()) + "_" + string(vd.Name()),
					tsName:  strings.TrimPrefix(string(vd.Name()), prefix),
					number:  int32(vd.Number()),
				}
				ev.name = strings.ToLower(ev.tsName)
				ev.label = ev.name
				if ext := enumValue(vd); ext != nil {
					if ext.Name != "" {
						ev.name = ext.Name
						ev.label = ext.Name
					}
					if ext.Label != "" {
						ev.label = ext.Label
					}
					ev.help = ext.Desc
				}
				es.values = append(es.values, ev)
			}
			enums = append(enums, es)
		}
		return true
	})
	sort.Slice(enums, func(i, j int) bool { return enums[i].name < enums[j].name })
	return enums
}

func dbModel(md protoreflect.MessageDescriptor) *optionsv1.DbModel {
	opts := md.Options().(*descriptorpb.MessageOptions)
	return proto.GetExtension(opts, optionsv1.E_DbModel).(*optionsv1.DbModel)
}

func dbField(fd protoreflect.FieldDescriptor) *optionsv1.DbField {
	opts := fd.Options().(*descriptorpb.FieldOptions)
	return proto.GetExtension(opts, optionsv1.E_Db).(*optionsv1.DbField)
}

func propField(fd protoreflect.FieldDescriptor) *optionsv1.PropField {
	opts := fd.Options().(*descriptorpb.FieldOptions)
	return proto.GetExtension(opts, optionsv1.E_Prop).(*optionsv1.PropField)
}

func enumType(ed protoreflect.EnumDescriptor) *optionsv1.EnumType {
	opts := ed.Options().(*descriptorpb.EnumOptions)
	return proto.GetExtension(opts, optionsv1.E_EnumType).(*optionsv1.EnumType)
}

func enumValue(vd protoreflect.EnumValueDescriptor) *optionsv1.EnumValue {
	opts := vd.Options().(*descriptorpb.EnumValueOptions)
	return proto.GetExtension(opts, optionsv1.E_Value).(*optionsv1.EnumValue)
}

func goCamel(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up {
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Enum value prefix like MOD_LOADER_ from ModLoader
func screamingPrefix(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String()) + "_"
}

// Lower spaced words from a camel name
func humanName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' && i > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// Trailing s added unless already present
func plural(name string) string {
	if strings.HasSuffix(name, "s") {
		return name
	}
	return name + "s"
}

// Lower camel go identifier from a column name
func paramName(col string) string {
	c := goCamel(col)
	if strings.HasSuffix(c, "Id") {
		c = strings.TrimSuffix(c, "Id") + "ID"
	}
	if c == "" {
		return c
	}
	r := []rune(c)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	out := string(r)
	if out == "iD" {
		out = "id"
	}
	return out
}

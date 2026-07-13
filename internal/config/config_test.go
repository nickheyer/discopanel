package config

import (
	"reflect"
	"regexp"
	"testing"
)

// Secret shaped names must refuse alias resolution
var secretFieldPattern = regexp.MustCompile(`(?i)(secret|token|password|api_?key)`)

func TestSecretConfigFieldsCarrySecretTag(t *testing.T) {
	assertSecretTags(t, reflect.TypeOf(Config{}), "Config")
}

func assertSecretTags(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldPath := path + "." + field.Name
		ft := field.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			assertSecretTags(t, ft, fieldPath)
			continue
		}
		if secretFieldPattern.MatchString(field.Name) && field.Tag.Get("alias") != "secret" {
			t.Errorf("field %s matches secret pattern without alias secret tag", fieldPath)
		}
	}
}

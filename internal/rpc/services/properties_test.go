package services

import (
	"reflect"
	"testing"

	storage "github.com/nickheyer/discopanel/internal/db"
)

// Every exposed properties field must carry a known category tag
func TestEveryPropertyFieldHasCategory(t *testing.T) {
	cfgType := reflect.TypeOf(storage.ServerProperties{})
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" || jsonTag == "id" || jsonTag == "server_id" || jsonTag == "updated_at" {
			continue
		}
		if propertyCategoryIndex(field.Tag.Get("category")) < 0 {
			t.Errorf("field %s (json %q) has no category tag, hidden from API", field.Name, jsonTag)
		}
	}
}

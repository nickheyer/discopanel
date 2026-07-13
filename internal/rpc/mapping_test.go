package rpc

import (
	"fmt"
	"testing"

	"github.com/nickheyer/discopanel/internal/rbac"
	_ "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Collects every procedure of the discopanel.v1 proto package
func panelProcedures(t *testing.T) map[string]protoreflect.MethodDescriptor {
	t.Helper()
	methods := map[string]protoreflect.MethodDescriptor{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "discopanel.v1" {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			ms := svc.Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				methods[fmt.Sprintf("/%s/%s", svc.FullName(), m.Name())] = m
			}
		}
		return true
	})
	if len(methods) == 0 {
		t.Fatal("no discopanel.v1 services registered")
	}
	return methods
}

// The interceptor fails closed, every RPC needs exactly one mapping
func TestEveryProcedureHasOneMapping(t *testing.T) {
	for proc := range panelProcedures(t) {
		count := 0
		if rbac.PublicProcedures[proc] {
			count++
		}
		if rbac.AuthenticatedOnlyProcedures[proc] {
			count++
		}
		if _, ok := rbac.ProcedurePermissions[proc]; ok {
			count++
		}
		if count == 0 {
			t.Errorf("procedure %s has no authorization mapping", proc)
		}
		if count > 1 {
			t.Errorf("procedure %s appears in %d mapping tables", proc, count)
		}
	}
}

// Stale map keys hide fail-closed gaps behind dead entries
func TestMappingsNameRealProcedures(t *testing.T) {
	methods := panelProcedures(t)
	for proc := range rbac.PublicProcedures {
		if _, ok := methods[proc]; !ok {
			t.Errorf("public mapping names unknown procedure %s", proc)
		}
	}
	for proc := range rbac.AuthenticatedOnlyProcedures {
		if _, ok := methods[proc]; !ok {
			t.Errorf("authenticated mapping names unknown procedure %s", proc)
		}
	}
	for proc := range rbac.ProcedurePermissions {
		if _, ok := methods[proc]; !ok {
			t.Errorf("permission mapping names unknown procedure %s", proc)
		}
	}
}

// Misnamed object fields silently widen scope to all objects
func TestPermissionShapesMatchRequests(t *testing.T) {
	methods := panelProcedures(t)
	validScopes := map[rbac.ObjectScope]bool{
		rbac.ScopeDirect:        true,
		rbac.ScopeTask:          true,
		rbac.ScopeTaskExecution: true,
		rbac.ScopeModule:        true,
	}
	for proc, perm := range rbac.ProcedurePermissions {
		m, ok := methods[proc]
		if !ok {
			continue
		}
		if perm.Resource == "" || perm.Action == "" {
			t.Errorf("procedure %s misses resource or action", proc)
		}
		if !validScopes[perm.Scope] {
			t.Errorf("procedure %s carries unknown scope %q", proc, perm.Scope)
		}
		if perm.ObjectIDField == "" {
			if perm.Scope != rbac.ScopeDirect {
				t.Errorf("procedure %s scopes without an object field", proc)
			}
			continue
		}
		fd := m.Input().Fields().ByName(protoreflect.Name(perm.ObjectIDField))
		if fd == nil {
			t.Errorf("procedure %s object field %q missing on request", proc, perm.ObjectIDField)
			continue
		}
		if fd.Kind() != protoreflect.StringKind {
			t.Errorf("procedure %s object field %q is not a string", proc, perm.ObjectIDField)
		}
	}
}

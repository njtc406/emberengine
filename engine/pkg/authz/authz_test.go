package authz

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Principal ──────────────────────────────────────────

func TestPrincipalFromPID(t *testing.T) {
	pid := actor.NewPID("127.0.0.1:6670", "node-1", 1, "svc-001", "game", "GameService", 1, "grpc")
	p := PrincipalFromPID(pid)
	assert.Equal(t, "game", p.ServiceType)
	assert.Equal(t, "GameService", p.ServiceName)
	assert.Equal(t, "node-1", p.NodeUid)
	assert.Contains(t, p.String(), "game/GameService@node-1")
}

func TestPrincipalFromNilPID(t *testing.T) {
	p := PrincipalFromPID(nil)
	assert.Empty(t, p.ServiceType)
	assert.Empty(t, p.ServiceName)
	assert.Empty(t, p.NodeUid)
}

// ─── Authorizer 基本功能 ────────────────────────────────

func TestAuthorizer_DisabledByDefault(t *testing.T) {
	a := NewAuthorizer()
	assert.False(t, a.IsEnabled())
	// 未启用时全部放行
	err := a.Authorize(Principal{ServiceType: "unknown"}, "AnyService", "AnyMethod")
	assert.NoError(t, err)
}

func TestAuthorizer_EnableDisable(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	assert.True(t, a.IsEnabled())
	a.Enable(false)
	assert.False(t, a.IsEnabled())
}

// ─── 角色与权限 ─────────────────────────────────────────

func TestAuthorizer_WildcardPermission(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("admin", []string{"*"})
	a.BindRole("admin", "admin")

	caller := Principal{ServiceType: "admin", ServiceName: "AdminService"}
	assert.NoError(t, a.Authorize(caller, "AnyService", "AnyMethod"))
}

func TestAuthorizer_ServiceWildcard(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("data-reader", []string{"DataService.*"})
	a.BindRole("data-reader", "game")

	caller := Principal{ServiceType: "game", ServiceName: "GameService"}
	assert.NoError(t, a.Authorize(caller, "DataService", "APIGetUser"))
	assert.NoError(t, a.Authorize(caller, "DataService", "RpcSaveData"))
	assert.Error(t, a.Authorize(caller, "AdminService", "RpcShutdown"))
}

func TestAuthorizer_PrefixMatch(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("reader", []string{"DataService.APIGet*", "DataService.RpcQuery*"})
	a.BindRole("reader", "gate")

	caller := Principal{ServiceType: "gate", ServiceName: "GateService"}
	assert.NoError(t, a.Authorize(caller, "DataService", "APIGetUser"))
	assert.NoError(t, a.Authorize(caller, "DataService", "APIGetItem"))
	assert.NoError(t, a.Authorize(caller, "DataService", "RpcQueryList"))
	assert.Error(t, a.Authorize(caller, "DataService", "APISaveUser"))
	assert.Error(t, a.Authorize(caller, "DataService", "RpcDelete"))
}

func TestAuthorizer_ExactMatch(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("limited", []string{"DataService.APIGetUser"})
	a.BindRole("limited", "game")

	caller := Principal{ServiceType: "game", ServiceName: "GameService"}
	assert.NoError(t, a.Authorize(caller, "DataService", "APIGetUser"))
	assert.Error(t, a.Authorize(caller, "DataService", "APIGetItem"))
}

func TestAuthorizer_MultipleRoles(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("reader", []string{"DataService.APIGet*"})
	a.AddRole("writer", []string{"DataService.APISave*"})
	a.BindRole("reader", "game")
	a.BindRole("writer", "game")

	caller := Principal{ServiceType: "game", ServiceName: "GameService"}
	assert.NoError(t, a.Authorize(caller, "DataService", "APIGetUser"))
	assert.NoError(t, a.Authorize(caller, "DataService", "APISaveUser"))
	assert.Error(t, a.Authorize(caller, "DataService", "RpcDelete"))
}

func TestAuthorizer_NoRolesAssigned(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)

	caller := Principal{ServiceType: "unknown"}
	err := a.Authorize(caller, "DataService", "APIGetUser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no roles assigned")
}

func TestAuthorizer_RoleNotFound(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.BindRole("nonexistent", "game")

	caller := Principal{ServiceType: "game"}
	err := a.Authorize(caller, "DataService", "APIGetUser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied access")
}

// ─── RemoveRole / UnbindRole ────────────────────────────

func TestAuthorizer_RemoveRole(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("admin", []string{"*"})
	a.BindRole("admin", "game")

	caller := Principal{ServiceType: "game"}
	assert.NoError(t, a.Authorize(caller, "Any", "Any"))

	a.RemoveRole("admin")
	assert.Error(t, a.Authorize(caller, "Any", "Any"))
}

func TestAuthorizer_UnbindRole(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)
	a.AddRole("admin", []string{"*"})
	a.BindRole("admin", "game")

	caller := Principal{ServiceType: "game"}
	assert.NoError(t, a.Authorize(caller, "Any", "Any"))

	a.UnbindRole("admin", "game")
	err := a.Authorize(caller, "Any", "Any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no roles assigned")
}

// ─── matchPermission ────────────────────────────────────

func TestMatchPermission(t *testing.T) {
	tests := []struct {
		pattern  string
		resource string
		want     bool
	}{
		{"*", "DataService.APIGetUser", true},
		{"DataService.*", "DataService.APIGetUser", true},
		{"DataService.*", "AdminService.APIGetUser", false},
		{"DataService.APIGet*", "DataService.APIGetUser", true},
		{"DataService.APIGet*", "DataService.APISave", false},
		{"DataService.APIGetUser", "DataService.APIGetUser", true},
		{"DataService.APIGetUser", "DataService.APIGetItem", false},
		{"", "", true},
		{"", "DataService.APIGetUser", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"→"+tt.resource, func(t *testing.T) {
			assert.Equal(t, tt.want, matchPermission(tt.pattern, tt.resource))
		})
	}
}

// ─── BindRole 幂等性 ────────────────────────────────────

func TestBindRole_NoDuplicate(t *testing.T) {
	a := NewAuthorizer()
	a.BindRole("admin", "game")
	a.BindRole("admin", "game") // 重复绑定
	a.BindRole("admin", "game")

	a.mu.RLock()
	assert.Len(t, a.bindings["game"], 1)
	a.mu.RUnlock()
}

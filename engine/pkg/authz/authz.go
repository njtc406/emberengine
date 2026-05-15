// Package authz 提供服务间授权能力，包含 Principal 身份模型和 RBAC 授权引擎。
//
// Principal 标识调用方身份（从 PID 的 ServiceType/Name 提取）；
// Authorizer 基于角色-权限策略决定是否允许指定 method 调用。
//
// 使用方式：
//
//	authz := authz.NewAuthorizer()
//	authz.AddRole("game", []string{"DataService.*", "CacheService.Get*"})
//	authz.BindRole("game", "GameService")
//
//	principal := authz.PrincipalFromPID(senderPID)
//	if err := authz.Authorize(principal, "DataService", "APIGetUser"); err != nil {
//	    // 拒绝
//	}
package authz

import (
	"fmt"
	"strings"
	"sync"

	"github.com/njtc406/emberengine/engine/pkg/actor"
)

// Principal 表示 RPC 调用方的身份。
type Principal struct {
	ServiceType string // 服务类型（如 "game", "gate", "admin"）
	ServiceName string // 服务名称（如 "GameService"）
	NodeUid     string // 来源节点 UID
}

// String 返回 Principal 的字符串表示。
func (p Principal) String() string {
	return fmt.Sprintf("%s/%s@%s", p.ServiceType, p.ServiceName, p.NodeUid)
}

// PrincipalFromPID 从 PID 提取 Principal 身份信息。
func PrincipalFromPID(pid *actor.PID) Principal {
	if pid == nil {
		return Principal{}
	}
	return Principal{
		ServiceType: pid.GetServiceType(),
		ServiceName: pid.GetName(),
		NodeUid:     pid.GetNodeUid(),
	}
}

// Role 定义角色及其权限集合。
type Role struct {
	Name        string   // 角色名称
	Permissions []string // 权限模式列表，格式 "ServiceName.MethodPattern" 或 "*"
}

// Authorizer 是 RBAC 授权引擎。
// 线程安全：策略表在启动阶段写入后通常只读；运行期读取走 RWMutex。
type Authorizer struct {
	mu       sync.RWMutex
	roles    map[string]*Role    // roleName → Role
	bindings map[string][]string // serviceType → []roleName
	enabled  bool                // 是否启用授权检查（false 时全部放行）
}

// NewAuthorizer 创建新的授权引擎。默认不启用，需调用 Enable(true) 开启。
func NewAuthorizer() *Authorizer {
	return &Authorizer{
		roles:    make(map[string]*Role),
		bindings: make(map[string][]string),
	}
}

// Enable 开启或关闭授权检查。
func (a *Authorizer) Enable(on bool) {
	a.mu.Lock()
	a.enabled = on
	a.mu.Unlock()
}

// IsEnabled 返回授权是否启用。
func (a *Authorizer) IsEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

// AddRole 添加或覆盖角色定义。
// permissions 格式：
//   - "*" 表示全部权限
//   - "ServiceName.*" 表示该服务所有方法
//   - "ServiceName.MethodPrefix*" 表示前缀匹配
//   - "ServiceName.ExactMethod" 表示精确匹配
func (a *Authorizer) AddRole(name string, permissions []string) {
	a.mu.Lock()
	a.roles[name] = &Role{Name: name, Permissions: permissions}
	a.mu.Unlock()
}

// RemoveRole 移除角色定义。
func (a *Authorizer) RemoveRole(name string) {
	a.mu.Lock()
	delete(a.roles, name)
	a.mu.Unlock()
}

// BindRole 将角色绑定到 serviceType。
// 同一 serviceType 可绑定多个角色，权限取并集。
func (a *Authorizer) BindRole(roleName, serviceType string) {
	a.mu.Lock()
	a.bindings[serviceType] = appendUnique(a.bindings[serviceType], roleName)
	a.mu.Unlock()
}

// UnbindRole 解除 serviceType 上的角色绑定。
func (a *Authorizer) UnbindRole(roleName, serviceType string) {
	a.mu.Lock()
	if roles, ok := a.bindings[serviceType]; ok {
		a.bindings[serviceType] = removeStr(roles, roleName)
	}
	a.mu.Unlock()
}

// Authorize 检查 principal 是否有权调用 targetService 的 method。
// 返回 nil 表示允许，否则返回包含拒绝原因的 error。
func (a *Authorizer) Authorize(caller Principal, targetService, method string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.enabled {
		return nil
	}

	roleNames, ok := a.bindings[caller.ServiceType]
	if !ok || len(roleNames) == 0 {
		return fmt.Errorf("authz: service type %q has no roles assigned", caller.ServiceType)
	}

	resource := targetService + "." + method
	for _, rn := range roleNames {
		role, exists := a.roles[rn]
		if !exists {
			continue
		}
		for _, perm := range role.Permissions {
			if matchPermission(perm, resource) {
				return nil
			}
		}
	}

	return fmt.Errorf("authz: %s denied access to %s", caller, resource)
}

// matchPermission 检查权限模式是否匹配资源。
func matchPermission(pattern, resource string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(resource, prefix)
	}
	return pattern == resource
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

func removeStr(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

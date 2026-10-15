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
	revision int64               // 当前策略快照的 revision
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
	perms := make([]string, len(permissions))
	copy(perms, permissions)

	a.mu.Lock()
	a.roles[name] = &Role{Name: name, Permissions: perms}
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

// ApplySnapshot 原子应用策略快照到 Authorizer。
// 先校验快照合法性，校验通过后在写锁内整体替换 roles/bindings/revision。
// 不修改 enabled 状态（由外部配置控制）。
func (a *Authorizer) ApplySnapshot(snapshot *PolicySnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("authz: snapshot is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	snapshot.Normalize()

	// 构建新的 roles 和 bindings
	newRoles := make(map[string]*Role, len(snapshot.Roles))
	for name, pr := range snapshot.Roles {
		perms := make([]string, len(pr.Permissions))
		copy(perms, pr.Permissions)
		newRoles[name] = &Role{Name: name, Permissions: perms}
	}

	newBindings := make(map[string][]string)
	for _, binding := range snapshot.Bindings {
		for _, svcType := range binding.ServiceTypes {
			for _, roleName := range binding.Roles {
				newBindings[svcType] = appendUnique(newBindings[svcType], roleName)
			}
		}
	}

	a.mu.Lock()
	a.roles = newRoles
	a.bindings = newBindings
	a.revision = snapshot.Revision
	a.mu.Unlock()

	return nil
}

// Snapshot 返回当前 Authorizer 中策略的只读快照。
func (a *Authorizer) Snapshot() *PolicySnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	snap := &PolicySnapshot{
		Revision: a.revision,
		Roles:    make(map[string]*PolicyRole, len(a.roles)),
		Bindings: make(map[string]*PolicyBinding),
	}

	for name, role := range a.roles {
		perms := make([]string, len(role.Permissions))
		copy(perms, role.Permissions)
		snap.Roles[name] = &PolicyRole{Permissions: perms}
	}

	// 反转 bindings: serviceType→[]roleName → 按 roleName 分组
	// 简化实现：每个 serviceType 生成一个 binding
	for svcType, roleNames := range a.bindings {
		roles := make([]string, len(roleNames))
		copy(roles, roleNames)
		snap.Bindings[svcType] = &PolicyBinding{
			ServiceTypes: []string{svcType},
			Roles:        roles,
		}
	}

	return snap
}

// Revision 返回当前策略快照的 revision。
func (a *Authorizer) Revision() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.revision
}

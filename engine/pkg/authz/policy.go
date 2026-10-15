// Package authz - 策略快照模型定义。
//
// PolicySnapshot 是 RBAC 策略的可序列化表示，
// 支持从本地文件或 etcd 加载后原子应用到 Authorizer。

package authz

import (
	"fmt"
	"strings"
)

// PolicySnapshot 表示完整的 RBAC 策略快照。
type PolicySnapshot struct {
	Version  int                       `json:"version" yaml:"version"`
	Revision int64                     `json:"revision" yaml:"revision"`
	Roles    map[string]*PolicyRole    `json:"roles" yaml:"roles"`
	Bindings map[string]*PolicyBinding `json:"bindings" yaml:"bindings"`
}

// PolicyRole 定义角色及其权限列表。
type PolicyRole struct {
	Permissions []string `json:"permissions" yaml:"permissions"`
}

// PolicyBinding 定义服务类型到角色的绑定关系。
type PolicyBinding struct {
	ServiceTypes []string `json:"serviceTypes" yaml:"serviceTypes"`
	Roles        []string `json:"roles" yaml:"roles"`
}

// Validate 校验策略快照的合法性。
// 返回第一个发现的错误，或 nil 表示合法。
func (s *PolicySnapshot) Validate() error {
	if s == nil {
		return fmt.Errorf("authz: policy snapshot is nil")
	}

	// 校验角色权限格式
	for name, role := range s.Roles {
		if role == nil {
			return fmt.Errorf("authz: role %q is nil", name)
		}
		for _, perm := range role.Permissions {
			if err := validatePermission(perm); err != nil {
				return fmt.Errorf("authz: role %q: %w", name, err)
			}
		}
	}

	// 校验绑定引用的角色是否存在
	for bindingName, binding := range s.Bindings {
		if binding == nil {
			return fmt.Errorf("authz: binding %q is nil", bindingName)
		}
		if len(binding.ServiceTypes) == 0 {
			return fmt.Errorf("authz: binding %q has no serviceTypes", bindingName)
		}
		if len(binding.Roles) == 0 {
			return fmt.Errorf("authz: binding %q has no roles", bindingName)
		}
		for _, roleName := range binding.Roles {
			if _, ok := s.Roles[roleName]; !ok {
				return fmt.Errorf("authz: binding %q references unknown role %q", bindingName, roleName)
			}
		}
	}

	return nil
}

// Normalize 标准化策略快照（去除权限前后空格、去重）。
func (s *PolicySnapshot) Normalize() {
	if s == nil {
		return
	}
	for _, role := range s.Roles {
		if role == nil {
			continue
		}
		seen := make(map[string]struct{})
		normalized := make([]string, 0, len(role.Permissions))
		for _, perm := range role.Permissions {
			perm = strings.TrimSpace(perm)
			if perm == "" {
				continue
			}
			if _, ok := seen[perm]; ok {
				continue
			}
			seen[perm] = struct{}{}
			normalized = append(normalized, perm)
		}
		role.Permissions = normalized
	}
}

// validatePermission 校验单个权限格式。
// 合法格式："*", "Service.*", "Service.Method*", "Service.Method"
func validatePermission(perm string) error {
	perm = strings.TrimSpace(perm)
	if perm == "" {
		return fmt.Errorf("permission is empty")
	}
	if perm == "*" {
		return nil
	}
	// 必须包含 "." 分隔 Service 和 Method 部分
	dotIdx := strings.Index(perm, ".")
	if dotIdx <= 0 {
		return fmt.Errorf("permission %q: must be in format 'Service.Method' or contain '*'", perm)
	}
	return nil
}

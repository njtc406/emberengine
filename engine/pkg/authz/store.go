package authz

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PolicyEvent 表示策略变更事件。
type PolicyEvent struct {
	Type     PolicyEventType
	Snapshot *PolicySnapshot
	Err      error
}

// PolicyEventType 策略事件类型。
type PolicyEventType int

const (
	// PolicyEventUpdate 策略更新事件。
	PolicyEventUpdate PolicyEventType = iota
	// PolicyEventDelete 策略删除事件（回退到空策略）。
	PolicyEventDelete
	// PolicyEventError watch 错误事件。
	PolicyEventError
)

// PolicyStore 策略存储接口。
// Load 返回当前策略快照；Watch 返回持续的策略变更事件通道。
type PolicyStore interface {
	// Load 加载当前策略快照。
	Load(ctx context.Context) (*PolicySnapshot, error)

	// Watch 返回策略变更事件通道。ctx 取消时通道关闭。
	// 对于不支持 watch 的实现（如本地文件），可返回 nil channel。
	Watch(ctx context.Context) (<-chan PolicyEvent, error)

	// Close 关闭 store 并释放资源。
	Close() error
}

// LocalPolicyStore 从本地 YAML 文件加载策略的 Store 实现。
// 不支持 watch（启动时一次性加载）。
type LocalPolicyStore struct {
	path string
}

// NewLocalPolicyStore 创建本地策略文件 Store。
func NewLocalPolicyStore(path string) *LocalPolicyStore {
	return &LocalPolicyStore{path: path}
}

// Load 从本地文件加载策略快照。
func (s *LocalPolicyStore) Load(_ context.Context) (*PolicySnapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("authz: read policy file %q: %w", s.path, err)
	}

	var snapshot PolicySnapshot
	if err := yaml.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("authz: parse policy file %q: %w", s.path, err)
	}

	return &snapshot, nil
}

// Watch 本地文件 Store 不支持 watch，返回 nil channel。
func (s *LocalPolicyStore) Watch(_ context.Context) (<-chan PolicyEvent, error) {
	return nil, nil
}

// Close 本地文件 Store 无需释放资源。
func (s *LocalPolicyStore) Close() error {
	return nil
}

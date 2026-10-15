package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// KVClient 是对 etcd kv 操作的最小抽象，便于测试时 fake。
type KVClient interface {
	Get(ctx context.Context, key string) ([]byte, int64, error)
	Watch(ctx context.Context, prefix string) (<-chan WatchEvent, error)
	Close() error
}

// WatchEvent etcd watch 事件。
type WatchEvent struct {
	Type  WatchEventType
	Key   string
	Value []byte
}

// WatchEventType watch 事件类型。
type WatchEventType int

const (
	WatchEventPut WatchEventType = iota
	WatchEventDelete
)

// EtcdPolicyStore 基于 etcd 的策略 Store。
type EtcdPolicyStore struct {
	client KVClient
	prefix string // 例如 /ember/authz/policies

	mu       sync.Mutex
	cancelFn context.CancelFunc
}

// NewEtcdPolicyStore 创建 etcd 策略 Store。
func NewEtcdPolicyStore(client KVClient, prefix string) *EtcdPolicyStore {
	return &EtcdPolicyStore{
		client: client,
		prefix: prefix,
	}
}

// Load 从 etcd 加载完整策略快照。
func (s *EtcdPolicyStore) Load(ctx context.Context) (*PolicySnapshot, error) {
	key := s.prefix + "/snapshot"
	data, _, err := s.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("authz: etcd get %q: %w", key, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("authz: etcd key %q is empty", key)
	}

	var snapshot PolicySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("authz: etcd parse snapshot: %w", err)
	}
	return &snapshot, nil
}

// Watch 监听 etcd prefix 下的策略变更。
func (s *EtcdPolicyStore) Watch(ctx context.Context) (<-chan PolicyEvent, error) {
	watchCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()

	wch, err := s.client.Watch(watchCtx, s.prefix)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("authz: etcd watch %q: %w", s.prefix, err)
	}

	out := make(chan PolicyEvent, 8)
	go func() {
		defer close(out)
		for {
			select {
			case <-watchCtx.Done():
				return
			case evt, ok := <-wch:
				if !ok {
					return
				}
				pEvt := s.convertEvent(evt)
				select {
				case out <- pEvt:
				case <-watchCtx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}

// Close 关闭 etcd 客户端。
func (s *EtcdPolicyStore) Close() error {
	s.mu.Lock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.mu.Unlock()
	return s.client.Close()
}

func (s *EtcdPolicyStore) convertEvent(evt WatchEvent) PolicyEvent {
	switch evt.Type {
	case WatchEventPut:
		var snapshot PolicySnapshot
		if err := json.Unmarshal(evt.Value, &snapshot); err != nil {
			return PolicyEvent{Type: PolicyEventError, Err: err}
		}
		return PolicyEvent{Type: PolicyEventUpdate, Snapshot: &snapshot}
	case WatchEventDelete:
		return PolicyEvent{Type: PolicyEventDelete}
	default:
		return PolicyEvent{Type: PolicyEventError, Err: fmt.Errorf("unknown event type")}
	}
}

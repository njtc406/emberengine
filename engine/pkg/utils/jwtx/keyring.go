package jwtx

import (
	"errors"
	"fmt"
	"sync"
)

// MinKeyLength 最小 secret 长度（字节），低于此值拒绝添加。
const MinKeyLength = 16

var (
	ErrKeyTooShort = errors.New("jwtx: key length must be at least 16 bytes")
	ErrKeyNotFound = errors.New("jwtx: key not found for kid")
	ErrEmptyKID    = errors.New("jwtx: kid must not be empty")
	ErrNoActiveKey = errors.New("jwtx: no active key in keyring")
)

// KeyRing 管理多个 JWT 签名密钥，支持按 kid 查找和平滑轮换。
// 签发 token 使用 ActiveKID 对应的 key；校验 token 根据 token header 中的 kid 查找 key。
type KeyRing struct {
	mu        sync.RWMutex
	activeKID string
	keys      map[string][]byte // kid → secret
}

// NewKeyRing 创建空的 KeyRing。
func NewKeyRing() *KeyRing {
	return &KeyRing{
		keys: make(map[string][]byte),
	}
}

// AddKey 添加或替换一个 key。secret 长度必须 >= MinKeyLength。
func (kr *KeyRing) AddKey(kid string, secret []byte) error {
	if kid == "" {
		return ErrEmptyKID
	}
	if len(secret) < MinKeyLength {
		return fmt.Errorf("%w: got %d", ErrKeyTooShort, len(secret))
	}
	// 防御性拷贝
	s := make([]byte, len(secret))
	copy(s, secret)

	kr.mu.Lock()
	kr.keys[kid] = s
	kr.mu.Unlock()
	return nil
}

// SetActiveKID 设置当前签发使用的 kid。该 kid 必须已通过 AddKey 添加。
func (kr *KeyRing) SetActiveKID(kid string) error {
	kr.mu.RLock()
	_, ok := kr.keys[kid]
	kr.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrKeyNotFound, kid)
	}
	kr.mu.Lock()
	kr.activeKID = kid
	kr.mu.Unlock()
	return nil
}

// ActiveKID 返回当前活跃的 kid。
func (kr *KeyRing) ActiveKID() string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return kr.activeKID
}

// GetActiveKey 返回当前活跃的 kid 和 secret。
func (kr *KeyRing) GetActiveKey() (kid string, secret []byte, err error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	if kr.activeKID == "" {
		return "", nil, ErrNoActiveKey
	}
	s, ok := kr.keys[kr.activeKID]
	if !ok {
		return "", nil, fmt.Errorf("%w: active kid %q", ErrKeyNotFound, kr.activeKID)
	}
	return kr.activeKID, s, nil
}

// GetKey 按 kid 查找 secret。
func (kr *KeyRing) GetKey(kid string) ([]byte, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	s, ok := kr.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrKeyNotFound, kid)
	}
	return s, nil
}

// RemoveKey 移除一个 key。如果移除的是 activeKID，则清空 activeKID。
func (kr *KeyRing) RemoveKey(kid string) {
	kr.mu.Lock()
	delete(kr.keys, kid)
	if kr.activeKID == kid {
		kr.activeKID = ""
	}
	kr.mu.Unlock()
}

// KeyCount 返回当前 key 数量。
func (kr *KeyRing) KeyCount() int {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return len(kr.keys)
}

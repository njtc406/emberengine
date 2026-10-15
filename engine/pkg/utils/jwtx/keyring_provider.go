package jwtx

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

// KeyRingProvider 基于 KeyRing 的 JWT 签发/校验 Provider。
// 签发 token 时在 header 中写入 kid，校验时按 kid 查找对应 key。
type KeyRingProvider struct {
	ring *KeyRing
}

// NewKeyRingProvider 创建基于 KeyRing 的 Provider。
func NewKeyRingProvider(ring *KeyRing) *KeyRingProvider {
	return &KeyRingProvider{ring: ring}
}

// CreateJwtToken 使用 KeyRing 中的活跃 key 签发 token。
func (p *KeyRingProvider) CreateJwtToken(uid int64, expireTime time.Duration) (string, error) {
	kid, secret, err := p.ring.GetActiveKey()
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, EmberClaims{
		UserID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expireTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})
	token.Header["kid"] = kid

	return token.SignedString(secret)
}

// ParseJwtToken 根据 token header 中的 kid 查找 key 进行校验。
func (p *KeyRingProvider) ParseJwtToken(tokenStr string) (*EmberClaims, error) {
	claims := &EmberClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("jwtx: unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("jwtx: missing kid in token header")
		}
		secret, err := p.ring.GetKey(kid)
		if err != nil {
			return nil, err
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, def.ErrTokenInvalid
	}
	return claims, nil
}

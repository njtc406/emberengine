// Package jwtx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/20 0020 0:12
// 最后更新:  yr  2025/8/20 0020 0:12
package jwtx

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

var ErrJWTSecretNotConfigured = errors.New("jwt secret not configured")

type Provider struct {
	secret []byte
}

func NewProvider(secret string) *Provider {
	return &Provider{secret: []byte(secret)}
}

func (p *Provider) Secret() []byte {
	if p == nil {
		return nil
	}
	return p.secret
}

type EmberClaims struct {
	jwt.RegisteredClaims
	UserID int64 `json:"uid"`
}

func (p *Provider) CreateJwtToken(uid int64, expireTime time.Duration) (string, error) {
	secret := p.Secret()
	if len(secret) == 0 {
		return "", ErrJWTSecretNotConfigured
	}
	jtc := jwt.NewWithClaims(jwt.SigningMethodHS256, EmberClaims{
		UserID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expireTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	})

	return jtc.SignedString(secret)
}

func (p *Provider) ParseJwtToken(tokenStr string) (*EmberClaims, error) {
	secret := p.Secret()
	if len(secret) == 0 {
		return nil, ErrJWTSecretNotConfigured
	}
	claims := &EmberClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("jwtx: unexpected signing method: %v", token.Header["alg"])
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

var (
	defaultProvider *Provider
	providerLock    sync.RWMutex
)

func init() {
	defaultProvider = NewProvider(os.Getenv("EMBER_JWT_SECRET"))
}

func GetDefaultProvider() *Provider {
	providerLock.RLock()
	p := defaultProvider
	providerLock.RUnlock()
	return p
}

// CreateJwtToken 生成一个jwt token
func CreateJwtToken(uid int64, expireTime time.Duration) (string, error) {
	return GetDefaultProvider().CreateJwtToken(uid, expireTime)
}

func CreateJwtTokenWithSecret(secret string, uid int64, expireTime time.Duration) (string, error) {
	return NewProvider(secret).CreateJwtToken(uid, expireTime)
}

func ParseJwtToken(tokenStr string) (*EmberClaims, error) {
	return GetDefaultProvider().ParseJwtToken(tokenStr)
}

func ParseJwtTokenWithSecret(secret, tokenStr string) (*EmberClaims, error) {
	return NewProvider(secret).ParseJwtToken(tokenStr)
}

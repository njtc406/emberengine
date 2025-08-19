// Package jwtx
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/8/20 0020 0:12
// 最后更新:  yr  2025/8/20 0020 0:12
package jwtx

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"time"
)

var jwtSecret = []byte("ember-secret-pwd-xxyyzz") // 和 Auth 里的保持一致

type EmberClaims struct {
	jwt.RegisteredClaims
	UserID int64 `json:"uid"`
}

// CreateJwtToken 生成一个jwt token
func CreateJwtToken(uid int64, expireTime time.Duration) (string, error) {
	jtc := jwt.NewWithClaims(jwt.SigningMethodHS256, EmberClaims{
		UserID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expireTime)), // 过期时间
			IssuedAt:  jwt.NewNumericDate(time.Now()),                 // 签发时间
			NotBefore: jwt.NewNumericDate(time.Now()),                 // 生效时间
		},
	})

	return jtc.SignedString(jwtSecret)
}

func ParseJwtToken(tokenStr string) (*EmberClaims, error) {
	claims := &EmberClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, def.ErrTokenInvalid
	}
	return claims, nil
}

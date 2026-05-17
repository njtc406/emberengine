package jwtx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyRing_AddKey_TooShort(t *testing.T) {
	kr := NewKeyRing()
	err := kr.AddKey("k1", []byte("short"))
	assert.ErrorIs(t, err, ErrKeyTooShort)
}

func TestKeyRing_AddKey_EmptyKID(t *testing.T) {
	kr := NewKeyRing()
	err := kr.AddKey("", []byte("0123456789abcdef"))
	assert.ErrorIs(t, err, ErrEmptyKID)
}

func TestKeyRing_AddAndGet(t *testing.T) {
	kr := NewKeyRing()
	secret := []byte("0123456789abcdef0123456789abcdef")
	require.NoError(t, kr.AddKey("v1", secret))
	got, err := kr.GetKey("v1")
	require.NoError(t, err)
	assert.Equal(t, secret, got)
}

func TestKeyRing_SetActiveKID(t *testing.T) {
	kr := NewKeyRing()
	require.NoError(t, kr.AddKey("v1", []byte("0123456789abcdef")))
	require.NoError(t, kr.SetActiveKID("v1"))
	assert.Equal(t, "v1", kr.ActiveKID())
}

func TestKeyRing_SetActiveKID_NotFound(t *testing.T) {
	kr := NewKeyRing()
	err := kr.SetActiveKID("nonexistent")
	assert.ErrorIs(t, err, ErrKeyNotFound)
}

func TestKeyRing_GetActiveKey_NoActive(t *testing.T) {
	kr := NewKeyRing()
	_, _, err := kr.GetActiveKey()
	assert.ErrorIs(t, err, ErrNoActiveKey)
}

func TestKeyRing_RemoveKey_ClearsActive(t *testing.T) {
	kr := NewKeyRing()
	require.NoError(t, kr.AddKey("v1", []byte("0123456789abcdef")))
	require.NoError(t, kr.SetActiveKID("v1"))
	kr.RemoveKey("v1")
	assert.Empty(t, kr.ActiveKID())
	assert.Equal(t, 0, kr.KeyCount())
}

func TestKeyRingProvider_CreateAndParse(t *testing.T) {
	kr := NewKeyRing()
	require.NoError(t, kr.AddKey("v1", []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(t, kr.SetActiveKID("v1"))

	p := NewKeyRingProvider(kr)
	tokenStr, err := p.CreateJwtToken(42, time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	claims, err := p.ParseJwtToken(tokenStr)
	require.NoError(t, err)
	assert.Equal(t, int64(42), claims.UserID)
}

func TestKeyRingProvider_OldKeyCanVerify(t *testing.T) {
	kr := NewKeyRing()
	require.NoError(t, kr.AddKey("v1", []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(t, kr.SetActiveKID("v1"))

	p := NewKeyRingProvider(kr)
	tokenV1, err := p.CreateJwtToken(1, time.Hour)
	require.NoError(t, err)

	// 轮换到 v2
	require.NoError(t, kr.AddKey("v2", []byte("abcdef0123456789abcdef0123456789")))
	require.NoError(t, kr.SetActiveKID("v2"))

	// v1 token 仍可验证
	claims, err := p.ParseJwtToken(tokenV1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), claims.UserID)

	// v2 也可签发和验证
	tokenV2, err := p.CreateJwtToken(2, time.Hour)
	require.NoError(t, err)
	claims2, err := p.ParseJwtToken(tokenV2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), claims2.UserID)
}

func TestKeyRingProvider_UnknownKID_Rejected(t *testing.T) {
	kr := NewKeyRing()
	require.NoError(t, kr.AddKey("v1", []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(t, kr.SetActiveKID("v1"))

	p := NewKeyRingProvider(kr)
	tokenStr, err := p.CreateJwtToken(1, time.Hour)
	require.NoError(t, err)

	// 移除 v1，模拟未知 kid
	kr.RemoveKey("v1")

	_, err = p.ParseJwtToken(tokenStr)
	assert.Error(t, err)
}

func TestKeyRingProvider_NoActiveKey_CreateFails(t *testing.T) {
	kr := NewKeyRing()
	p := NewKeyRingProvider(kr)
	_, err := p.CreateJwtToken(1, time.Hour)
	assert.ErrorIs(t, err, ErrNoActiveKey)
}

func TestKeyRing_DefensiveCopy(t *testing.T) {
	kr := NewKeyRing()
	secret := []byte("0123456789abcdef")
	require.NoError(t, kr.AddKey("v1", secret))

	// 修改原始 secret 不应影响存储的 key
	secret[0] = 'X'
	got, err := kr.GetKey("v1")
	require.NoError(t, err)
	assert.Equal(t, byte('0'), got[0])
}

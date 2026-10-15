// Package memdbx
// @Title  title
// @Description  基于sqlite内存模式的数据库
// @Author  yr  2025/6/19
// @Update  yr  2025/6/19
package memdbx

import (
	"fmt"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type MemDB struct {
	db *gorm.DB
}

func NewMemDB() (*MemDB, error) {
	slowLogger := logger.Default.LogMode(logger.Warn)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                 slowLogger,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &MemDB{db: db}, nil
}

func (m *MemDB) GetDB() *gorm.DB {
	if m == nil {
		return nil
	}
	return m.db
}

func (m *MemDB) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

var (
	defaultMemDB *MemDB
	defaultMu    sync.RWMutex
)

// Start 启动默认内存 DB（兼容旧接口）。
func Start() {
	if err := StartWithError(); err != nil {
		fmt.Printf("memdb init failed: %v\n", err)
	}
}

// StartWithError 启动默认内存 DB，并返回初始化错误。
func StartWithError() error {
	m, err := NewMemDB()
	if err != nil {
		return fmt.Errorf("memdb init failed: %w", err)
	}
	defaultMu.Lock()
	defaultMemDB = m
	defaultMu.Unlock()
	return nil
}

func GetDB() *gorm.DB {
	defaultMu.RLock()
	m := defaultMemDB
	defaultMu.RUnlock()
	if m == nil {
		return nil
	}
	return m.GetDB()
}

func SetDefaultMemDB(m *MemDB) {
	defaultMu.Lock()
	defaultMemDB = m
	defaultMu.Unlock()
}

func GetDefaultMemDB() *MemDB {
	defaultMu.RLock()
	m := defaultMemDB
	defaultMu.RUnlock()
	return m
}

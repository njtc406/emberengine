// Package repository
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2025/6/19 0019 1:15
// 最后更新:  yr  2025/6/19 0019 1:15
package repository

import (
	"fmt"
	"github.com/hashicorp/go-memdb"
	"github.com/njtc406/emberengine/engine/pkg/actor"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
)

const ServiceTableName = "services"

// 定义索引信息的辅助结构
type indexDef struct {
	Name    string
	Unique  bool
	Indexer memdb.Indexer
}

// Repository 负责管理所有服务数据
type Repository struct {
	db *memdb.MemDB
}

// NewRepository 初始化并创建索引结构
func NewRepository() (*Repository, error) {
	// 字段到索引器的映射
	indexDefs := []indexDef{
		{"id", true, &memdb.StringFieldIndex{Field: "ServiceUid"}},
		{"service_name", false, &memdb.StringFieldIndex{Field: "ServiceName"}},
		{"service_id", false, &memdb.StringFieldIndex{Field: "ServiceId"}},
		{"service_type", false, &memdb.StringFieldIndex{Field: "ServiceType"}},
		{"is_master", false, &memdb.BoolFieldIndex{Field: "IsMaster"}},
		{"node_uid", false, &memdb.StringFieldIndex{Field: "NodeUid"}},
		{"server_id", false, &memdb.IntFieldIndex{Field: "ServerId"}},
		{"state", false, &memdb.IntFieldIndex{Field: "State"}},
		{"version", false, &memdb.IntFieldIndex{Field: "Version"}},
	}

	// 动态生成索引map
	indexes := make(map[string]*memdb.IndexSchema, len(indexDefs))
	for _, def := range indexDefs {
		indexes[def.Name] = &memdb.IndexSchema{
			Name:    def.Name,
			Unique:  def.Unique,
			Indexer: def.Indexer,
		}
	}

	schema := &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			ServiceTableName: {
				Name:    ServiceTableName,
				Indexes: indexes,
			},
		},
	}

	db, err := memdb.NewMemDB(schema)
	if err != nil {
		return nil, err
	}

	return &Repository{db: db}, nil
}

// Insert 插入或更新服务元数据
func (r *Repository) Insert(dispatcher inf.IRpcDispatcher) error {
	txn := r.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert(ServiceTableName, dispatcher); err != nil {
		return err
	}

	txn.Commit()
	return nil
}

// Delete 根据 ServiceUid 删除
func (r *Repository) Delete(serviceUid string) error {
	txn := r.db.Txn(true)
	defer txn.Abort()

	raw, err := txn.Get(ServiceTableName, "id", serviceUid)
	if err != nil || raw == nil {
		return fmt.Errorf("service %s not found", serviceUid)
	}

	if err := txn.Delete(ServiceTableName, raw); err != nil {
		return err
	}

	txn.Commit()
	return nil
}

// SelectOptions 定义查询条件（链式构造）
type SelectOptions struct {
	ServiceName *string
	ServiceId   *string
	ServiceType *string
	IsMaster    *bool
	NodeUid     *string
	ServerId    *int32
	State       *int32
	VersionMin  *int64 // 版本号范围示例
	VersionMax  *int64
}

// Select 查询接口，根据条件返回符合的服务列表
func (r *Repository) Select(sender *actor.PID, opts SelectOptions) inf.IBus {
	txn := r.db.Txn(false)
	defer txn.Abort()

	var result []inf.IRpcDispatcher

	// 优先用索引查询
	// 简化示例：优先用ServiceName索引查询过滤起始集合作为示范
	if opts.ServiceName != nil {
		it, err := txn.Get("service", "service_name", *opts.ServiceName)
		if err != nil {
			return nil, err
		}

		for obj := it.Next(); obj != nil; obj = it.Next() {
			svc := obj.(inf.IRpcDispatcher)
			if matchService(&opts, svc) {
				result = append(result, svc)
			}
		}
		return result, nil
	}

	// 如果没有索引能用，就全表扫描（可以优化成多索引合并）
	it, err := txn.Get("service", "id") // 全表扫描
	if err != nil {
		return nil, err
	}

	for obj := it.Next(); obj != nil; obj = it.Next() {
		svc := obj.(*ServiceMeta)
		if matchService(&opts, svc) {
			result = append(result, svc)
		}
	}

	return result, nil
}

// matchService 判断一个ServiceMeta是否满足SelectOptions中的条件
func matchService(opts *SelectOptions, svc *ServiceMeta) bool {
	if opts.ServiceId != nil && *opts.ServiceId != svc.ServiceId {
		return false
	}
	if opts.ServiceType != nil && *opts.ServiceType != svc.ServiceType {
		return false
	}
	if opts.IsMaster != nil && *opts.IsMaster != svc.IsMaster {
		return false
	}
	if opts.NodeUid != nil && *opts.NodeUid != svc.NodeUid {
		return false
	}
	if opts.ServerId != nil && *opts.ServerId != svc.ServerId {
		return false
	}
	if opts.State != nil && *opts.State != svc.State {
		return false
	}
	if opts.VersionMin != nil && svc.Version < *opts.VersionMin {
		return false
	}
	if opts.VersionMax != nil && svc.Version > *opts.VersionMax {
		return false
	}
	return true
}

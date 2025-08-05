package dedup

import (
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

// TODO 这里有点问题,使用cache模式,会存在一个短时高并发时,map被击穿的风险,在gate上表现可能会比较突出,所以可能需要考虑使用不同的模式
// 对于高并发的节点,使用lru缓存模式,设置一个固定大小的缓存,或者使用redis

var duplicator inf.IDeDuplicator

type DeDuplicatorOption struct {
	TTL      time.Duration
	CleanTTL time.Duration
	Size     int
}

type Option func(option *DeDuplicatorOption)

// TODO 在node启动时根据配置来初始化
func Init(tp string, options ...Option) {
	if duplicator != nil {
		return
	}

	option := &DeDuplicatorOption{}
	for _, op := range options {
		op(option)
	}

	duplicator = new(tp, option)
}

func GetDeDuplicator() inf.IDeDuplicator {
	if duplicator == nil {
		duplicator = new(def.DeDuplicatorTypeTTL, &DeDuplicatorOption{})
	}
	return duplicator
}

func new(tp string, option *DeDuplicatorOption) inf.IDeDuplicator {
	if option == nil {
		option = &DeDuplicatorOption{}
	}
	var dpt inf.IDeDuplicator
	switch tp {
	case def.DeDuplicatorTypeLRU:
		dpt = NewLRUDeDuplicator(option.Size)
	default:
		dpt = NewTTLDeDuplicator(option.TTL, option.CleanTTL)
	}
	return dpt
}

func reqIdKey(serviceUid string, id uint64) string {
	return fmt.Sprintf("rpc_%s_reqid_%d", serviceUid, id)
}

// TTLDeDuplicator 用于识别重复 ReqId，防止重复处理
type TTLDeDuplicator struct {
	reqCache *cache.Cache
}

func NewTTLDeDuplicator(ttl, cleanTTL time.Duration) *TTLDeDuplicator {
	d := &TTLDeDuplicator{}
	if ttl <= 0 {
		ttl = def.DefaultDeDuplicatorTTL
	}
	if cleanTTL <= 0 {
		cleanTTL = ttl * 3
	}
	d.reqCache = cache.New(ttl, cleanTTL)
	return d
}

// Seen 判断是否已经见过该请求，
// 如果没见过，会立即插入一条标记（pending）
func (d *TTLDeDuplicator) Seen(serviceUid string, id uint64) bool {
	key := reqIdKey(serviceUid, id)
	_, found := d.reqCache.Get(key)
	if found {
		return true
	}
	// 第一次见，先插入标记
	d.reqCache.SetDefault(key, "pending")
	return false
}

// MarkDone 表示处理完成，后续可以选择缓存响应结果（可扩展）
func (d *TTLDeDuplicator) MarkDone(serviceUid string, id uint64) {
	d.reqCache.SetDefault(reqIdKey(serviceUid, id), "done")
}

type LRUDeDuplicator struct {
	mu    sync.Mutex
	cache *lru.Cache // TODO 这个需要换一个库，使用github.com/bluele/gcache来实现
}

func NewLRUDeDuplicator(size int) *LRUDeDuplicator {
	c, _ := lru.New(size)
	return &LRUDeDuplicator{cache: c}
}

func (d *LRUDeDuplicator) Seen(serviceUid string, id uint64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := reqIdKey(serviceUid, id)
	if _, found := d.cache.Get(key); found {
		return true
	}
	d.cache.Add(key, struct{}{})
	return false
}

func (d *LRUDeDuplicator) MarkDone(serviceUid string, id uint64) {
	// no-op for LRU since it's already added in Seen
}

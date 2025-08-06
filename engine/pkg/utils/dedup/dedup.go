package dedup

import (
	"fmt"
	"github.com/bluele/gcache"
	"github.com/njtc406/emberengine/engine/pkg/def"
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

var duplicator inf.IDeDuplicator

type DeDuplicatorOption struct {
	TTL      time.Duration
	CleanTTL time.Duration
	Size     int
}

type Option func(option *DeDuplicatorOption)

func WithTTL(ttl time.Duration) Option {
	return func(option *DeDuplicatorOption) {
		option.TTL = ttl
	}
}

func WithCleanTTL(ttl time.Duration) Option {
	return func(option *DeDuplicatorOption) {
		option.CleanTTL = ttl
	}
}

func WithSize(size int) Option {
	return func(option *DeDuplicatorOption) {
		option.Size = size
	}
}

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
		dpt = NewLRUDeDuplicator(option.Size, option.TTL)
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
// 如果没见过，会立即插入一条标记
func (d *TTLDeDuplicator) Seen(serviceUid string, id uint64) bool {
	key := reqIdKey(serviceUid, id)
	_, found := d.reqCache.Get(key)
	if found {
		return true
	}
	// 第一次见，先插入标记
	d.reqCache.SetDefault(key, struct{}{})
	return false
}

type LRUDeDuplicator struct {
	mu    sync.Mutex
	ttl   time.Duration
	cache gcache.Cache
}

func NewLRUDeDuplicator(size int, ttl time.Duration) *LRUDeDuplicator {
	return &LRUDeDuplicator{cache: gcache.New(size).LRU().Build(), ttl: ttl}
}

func (d *LRUDeDuplicator) Seen(serviceUid string, id uint64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := reqIdKey(serviceUid, id)
	if _, err := d.cache.Get(key); err == nil {
		return true
	}
	d.cache.SetWithExpire(key, struct{}{}, d.ttl)
	return false
}

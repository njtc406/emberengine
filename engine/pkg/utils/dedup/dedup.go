package dedup

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/def"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

var reqDuplicator *RpcReqDuplicator
var once = sync.Once{}

// RpcReqDuplicator 用于识别重复 ReqId，防止重复处理
type RpcReqDuplicator struct {
	reqCache *cache.Cache
}

func GetRpcReqDuplicator() *RpcReqDuplicator {
	if reqDuplicator == nil {
		reqDuplicator = &RpcReqDuplicator{}
	}
	return reqDuplicator
}

func (d *RpcReqDuplicator) Init(ttl time.Duration) {
	if ttl == 0 {
		ttl = def.DefaultReqDuplicatorTTL
	}
	once.Do(func() {
		reqDuplicator.reqCache = cache.New(ttl, ttl*3)
	})
}

// Seen 判断是否已经见过该请求，
// 如果没见过，会立即插入一条标记（pending）
func (d *RpcReqDuplicator) Seen(reqId uint64) bool {
	if reqDuplicator == nil {
		return false
	}
	_, found := d.reqCache.Get(reqIdKey(reqId))
	if found {
		return true
	}
	// 第一次见，先插入标记
	d.reqCache.SetDefault(reqIdKey(reqId), "pending")
	return false
}

// MarkDone 表示处理完成，后续可以选择缓存响应结果（可扩展）
func (d *RpcReqDuplicator) MarkDone(reqId uint64) {
	if reqDuplicator == nil {
		return
	}
	d.reqCache.SetDefault(reqIdKey(reqId), "done")
}

func reqIdKey(id uint64) string {
	return "rpc_reqid_" + fmt.Sprint(id)
}

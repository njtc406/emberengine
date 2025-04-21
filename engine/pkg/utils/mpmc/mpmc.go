// Package mpmc
// @Title  title
// @Description  desc
// @Author  yr  2025/2/7
// @Update  yr  2025/2/7
package mpmc

import (
	"math/bits"
	"sync/atomic"
	"time"
	"unsafe"
)

const maxBackoff uint32 = 8 // 最大退避时间(最大退避3次)
const cacheLineSize = 128

// slot 表示环形队列中的一个槽
// 每个槽包含一个 value 和一个 sequence
// sequence 用于标识该槽当前的状态，参照 Vyukov 算法的设计
type slot[T any] struct {
	sequence int64 // 状态标识
	value    T     // 存储的数据
	// 添加 padding 避免伪共享
	_ [cacheLineSize - unsafe.Sizeof(int64(0))%cacheLineSize]byte
}

// Queue 是一个支持多生产者-多消费者的无锁环形队列
// 队列大小必须为 2 的幂次方，以便使用位运算优化取模操作
type Queue[T any] struct {
	mask      int64 // capacity - 1
	_padding1 [8]int64
	size      int64 // 队列容量
	_padding2 [8]int64
	// head 和 tail 使用原子操作更新
	head      int64 // 消费者读取位置
	_padding3 [8]int64
	tail      int64 // 生产者写入位置
	_padding4 [8]int64
	buffer    []slot[T]
}

// NewQueue 创建一个新的 Queue，capacity 会向上取整为 2 的幂次方
func NewQueue[T any](capacity int64) *Queue[T] {
	if capacity == 0 {
		panic("capacity must be > 0")
	}
	if capacity&(capacity-1) != 0 {
		capacity = roundUpToPowerOfTwo(capacity)
	}
	buffer := make([]slot[T], capacity)
	// 初始化每个槽的 sequence 为槽的下标
	for i := int64(0); i < capacity; i++ {
		buffer[i].sequence = i
	}
	return &Queue[T]{
		buffer: buffer,
		mask:   capacity - 1,
		size:   capacity,
		head:   0,
		tail:   0,
	}
}

// roundUpToPowerOfTwo 向上取整到 2 的幂次方
func roundUpToPowerOfTwo(v int64) int64 {
	if v <= 0 {
		return 1024
	}
	// 将 v-1 转换为 uint64 后计算前导零
	return int64(1) << (64 - bits.LeadingZeros64(uint64(v-1)))
}

// Push 入队操作 并发安全
// 如果队列满了则返回 false，否则返回 true。
func (q *Queue[T]) Push(item T) bool {
	var pos, seq, dif int64
	var backoff uint32 = 1 // 初始退避 1 微秒
	var cell *slot[T]
	for {
		pos = atomic.LoadInt64(&q.tail)
		// 在循环中给 cell 赋值
		cell = &q.buffer[pos&q.mask]
		seq = atomic.LoadInt64(&cell.sequence)
		dif = seq - pos
		if dif == 0 {
			if atomic.CompareAndSwapInt64(&q.tail, pos, pos+1) {
				break // 跳出循环，此时 cell 仍然指向最后一次迭代的槽
			}
		} else if dif < 0 {
			return false
		} else {
			if backoff < maxBackoff {
				backoff *= 2
			}
			time.Sleep(time.Microsecond * time.Duration(backoff))
			//runtime.Gosched()
		}
	}
	// 循环结束后，我们仍然可以使用 cell，因为它是在循环外声明的
	cell.value = item
	atomic.StoreInt64(&cell.sequence, pos+1)
	return true
}

// Pop 出队操作 并发安全
// 如果队列为空，则返回零值和 false；否则返回出队的数据和 true。
func (q *Queue[T]) Pop() (T, bool) {
	var pos, seq, dif int64
	var backoff uint32 = 1

	for {
		pos = atomic.LoadInt64(&q.head)
		cell := &q.buffer[pos&q.mask]
		seq = atomic.LoadInt64(&cell.sequence)
		// 对于消费者，槽有数据时应满足：cell.sequence == pos+1
		dif = seq - (pos + 1)
		if dif == 0 {
			// 尝试CAS更新 head
			if atomic.CompareAndSwapInt64(&q.head, pos, pos+1) {
				break
			}
		} else if dif < 0 {
			// 队列为空
			var zero T
			return zero, false
		} else {
			// 使用指数退避来减少忙等开销
			if backoff < maxBackoff {
				backoff *= 2
			}
			time.Sleep(time.Microsecond * time.Duration(backoff))

			//runtime.Gosched()
		}
	}
	// 读取槽中的数据
	ret := q.buffer[pos&q.mask].value
	// 更新槽的序号为 pos + q.size，以便后续生产者写入时判断该槽可用
	atomic.StoreInt64(&q.buffer[pos&q.mask].sequence, pos+q.mask+1)
	return ret, true
}

// Len 返回队列当前大小（大致值，因为在并发情况下可能不精确）
func (q *Queue[T]) Len() int {
	head := atomic.LoadInt64(&q.head)
	tail := atomic.LoadInt64(&q.tail)
	if tail >= head {
		return int(tail - head)
	}
	return int(q.size - head + tail)
}

func (q *Queue[T]) Empty() bool {
	return atomic.LoadInt64(&q.head) == atomic.LoadInt64(&q.tail)
}

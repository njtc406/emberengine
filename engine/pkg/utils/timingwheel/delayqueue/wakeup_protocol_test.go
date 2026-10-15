package delayqueue
package delayqueue

import (
	"sync/atomic"
	"testing"
)

func TestOfferRetainsWakeupBeforeReceiver(t *testing.T) {
	queue := New(1)
	atomic.StoreInt32(&queue.sleeping, 1)
	queue.Offer("first", 10)
	select {
	case <-queue.wakeupC:
	default:
		t.Fatal("Offer lost wakeup before Poll started receiving")
	}
}

func TestOfferCoalescesWakeups(t *testing.T) {
	queue := New(1)
	for expiration := int64(10); expiration > 0; expiration-- {
		atomic.StoreInt32(&queue.sleeping, 1)
		queue.Offer(expiration, expiration)
	}
	if len(queue.wakeupC) != 1 {
		t.Fatalf("pending wakeups = %d, want 1", len(queue.wakeupC))
	}
}
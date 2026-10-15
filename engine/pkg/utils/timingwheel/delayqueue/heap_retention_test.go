package delayqueue

import "testing"

func TestPopReleasesBackingReference(t *testing.T) {
	queue := newPriorityQueue(4)
	queue.Push(&item{Value: new(int)})
	queue.Pop()
	if queue[:cap(queue)][0] != nil {
		t.Fatal("Pop retained the removed item in its backing array")
	}
}

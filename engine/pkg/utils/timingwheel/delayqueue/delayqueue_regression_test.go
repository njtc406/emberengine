package delayqueue

import (
	"testing"
	"time"
)

func TestDelayQueueOfferAfterPollExitDoesNotBlock(t *testing.T) {
	queue := New(1)
	exit := make(chan struct{})
	close(exit)

	pollDone := make(chan struct{})
	go func() {
		queue.Poll(exit, func() int64 { return time.Now().UnixMilli() })
		close(pollDone)
	}()

	select {
	case <-pollDone:
	case <-time.After(time.Second):
		t.Fatal("Poll did not exit")
	}

	offerDone := make(chan struct{})
	go func() {
		queue.Offer("after-stop", time.Now().UnixMilli())
		close(offerDone)
	}()

	select {
	case <-offerDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Offer blocked after Poll exited")
	}
}

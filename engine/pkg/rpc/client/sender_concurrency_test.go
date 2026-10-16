package client

import (
	inf "github.com/njtc406/emberengine/engine/pkg/interfaces"
	"sync"
	"testing"
)

type coldMailbox struct{ inf.IMailboxChannel }

func TestDispatcherConcurrentColdLocalSender(t *testing.T) {
	for round := 0; round < 50; round++ {
		d := &Dispatcher{sm: &SenderManager{}, IMailboxChannel: &coldMailbox{}}
		results := make([]inf.IRpcSender, 32)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) { defer wg.Done(); <-start; results[i] = d.getSender() }(i)
		}
		close(start)
		wg.Wait()
		for _, sender := range results {
			if sender == nil || sender != results[0] {
				t.Fatal("local sender must be initialized exactly once")
			}
		}
	}
}

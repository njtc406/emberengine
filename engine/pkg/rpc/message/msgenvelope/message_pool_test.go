package msgenvelope

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestMessagePoolGOMAXPROCSGrowth(t *testing.T) {
	if os.Getenv("MESSAGE_POOL_GROWTH_CHILD") == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMessagePoolGOMAXPROCSGrowth$")
		cmd.Env = append(os.Environ(), "MESSAGE_POOL_GROWTH_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("growth child: %v\n%s", err, out)
		}
		return
	}
	runtime.GOMAXPROCS(1)
	ReleaseMessage(NewMessage())
	runtime.GOMAXPROCS(8)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				v := NewMessage()
				if v.Method != "" || v.Request != nil {
					t.Error("message not reset")
				}
				v.Method = "owned"
				runtime.Gosched()
				if v.Method != "owned" {
					t.Error("live message overwritten")
				}
				ReleaseMessage(v)
			}
		}()
	}
	wg.Wait()
}

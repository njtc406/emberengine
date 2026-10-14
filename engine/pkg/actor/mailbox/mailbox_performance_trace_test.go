package mailbox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime/trace"
	"sync"
	"testing"
	"time"
)

// Discard overflow rather than allowing an unbounded diagnostic file. Overflow
// invalidates attribution; the metadata makes this explicit.
type mailboxTraceWriter struct {
	w         io.Writer
	remaining int64
	discarded int64
}

func (w *mailboxTraceWriter) Write(p []byte) (int, error) {
	n := len(p)
	keep := int64(n)
	if keep > w.remaining {
		keep = w.remaining
	}
	if keep > 0 {
		written, err := w.w.Write(p[:keep])
		w.remaining -= int64(written)
		if err != nil {
			return written, err
		}
		if int64(written) != keep {
			return written, io.ErrShortWrite
		}
	}
	w.discarded += int64(n) - keep
	return n, nil
}

func startMailboxTrace(t *testing.T, path string) func(string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := &mailboxTraceWriter{w: f, remaining: 64 << 20}
	started := time.Now().UTC()
	if err := trace.Start(w); err != nil {
		f.Close()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func(reason string) {
		once.Do(func() {
			trace.Log(context.Background(), "mailbox-trace-stop", reason)
			trace.Stop()
			closeErr := f.Close()
			data, err := json.Marshal(map[string]any{"Reason": reason, "Started": started, "Stopped": time.Now().UTC(), "LimitBytes": 64 << 20, "DiscardedBytes": w.discarded, "CloseError": closeErr})
			if err != nil {
				t.Error(err)
				return
			}
			if err := os.WriteFile(path+".json", data, 0600); err != nil {
				t.Error(err)
			}
		})
	}
	timer := time.AfterFunc(60*time.Second, func() { stop("60s capture limit") })
	t.Cleanup(func() { timer.Stop(); stop("test cleanup") })
	return stop
}

func TestMailboxTraceWriterBound(t *testing.T) {
	w := &mailboxTraceWriter{w: io.Discard, remaining: 3}
	for i := 0; i < 2; i++ {
		n, err := w.Write([]byte("12345"))
		if n != 5 || err != nil {
			t.Fatalf("write=%d %v", n, err)
		}
	}
	if w.remaining != 0 || w.discarded != 7 {
		t.Fatalf("writer=%+v", w)
	}
}

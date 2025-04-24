// Package log
// @Title  异步写入
// @Description  不同goroutine同时写入时无法保证顺序
// @Author  yr  2025/4/17
// @Update  yr  2025/4/17
package log

import (
	"bytes"
	"context"
	"github.com/njtc406/emberengine/engine/pkg/utils/mpsc"
	"io"
	"sync"
	"time"
)

const defaultBufferSize = 1024 * 1024

type AsyncWriterConfig struct {
	FlushInterval time.Duration // 刷新间隔
	BufferSize    int           // 缓冲区大小(满时会立即写入)
}

type AsyncWriter struct {
	writer io.Writer
	queue  *mpsc.Queue[[]byte]
	conf   *AsyncWriterConfig
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func fixAsyncWriterConf(conf *AsyncWriterConfig) *AsyncWriterConfig {
	if conf == nil {
		conf = &AsyncWriterConfig{}
	}
	if conf.FlushInterval == 0 {
		conf.FlushInterval = 1 * time.Second
	}
	if conf.BufferSize <= 0 {
		conf.BufferSize = defaultBufferSize
	}
	return conf
}

func NewAsyncWriter(w io.Writer, conf *AsyncWriterConfig) *AsyncWriter {
	conf = fixAsyncWriterConf(conf)
	ctx, cancel := context.WithCancel(context.Background())
	aw := &AsyncWriter{
		writer: w,
		queue:  mpsc.New[[]byte](),
		conf:   conf,
		ctx:    ctx,
		cancel: cancel,
	}

	aw.wg.Add(1)
	go aw.loop()
	return aw
}

func (aw *AsyncWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	// 拷贝一份,上层会复用buffer,导致数据发生变化
	data := make([]byte, len(p))
	copy(data, p)
	aw.queue.Push(data)
	return len(data), nil
}

func (aw *AsyncWriter) loop() {
	defer aw.wg.Done()
	ticker := time.NewTicker(aw.conf.FlushInterval)
	defer ticker.Stop()

	buf := new(bytes.Buffer)

	for {
		select {
		case <-aw.ctx.Done():
			// 循环从队列中读取数据,直到队列为空
			for aw.read(buf) {

			}
			aw.flush(buf)
			return
		case <-ticker.C:
			aw.flush(buf)
		default:
			if !aw.read(buf) {
				time.Sleep(1 * time.Millisecond) // 避免空转
			}
		}
	}
}

func (aw *AsyncWriter) read(buf *bytes.Buffer) bool {
	b, ok := aw.queue.Pop()
	if ok {
		buf.Write(b)

		if buf.Len() >= aw.conf.BufferSize {
			aw.flush(buf)
		}
	}
	return ok
}

func (aw *AsyncWriter) flush(buf *bytes.Buffer) {
	if buf.Len() == 0 {
		return
	}
	_, _ = aw.writer.Write(buf.Bytes())
	buf.Reset()
}

func (aw *AsyncWriter) Close() error {
	aw.cancel()
	aw.wg.Wait()
	return nil
}

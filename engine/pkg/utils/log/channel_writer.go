// Package log
// @Title  异步写入
// @Description  和mpsc的异步不知道谁性能会更好,所以先放这,等后续有空测试一下
// @Author  yr  2025/4/17
// @Update  yr  2025/4/17
package log

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"
)

const (
	defaultChanBufferSize = 4096
	defaultFlushSize      = 1024 * 64 // 64KB
	defaultFlushInterval  = 1 * time.Second
)

type ChannelWriterConfig struct {
	ChanBufferSize int           // channel 缓冲大小（单位：条日志）
	FlushSize      int           // 缓冲区大小，超过后立即 flush（单位：字节）
	FlushInterval  time.Duration // 定时 flush 间隔
}

type ChannelWriter struct {
	writer io.Writer
	conf   *ChannelWriterConfig

	logChan chan []byte
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func fixChannelWriterConf(conf *ChannelWriterConfig) *ChannelWriterConfig {
	if conf == nil {
		conf = &ChannelWriterConfig{}
	}
	if conf.ChanBufferSize <= 0 {
		conf.ChanBufferSize = defaultChanBufferSize
	}
	if conf.FlushSize <= 0 {
		conf.FlushSize = defaultFlushSize
	}
	if conf.FlushInterval <= 0 {
		conf.FlushInterval = defaultFlushInterval
	}
	return conf
}

func NewChannelWriter(w io.Writer, conf *ChannelWriterConfig) *ChannelWriter {
	conf = fixChannelWriterConf(conf)
	ctx, cancel := context.WithCancel(context.Background())

	cw := &ChannelWriter{
		writer:  w,
		conf:    conf,
		logChan: make(chan []byte, conf.ChanBufferSize),
		ctx:     ctx,
		cancel:  cancel,
	}
	cw.wg.Add(1)
	go cw.loop()
	return cw
}

func (cw *ChannelWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	data := make([]byte, len(p))
	copy(data, p)

	select {
	case cw.logChan <- data:
		// ok
	default:
		// 如果写入满了，就丢弃或者阻塞写入，这里选择阻塞（可改为日志告警或丢弃策略）
		cw.logChan <- data
	}
	return len(p), nil
}

func (cw *ChannelWriter) loop() {
	defer cw.wg.Done()

	buf := new(bytes.Buffer)
	ticker := time.NewTicker(cw.conf.FlushInterval)
	defer ticker.Stop()

	flush := func() {
		if buf.Len() > 0 {
			_, _ = cw.writer.Write(buf.Bytes())
			buf.Reset()
		}
	}

	for {
		select {
		case <-cw.ctx.Done():
			flush()
			return

		case <-ticker.C:
			flush()

		case msg := <-cw.logChan:
			buf.Write(msg)
			if buf.Len() >= cw.conf.FlushSize {
				flush()
			}
		}
	}
}

func (cw *ChannelWriter) Close() error {
	cw.cancel()
	cw.wg.Wait()
	return nil
}

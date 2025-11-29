package log

import "io"

type logWriter struct {
	writerMap map[Level]io.Writer
}

func newLogWriter(conf *LevelWriterConf) io.Writer {

}

func (self *logWriter) Write(p []byte) (n int, err error) {
	// TODO 看了下源码，这里已经只有format好之后的数据了,拿不到level，所以可能需要修改一下源码,在writer之前加个hook，
	// 然后根据level分离io.writter,如果没有hook，则直接使用默认的writer
	// 在entry文件的294行这里写入的，在这里修改！
}

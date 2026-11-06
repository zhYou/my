package iostreams

import (
	"io"
	"sync"
)

// Synchronize 让调用共享同一写锁；lock 为 nil 时创建独立锁。
// 包装仅暴露 Write，不同步或关闭借用流。共享原始流的回调须使用同一锁。
func Synchronize(writer io.Writer, lock *sync.Mutex) io.Writer {
	if writer == nil {
		writer = io.Discard
	}
	if lock == nil {
		lock = &sync.Mutex{}
	}
	return &serializedWriter{writer: writer, lock: lock}
}

type serializedWriter struct {
	writer io.Writer
	lock   *sync.Mutex
}

func (w *serializedWriter) Write(p []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.writer.Write(p)
}

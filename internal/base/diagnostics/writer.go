package diagnostics

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Writer 串行写入完整记录，按软阈值轮转，持有自己打开的文件。
type Writer struct {
	mu       sync.Mutex
	root     *os.Root
	name     string
	file     *os.File
	limit    int64
	size     int64
	backups  int
	sequence int
	closed   bool
	closeErr error
}

func newWriter(root *os.Root, name string, limit int64, backups int) (*Writer, error) {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Writer{root: root, name: name, file: f, limit: limit, backups: backups}, nil
}

// Write 不拆分调用方的记录；单条超大记录独占分片。
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if w.size > 0 && (w.size >= w.limit || int64(len(p)) > w.limit-w.size) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Sync 同步当前文件，关闭后返回 os.ErrClosed。
func (w *Writer) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	return w.file.Sync()
}

// Close 同步并关闭一次，重复调用返回首次结果。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.closeErr = errors.Join(w.file.Sync(), w.file.Close())
	}
	return w.closeErr
}

func (w *Writer) rotate() error {
	if err := errors.Join(w.file.Sync(), w.file.Close()); err != nil {
		w.closed = true
		w.closeErr = err
		return err
	}
	w.sequence++
	if err := w.root.Rename(w.name, w.segment(w.sequence)); err != nil {
		w.closed = true
		w.closeErr = err
		return err
	}
	f, err := w.root.OpenFile(w.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		w.closed = true
		w.closeErr = err
		return err
	}
	w.file = f
	w.size = 0
	if w.backups > 0 && w.sequence > w.backups {
		if err := w.root.Remove(w.segment(w.sequence - w.backups)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (w *Writer) segment(sequence int) string {
	return fmt.Sprintf("%s.%06d.jsonl", strings.TrimSuffix(w.name, ".jsonl"), sequence)
}

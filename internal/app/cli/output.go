package cli

import (
	"bytes"
	"encoding/json"
	"io"
)

// recordingWriter 捕获 Cobra 无返回值回调的写入失败；不拥有底层 writer。
type recordingWriter struct {
	writer io.Writer
	err    error
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

func (w *recordingWriter) Err() error {
	return w.err
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeBytes(out, append(data, '\n'))
}

func writeText(out io.Writer, render func(io.Writer) error) error {
	var buffer bytes.Buffer
	if err := render(&buffer); err != nil {
		return err
	}
	return writeBytes(out, buffer.Bytes())
}

func writeBytes(out io.Writer, data []byte) error {
	n, err := out.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

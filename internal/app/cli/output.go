package cli

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/zhYou/my/internal/base/fault"
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
		return outputFailure("render", err)
	}
	return writeBytes(out, append(data, '\n'))
}

func writeText(out io.Writer, render func(io.Writer) error) error {
	var buffer bytes.Buffer
	if err := render(&buffer); err != nil {
		return outputFailure("render", err)
	}
	return writeBytes(out, buffer.Bytes())
}

func writeBytes(out io.Writer, data []byte) error {
	n, err := out.Write(data)
	if err == nil && n != len(data) {
		return outputFailure("output", io.ErrShortWrite)
	}
	return outputFailure("output", err)
}

func outputFailure(phase string, err error) error {
	return (fault.Definition{Code: "cli.write", Kind: fault.Runtime}).Wrap(
		err,
		fault.WithDiagnostic(fault.Diagnostic{Phase: phase, Recovery: &fault.Recovery{Code: "cli.recover_output"}}),
	)
}

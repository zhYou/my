package iostreams

import (
	"io"
	"strings"
)

// IOStreams 描述调用者提供的输入、结果输出和诊断输出。
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// WithDefaults 返回补齐默认值的副本；缺省输入为 EOF，缺省输出被丢弃。
func (streams IOStreams) WithDefaults() IOStreams {
	if streams.In == nil {
		streams.In = strings.NewReader("")
	}
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.ErrOut == nil {
		streams.ErrOut = io.Discard
	}
	return streams
}

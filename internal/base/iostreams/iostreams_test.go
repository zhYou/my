package iostreams

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithDefaultsProvidesEOFAndDiscardWithoutMutatingCaller(t *testing.T) {
	original := IOStreams{}
	streams := original.WithDefaults()
	require.Equal(t, IOStreams{}, original)
	var data [1]byte
	n, err := streams.In.Read(data[:])
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
	for _, out := range []io.Writer{streams.Out, streams.ErrOut} {
		n, err := io.WriteString(out, "忽略输出")
		require.NoError(t, err)
		require.Equal(t, len("忽略输出"), n)
	}
}

func TestWithDefaultsPreservesProvidedStreams(t *testing.T) {
	in := strings.NewReader("输入")
	var out, errOut bytes.Buffer
	original := IOStreams{In: in, Out: &out, ErrOut: &errOut}
	streams := original.WithDefaults()
	require.Same(t, in, streams.In)
	require.Same(t, &out, streams.Out)
	require.Same(t, &errOut, streams.ErrOut)
	streams.Out = io.Discard
	require.Same(t, &out, original.Out)
}

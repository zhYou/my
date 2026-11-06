package command

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"time"

	"github.com/zhYou/my/internal/base/iostreams"
	"golang.org/x/sys/windows"
)

type windowsIO struct {
	handles []windows.Handle
	pipes   []*os.File
	copies  []func() error
	done    chan error
}

func prepareWindowsIO(streams iostreams.IOStreams) (result *windowsIO, err error) {
	result = &windowsIO{}
	defer func() {
		if err != nil {
			err = errors.Join(err, result.close())
		}
	}()
	input, err := result.input(streams.In)
	if err != nil {
		return result, err
	}
	result.handles = append(result.handles, input)
	output, err := result.output(streams.Out)
	if err != nil {
		return result, err
	}
	result.handles = append(result.handles, output)
	// 共用 writer 时只启用一个复制 goroutine，允许 bytes.Buffer 等非并发 writer。
	if sameWindowsWriter(streams.Out, streams.ErrOut) {
		// 白名单内每项都独立拥有；避免重复句柄关闭和列表重复值。
		stderr, duplicateErr := duplicateWindowsHandle(output)
		if duplicateErr != nil {
			return result, duplicateErr
		}
		result.handles = append(result.handles, stderr)
	} else {
		stderr, outputErr := result.output(streams.ErrOut)
		if outputErr != nil {
			return result, outputErr
		}
		result.handles = append(result.handles, stderr)
	}
	return result, nil
}

func sameWindowsWriter(a, b io.Writer) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a) != nil && reflect.TypeOf(a).Comparable() &&
		a == b
}

func duplicateWindowsHandle(source windows.Handle) (windows.Handle, error) {
	var duplicate windows.Handle
	current := windows.CurrentProcess()
	err := windows.DuplicateHandle(current, source, current, &duplicate, 0, true, windows.DUPLICATE_SAME_ACCESS)
	return duplicate, err
}

func duplicateWindowsFile(file *os.File) (windows.Handle, error) {
	conn, err := file.SyscallConn()
	if err != nil {
		return 0, err
	}
	var handle windows.Handle
	var duplicateErr error
	err = conn.Control(func(fd uintptr) {
		handle, duplicateErr = duplicateWindowsHandle(windows.Handle(fd))
	})
	return handle, errors.Join(err, duplicateErr)
}

func (streams *windowsIO) input(reader io.Reader) (windows.Handle, error) {
	if file, ok := reader.(*os.File); ok {
		return duplicateWindowsFile(file)
	}
	read, write, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	handle, err := duplicateWindowsPipeEnd(read)
	if err != nil {
		return 0, errors.Join(err, closeWindowsPipe(write))
	}
	streams.pipes = append(streams.pipes, write)
	streams.copies = append(streams.copies, func() error {
		_, copyErr := io.Copy(write, reader)
		closeErr := closeWindowsPipe(write)
		// 子进程提前关闭输入与 os/exec 的 stdin 复制语义一致。
		if errors.Is(copyErr, windows.ERROR_BROKEN_PIPE) || errors.Is(copyErr, windows.ERROR_NO_DATA) {
			return closeErr
		}
		return errors.Join(copyErr, closeErr)
	})
	return handle, nil
}

func (streams *windowsIO) output(writer io.Writer) (windows.Handle, error) {
	if file, ok := writer.(*os.File); ok {
		return duplicateWindowsFile(file)
	}
	if writer == io.Discard {
		file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return 0, err
		}
		return duplicateWindowsPipeEnd(file)
	}
	read, write, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	handle, err := duplicateWindowsPipeEnd(write)
	if err != nil {
		return 0, errors.Join(err, closeWindowsPipe(read))
	}
	streams.pipes = append(streams.pipes, read)
	streams.copies = append(streams.copies, func() error {
		_, copyErr := io.Copy(writer, read)
		return errors.Join(copyErr, closeWindowsPipe(read))
	})
	return handle, nil
}

func (streams *windowsIO) start() {
	streams.done = make(chan error, len(streams.copies))
	for _, copyStream := range streams.copies {
		go func() {
			streams.done <- copyStream()
		}()
	}
}

func (streams *windowsIO) wait(delay time.Duration) error {
	if len(streams.copies) == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var result error
	for remaining := len(streams.copies); remaining > 0; remaining-- {
		select {
		case err := <-streams.done:
			result = errors.Join(result, err)
		case <-timer.C:
			for _, pipe := range streams.pipes {
				result = errors.Join(result, closeWindowsPipe(pipe))
			}
			// 不关闭借用 reader/writer；任意永久阻塞的自定义 IO 与 os/exec 有相同限制。
			for ; remaining > 0; remaining-- {
				result = errors.Join(result, <-streams.done)
			}
			return errors.Join(result, exec.ErrWaitDelay)
		}
	}
	return result
}

func (streams *windowsIO) closeChildHandles() error {
	var problems []error
	for _, handle := range streams.handles {
		problems = append(problems, windows.CloseHandle(handle))
	}
	streams.handles = nil
	return errors.Join(problems...)
}

func (streams *windowsIO) close() error {
	problems := []error{streams.closeChildHandles()}
	for _, pipe := range streams.pipes {
		problems = append(problems, closeWindowsPipe(pipe))
	}
	return errors.Join(problems...)
}

// closeWindowsPipe 允许复制、超时和最终收尾关闭同一个自有管道；其他关闭故障仍须传播。
func closeWindowsPipe(pipe *os.File) error {
	err := pipe.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// duplicateWindowsPipeEnd 转交独立句柄后关闭原端；失败时不遗留已复制的句柄。
func duplicateWindowsPipeEnd(file *os.File) (windows.Handle, error) {
	handle, err := duplicateWindowsFile(file)
	err = errors.Join(err, file.Close())
	if err != nil {
		if handle != 0 {
			err = errors.Join(err, windows.CloseHandle(handle))
		}
		return 0, err
	}
	return handle, nil
}

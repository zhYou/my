package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcThreadAttributeValue(JobList=13, false, true, false)，Win10/Server2016 起支持。
// https://learn.microsoft.com/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute
const procThreadAttributeJobList = 0x0002000d

func createWindows(
	ctx context.Context,
	program string,
	args []string,
	options Execution,
) (child *windowsChild, result error) {
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	application, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return nil, err
	}
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, syscall.EscapeArg(program))
	for _, arg := range args {
		argv = append(argv, syscall.EscapeArg(arg))
	}
	commandLine, err := windows.UTF16PtrFromString(strings.Join(argv, " "))
	if err != nil {
		return nil, err
	}
	var directory *uint16
	if options.Dir != "" {
		directory, err = windows.UTF16PtrFromString(options.Dir)
		if err != nil {
			return nil, err
		}
	}
	environment, err := windowsEnvironment(options.Env)
	if err != nil {
		return nil, err
	}
	streams, err := prepareWindowsIO(options.IO)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, streams.close())
		}
	}()
	job, err := newWindowsJob()
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil && job != 0 {
			result = errors.Join(result, windows.CloseHandle(job))
		}
	}()
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	jobs := []windows.Handle{job}
	//nolint:gosec // G103：Win32 属性列表要求指针及精确字节长度；x/sys 和下方 KeepAlive 保证句柄切片存活到 CreateProcess 返回。
	if err = attributes.Update(
		procThreadAttributeJobList,
		unsafe.Pointer(&jobs[0]),
		unsafe.Sizeof(jobs[0]),
	); err != nil {
		return nil, err
	}
	handles := streams.handles
	//nolint:gosec // G103：Win32 属性列表要求指针及精确字节长度；x/sys 和下方 KeepAlive 保证句柄切片存活到 CreateProcess 返回。
	if err = attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&handles[0]),
		uintptr(len(handles))*unsafe.Sizeof(handles[0]),
	); err != nil {
		return nil, err
	}
	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = handles[0], handles[1], handles[2]
	startup.ProcThreadAttributeList = attributes.List()
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	var info windows.ProcessInformation
	flags := uint32(
		windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT,
	)
	err = windows.CreateProcess(
		application,
		commandLine,
		nil,
		nil,
		true,
		flags,
		&environment[0],
		directory,
		&startup.StartupInfo,
		&info,
	)
	// Update 保存的指针由 x/sys 容器保活；显式保证原生调用使用的切片寿命。
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(environment)
	if err != nil {
		return nil, err
	}
	if err := streams.closeChildHandles(); err != nil {
		ownedJob := job
		job = 0
		return nil, errors.Join(err, abortWindowsProcess(info, ownedJob))
	}
	// 独立 os.Process 持有自己的句柄，用公共 Wait 获取 ProcessState/ExitError。
	// 根进程仍挂起且原始句柄未关闭，PID 不会在重新打开期间复用。
	process, err := os.FindProcess(int(info.ProcessId))
	if err != nil {
		// 中止函数取得唯一关闭权；不能等根退出后才由 defer 触发 kill-on-close。
		ownedJob := job
		job = 0
		return nil, errors.Join(err, abortWindowsProcess(info, ownedJob))
	}
	return &windowsChild{info: info, job: job, process: process, streams: streams}, nil
}

// abortWindowsProcess 消费尚未交付的原始进程、线程和 Job 句柄。
func abortWindowsProcess(info windows.ProcessInformation, job windows.Handle) error {
	cleanupErr := terminateAndCloseWindowsJob(job)
	_, waitErr := windows.WaitForSingleObject(info.Process, windows.INFINITE)
	return errors.Join(cleanupErr, waitErr, windows.CloseHandle(info.Thread), windows.CloseHandle(info.Process))
}

func windowsEnvironment(values []string) ([]uint16, error) {
	// Environ 的公开 API 复用标准库规则：空项过滤、大小写不敏感末项覆盖、
	// 保留隐藏驱动器变量及显式空 SYSTEMROOT。非 nil Env 不读取全局环境列表。
	// Environ 隐去错误，因此先单独拒绝 NUL，禁止静默删除非法输入。
	for _, value := range values {
		if strings.IndexByte(value, 0) >= 0 {
			return nil, syscall.EINVAL
		}
	}
	environment := (&exec.Cmd{Env: append([]string{}, values...)}).Environ()
	slices.SortFunc(environment, func(a, b string) int {
		aKey, _, _ := strings.Cut(a, "=")
		bKey, _, _ := strings.Cut(b, "=")
		return strings.Compare(strings.ToUpper(aKey), strings.ToUpper(bKey))
	})
	block := make([]uint16, 0)
	for _, value := range environment {
		block = append(block, utf16.Encode([]rune(value))...)
		block = append(block, 0)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return block, nil
}

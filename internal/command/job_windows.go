package command

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func newWindowsJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	// 不允许 breakaway；nil SECURITY_ATTRIBUTES 保证 Job 句柄不可继承。
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	//nolint:gosec // G103：同步 Win32 Job API 按固定原生结构大小访问栈对象，不跨调用保存指针。
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		return 0, errors.Join(err, windows.CloseHandle(job))
	}
	return job, nil
}

// 与 Win32 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION 布局一致，amd64/arm64 均为48字节。
// https://learn.microsoft.com/windows/win32/api/winnt/ns-winnt-jobobject_basic_accounting_information
// x/sys 尚未提供此结构。
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func jobActiveProcesses(job windows.Handle) (uint32, error) {
	var info jobAccounting
	//nolint:gosec // G103：同步 Win32 Job API 按固定原生结构大小访问栈对象，不跨调用保存指针。
	err := windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	return info.ActiveProcesses, err
}

func terminateWindowsJob(job windows.Handle) error {
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return err
	}
	// TerminateJobObject 返回时退出可能仍在进行；确认所有自有进程已停止再返回。
	// 内核异常不能无界阻塞收尾；超时必须返回失败，最后关闭 Job 仍提供 kill-on-close。
	deadline := time.Now().Add(5 * time.Second)
	for {
		active, err := jobActiveProcesses(job)
		if err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return windowsJobNotEmpty.New()
		}
		time.Sleep(time.Millisecond)
	}
}

// terminateAndCloseWindowsJob 消费 Job 句柄，调用方不得再次关闭。
// 原生终止或查询失败时仍先触发 kill-on-close，根进程等待不得阻挡此兜底。
func terminateAndCloseWindowsJob(job windows.Handle) error {
	terminationErr := terminateWindowsJob(job)
	return errors.Join(terminationErr, windows.CloseHandle(job))
}

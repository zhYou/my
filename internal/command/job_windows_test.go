package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsConcurrentJobIsolation(t *testing.T) {
	sibling, opts := helper(t, "gate-output")
	siblingReady, gate := filepath.Join(opts.Dir, "ready"), filepath.Join(opts.Dir, "gate")
	sibling.Args = append(sibling.Args, siblingReady, gate, "survived")
	canceled, cancelOpts := helper(t, "ready-sleep")
	canceledReady := filepath.Join(cancelOpts.Dir, "ready")
	canceled.Args = append(canceled.Args, canceledReady, "5s", "never")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	survivorDone := startCapture(t.Context(), t, sibling, opts)
	canceledDone := startCapture(ctx, t, canceled, cancelOpts)
	waitMarker(t, siblingReady)
	waitMarker(t, canceledReady)
	cancel()
	r := awaitCapture(t, canceledDone)
	if !errors.Is(r.Err, context.Canceled) || r.Status != Canceled {
		t.Fatalf("取消结果: %+v", r)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	survivor := awaitCapture(t, survivorDone)
	if survivor.Err != nil || survivor.Status != Succeeded || string(survivor.Stdout) != "survived" {
		t.Fatalf("取消误伤并发 Job: %+v", survivor)
	}
}

// 夹具结果明确区分宿主建立、挂起时归属与实际执行结果。
type windowsHostJobResult struct {
	Stage           string `json:"stage"`
	SetupError      string `json:"setup_error"`
	LimitFlags      uint32 `json:"limit_flags"`
	ActiveLimit     uint32 `json:"active_limit"`
	UIRestrictions  uint32 `json:"ui_restrictions"`
	HelperInHost    bool   `json:"helper_in_host"`
	InitialActive   uint32 `json:"initial_active"`
	Created         bool   `json:"created"`
	ChildInHost     bool   `json:"child_in_host"`
	ChildInOwned    bool   `json:"child_in_owned"`
	OwnedActive     uint32 `json:"owned_active"`
	BeforeBusiness  bool   `json:"before_business"`
	ChildQueryError string `json:"child_query_error"`
	RunError        string `json:"run_error"`
	NativeError     uint32 `json:"native_error"`
	HasExitState    bool   `json:"has_exit_state"`
	Canceled        bool   `json:"canceled"`
	Active          uint32 `json:"active"`
	QueryError      string `json:"query_error"`
}

func TestWindowsIncompatibleHostJobRejectsBeforeBusiness(t *testing.T) {
	result, marker := runWindowsHostJobFixture(t, true)
	// 官方只保证超出活动进程配额时拒绝分配，不保证特定 GetLastError 值。
	// https://learn.microsoft.com/windows/win32/api/jobapi2/nf-jobapi2-assignprocesstojobobject
	if result.RunError == "" || result.NativeError == 0 || result.HasExitState || result.Canceled {
		t.Fatalf("未在创建时拒绝已满宿主 Job: %+v", result)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("已满宿主中执行了业务，疑似 breakaway 降级: %v", err)
	}
}

func TestWindowsUIRestrictedHostPreservesJobMembership(t *testing.T) {
	result, marker := runWindowsHostJobFixture(t, false)
	if !result.Created {
		// 兼容旧主机严格拒绝 UI 嵌套的行为；未知创建错误、业务执行或退出状态均不能通过。
		if result.NativeError != uint32(windows.ERROR_ACCESS_DENIED) || result.RunError == "" || result.HasExitState ||
			result.Canceled {
			t.Fatalf("UI 宿主创建返回未知失败: %+v", result)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("UI 宿主拒绝创建后仍执行业务: %v", err)
		}
		return
	}
	// 当前原生 Windows 允许此组合；必须在 Resume 前同时属于宿主与自有 Job。
	if result.ChildQueryError != "" || !result.ChildInHost || !result.ChildInOwned || result.OwnedActive != 1 ||
		!result.BeforeBusiness {
		t.Fatalf("创建时未满足 Job 双归属或挂起保障: %+v", result)
	}
	if result.RunError != "" || result.NativeError != 0 || result.HasExitState || result.Canceled {
		t.Fatalf("UI 宿主内正常执行失败: %+v", result)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Resume 后未执行业务: %v", err)
	}
}

func probeWindowsProcessInJob(process, job windows.Handle, member *bool) error {
	var inJob int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	//nolint:gosec // G103：同步 Win32 API 的 BOOL 输出指向有效 int32 地址。
	ok, _, err := proc.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&inJob)))
	if ok == 0 {
		return err
	}
	*member = inJob != 0
	return nil
}

// 只由独立 helper 调用，主 go test 进程永远不加入此 Job。
func windowsHostJobScenario(marker string, quota bool) (result windowsHostJobResult) {
	result.Stage = "create_host_job"
	host, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		result.SetupError = err.Error()
		return result
	}
	assigned := false
	defer func() {
		if !assigned {
			if err := windows.CloseHandle(host); err != nil {
				if result.SetupError == "" {
					result.SetupError = err.Error()
				} else {
					result.SetupError += "; " + err.Error()
				}
			}
		}
	}()
	// 显式允许 BREAKAWAY，但不设 SILENT_BREAKAWAY；错误后端若主动脱离宿主，会被归属或 marker 检出。
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK | windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if quota {
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		limits.BasicLimitInformation.ActiveProcessLimit = 1
	}
	result.Stage = "set_host_limits"
	//nolint:gosec // G103：同步 Win32 API 读取固定原生结构的有效地址。
	if _, err = windows.SetInformationJobObject(
		host,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		result.SetupError = err.Error()
		return result
	}
	restrictions := windows.JOBOBJECT_BASIC_UI_RESTRICTIONS{}
	if !quota {
		restrictions.UIRestrictionsClass = windows.JOB_OBJECT_UILIMIT_HANDLES
	}
	result.Stage = "set_ui_restrictions"
	//nolint:gosec // G103：同步 Win32 API 读取固定原生结构的有效地址。
	if _, err = windows.SetInformationJobObject(
		host,
		windows.JobObjectBasicUIRestrictions,
		uintptr(unsafe.Pointer(&restrictions)),
		uint32(unsafe.Sizeof(restrictions)),
	); err != nil {
		result.SetupError = err.Error()
		return result
	}
	result.Stage = "assign_helper_to_host"
	if err = windows.AssignProcessToJobObject(host, windows.CurrentProcess()); err != nil {
		result.SetupError = err.Error()
		return result
	}
	// 由 helper 进程寿命持有：先序列化结果再 os.Exit；提前 Close 会终止自身。
	// helper 正常退出或被外层超时终止时，内核关闭句柄并清理遗留后代。
	assigned = true
	result.Stage = "query_host_configuration"
	//nolint:gosec // G103：同步 Win32 API 读取固定原生结构的有效地址。
	if err = windows.QueryInformationJobObject(
		host,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
		nil,
	); err != nil {
		result.SetupError = err.Error()
		return result
	}
	result.LimitFlags = limits.BasicLimitInformation.LimitFlags
	result.ActiveLimit = limits.BasicLimitInformation.ActiveProcessLimit
	//nolint:gosec // G103：同步 Win32 API 读取固定原生结构的有效地址。
	if err = windows.QueryInformationJobObject(
		host,
		windows.JobObjectBasicUIRestrictions,
		uintptr(unsafe.Pointer(&restrictions)),
		uint32(unsafe.Sizeof(restrictions)),
		nil,
	); err != nil {
		result.SetupError = err.Error()
		return result
	}
	result.UIRestrictions = restrictions.UIRestrictionsClass
	if err = probeWindowsProcessInJob(windows.CurrentProcess(), host, &result.HelperInHost); err != nil {
		result.SetupError = err.Error()
		return result
	}
	result.InitialActive, err = jobActiveProcesses(host)
	if err != nil {
		result.SetupError = err.Error()
		return result
	}
	executable, err := os.Executable()
	if err != nil {
		result.SetupError = err.Error()
		return result
	}
	result.Stage = "run_backend"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	call := Invocation{
		Program: executable,
		Args:    []string{"-test.run=^TestWindowsHelper$", "--", "marker-exit", marker},
	}
	options := Execution{Dir: filepath.Dir(marker), Env: os.Environ()}
	if quota {
		// helper 已占满唯一名额。新子进程必须在创建阶段失败，不能降级或逃离宿主。
		err = Run(ctx, call, options)
	} else {
		options.IO = options.IO.WithDefaults()
		var child *windowsChild
		child, err = createWindows(ctx, call.Program, call.Args, options)
		if err == nil {
			result.Created = true
			queryErr := probeWindowsProcessInJob(child.info.Process, host, &result.ChildInHost)
			queryErr = errors.Join(
				queryErr,
				probeWindowsProcessInJob(child.info.Process, child.job, &result.ChildInOwned),
			)
			var activeErr error
			result.OwnedActive, activeErr = jobActiveProcesses(child.job)
			queryErr = errors.Join(queryErr, activeErr)
			if queryErr != nil {
				result.ChildQueryError = queryErr.Error()
			}
			//nolint:gosec // G703：独立测试 helper 仅检查父测试生成并传入的临时 marker 路径。
			_, markerErr := os.Stat(marker)
			result.BeforeBusiness = errors.Is(markerErr, os.ErrNotExist)
			if queryErr != nil || !result.ChildInHost || !result.ChildInOwned || result.OwnedActive != 1 ||
				!result.BeforeBusiness {
				cancel()
			}
			err = child.wait(ctx, DefaultWaitDelay)
		}
	}
	if err != nil {
		result.RunError = err.Error()
	}
	if native, ok := errors.AsType[syscall.Errno](err); ok {
		//nolint:gosec // G115：Win32 错误码是 DWORD，转换保留系统返回值。
		result.NativeError = uint32(native)
	}
	var exit *exec.ExitError
	result.HasExitState = errors.As(err, &exit)
	result.Canceled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	result.Active, err = jobActiveProcesses(host)
	if err != nil {
		result.QueryError = err.Error()
	}
	result.Stage = "complete"
	return result
}

func runWindowsHostJobFixture(t *testing.T, quota bool) (windowsHostJobResult, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "business-marker")
	mode := "ui"
	if quota {
		mode = "quota"
	}
	call, options := windowsHelper(t, "host-job", marker, mode)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// 原生独立 helper 构造宿主，不经过被测后端，不改主测试进程的 Job 归属。
	//nolint:gosec // G204：辅助进程仅执行自身测试二进制，路径来自当前测试。
	fixture := exec.CommandContext(ctx, call.Program, call.Args...)
	fixture.Dir, fixture.Env = options.Dir, options.Env
	fixture.WaitDelay = DefaultWaitDelay
	output, err := fixture.CombinedOutput()
	if err != nil {
		t.Fatalf("宿主夹具未完成，未覆盖该场景: %v；%s", err, output)
	}
	var result windowsHostJobResult
	if err = json.Unmarshal(output, &result); err != nil {
		t.Fatalf("夹具结果无效，未覆盖该场景: %v；%s", err, output)
	}
	t.Logf("宿主 Job 原生证据: %s", output)
	if result.SetupError != "" || result.Stage != "complete" {
		// 准备失败不能证明后端拒绝；明确失败，不 Skip。
		t.Fatalf("未覆盖宿主场景，夹具在 %s 阶段失败: %s", result.Stage, result.SetupError)
	}
	wantFlags := uint32(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK | windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
	wantUI, wantLimit := uint32(windows.JOB_OBJECT_UILIMIT_HANDLES), uint32(0)
	if quota {
		wantFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		wantUI, wantLimit = 0, 1
	}
	if result.LimitFlags != wantFlags || result.UIRestrictions != wantUI || result.ActiveLimit != wantLimit ||
		!result.HelperInHost ||
		result.InitialActive != 1 {
		t.Fatalf("宿主原生限制或初始归属不符: %+v", result)
	}
	if result.QueryError != "" || result.Active != 1 {
		t.Fatalf("收尾后宿主除 helper 外仍有活动进程: %+v", result)
	}
	return result, marker
}

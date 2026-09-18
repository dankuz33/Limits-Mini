package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func hiddenCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}
func systemExe(name string) string { return filepath.Join(os.Getenv("SystemRoot"), "System32", name) }
func makeCodexCommand(ctx context.Context, path string) (*exec.Cmd, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if strings.EqualFold(filepath.Ext(p), ".cmd") {
		if strings.ContainsAny(p, "\"%\r\n!&|<>^") {
			return nil, errors.New("Путь к codex.cmd содержит неподдерживаемые символы. Выберите codex.exe.")
		}
		cmd := hiddenCommand(ctx, systemExe("cmd.exe"))
		cmd.SysProcAttr.CmdLine = `cmd.exe /D /S /C ""` + p + `" app-server"`
		return cmd, nil
	}
	return hiddenCommand(ctx, p, "app-server"), nil
}

// Limit the lifetime of the child app-server and its descendants to this request.
// No system-wide process names are killed, and existing Codex sessions are untouched.
type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}
type ioCounters struct{ ReadOperationCount, WriteOperationCount, OtherOperationCount, ReadTransferCount, WriteTransferCount, OtherTransferCount uint64 }
type jobExtendedLimit struct {
	BasicLimitInformation                                                        jobBasicLimit
	IoInfo                                                                       ioCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

func containProcess(cmd *exec.Cmd) func() {
	k := syscall.NewLazyDLL("kernel32.dll")
	job, _, _ := k.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return func() {}
	}
	info := jobExtendedLimit{}
	info.BasicLimitInformation.LimitFlags = 0x2000 // KILL_ON_JOB_CLOSE
	ok, _, _ := k.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(job))
		return func() {}
	}
	process, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(cmd.Process.Pid))
	if err == nil {
		ok, _, _ = k.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
		syscall.CloseHandle(process)
	}
	if err != nil || ok == 0 {
		syscall.CloseHandle(syscall.Handle(job))
		return func() {}
	}
	return func() { syscall.CloseHandle(syscall.Handle(job)) }
}

// findClaude locates the native Claude Code CLI without touching Claude Desktop's
// encrypted Electron storage. Desktop and CLI intentionally use separate sign-ins.
func findClaude() string {
	h, _ := os.UserHomeDir()
	for _, name := range []string{"claude.exe", "claude.cmd"} {
		if p, e := exec.LookPath(name); e == nil && fileExists(p) {
			return p
		}
	}
	patterns := []string{
		filepath.Join(h, ".local", "bin", "claude.exe"),
		filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "claude.exe"),
	}
	for _, pat := range patterns {
		if pat != "" && fileExists(pat) {
			return pat
		}
	}
	return ""
}

// launchClaudeLogin starts Anthropic's own OAuth flow in a separate console.
// Limits Mini never receives the authorization code and never writes credentials.
func launchClaudeLogin() error {
	p := findClaude()
	if p == "" {
		return errors.New("Claude Code CLI не найден")
	}
	var cmd *exec.Cmd
	if strings.EqualFold(filepath.Ext(p), ".cmd") {
		if strings.ContainsAny(p, "\"%\r\n!&|<>^") {
			return errors.New("путь Claude Code содержит неподдерживаемые символы")
		}
		cmd = exec.Command(systemExe("cmd.exe"))
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
		cmd.SysProcAttr.CmdLine = `cmd.exe /D /K ""` + p + `" auth login"`
	} else {
		cmd = exec.Command(p, "auth", "login")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
	}
	return cmd.Start()
}

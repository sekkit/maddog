//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SetProcessGroupKill is a no-op on Windows: the Job Object that StartTracked
// assigns reaps the whole tree on close, so Setpgid (which doesn't exist here)
// is unnecessary. It exists so non-Windows callers can request group kill
// uniformly.
func SetProcessGroupKill(*exec.Cmd) {}

// KillTree terminates cmd and every descendant it spawned. Process.Kill only
// signals the direct child, so a launcher (cmd.exe → node.exe) leaves the
// grandchild alive holding the inherited stdout/stderr pipes — which makes
// cmd.Wait block forever. taskkill /T walks the live tree and kills it all.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	HideWindow(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

// TrackTree assigns cmd to a new Job Object set to terminate every process in
// it when the job handle is closed. A launcher's detached grandchild (e.g. the
// CodeGraph node daemon, which re-parents itself away from the launcher) stays
// in the job even though it leaves cmd's live child tree, so KillTracked — and,
// crucially, an abrupt maddog exit, which closes the handle — still reaps it,
// where taskkill /T would miss it. Returns 0 on failure; the caller then relies
// on KillTree alone.
func TrackTree(cmd *exec.Cmd) uintptr {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return 0
	}
	return uintptr(job)
}

// resumeProcess resumes every thread of pid. A CREATE_SUSPENDED process has a
// single suspended primary thread, so this releases it once the job is assigned.
func resumeProcess(pid uint32) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err := windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if err != nil {
			continue
		}
		_, _ = windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
	}
}

// KillTracked terminates cmd's whole process tree. When job (from StartTracked)
// is non-zero, terminating it kills even detached descendants; the KillTree pass
// then catches anything spawned in the gap before the job was assigned.
func KillTracked(cmd *exec.Cmd, job uintptr) {
	if job != 0 {
		_ = windows.TerminateJobObject(windows.Handle(job), 1)
		_ = windows.CloseHandle(windows.Handle(job))
	}
	KillTree(cmd)
}

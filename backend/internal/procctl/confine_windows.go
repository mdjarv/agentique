//go:build windows

package procctl

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// confineJob holds the one handle to the confinement job for the life of the
// process. It is deliberately never closed: with KILL_ON_JOB_CLOSE, closing the
// last handle is what kills every surviving member — including us while we are
// alive. Process exit closes it implicitly, which is the mechanism.
var confineJob windows.Handle

// ConfineProcessTree places the current process in a Windows job object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. Children — the provider CLI and, through
// it, its MCP servers, node and Chromium subtree — inherit job membership, so
// when this process exits *for any reason* (graceful shutdown, crash,
// `schtasks /End`, TerminateProcess) the kernel closes our handle table, the
// job's last handle goes with it, and every surviving member is killed.
//
// This is the Windows counterpart of the unix orphan model, with stronger
// semantics: on unix an ungracefully-killed server leaves CLI process groups
// behind for the next boot's reaper; here they cannot outlive us at all, which
// is why findCLIProcesses staying a no-op on Windows is correct rather than
// missing.
//
// Nested jobs are supported since Windows 8, so being launched inside another
// job (Task Scheduler, a terminal) does not break assignment. On failure the
// caller should log and continue: an unconfined server is exactly today's
// behaviour, and refusing to boot over a lifecycle backstop would be worse.
func ConfineProcessTree() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("set kill-on-close on job object: %w", err)
	}

	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("assign self to job object: %w", err)
	}

	confineJob = job
	return nil
}

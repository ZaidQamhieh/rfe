//go:build windows

package launcher

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// bindToLauncher puts the agent in a job object that kills it when the launcher's last handle to the job closes,
// that is, when the launcher exits for any reason, including a hard kill. Without it the agent would outlive a
// killed launcher, keep its port, and make the restarted launcher's agent fail to start (counted as a bad version).
// The handle is kept for the life of the process on purpose. Failure is not fatal: the launcher still works, it
// just cannot clean up after its own hard kill.
func bindToLauncher(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return err
	}
	return nil
}

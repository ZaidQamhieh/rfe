//! Confinement of the render process. Everything it needs arrives on stdin and leaves on stdout, so after startup it
//! needs no file system, no network and no new processes; the sandbox takes those away before the first request is
//! read, so a decoder bug cannot be turned into access to anything else.

/// What was applied, for the log.
#[derive(Debug, Default)]
pub struct Applied {
    pub notes: Vec<String>,
}

/// Address space cap for the whole process: enough for several 40-megapixel decodes at once, small enough that a
/// hostile image cannot take the machine down.
pub const MEMORY_LIMIT: u64 = 6 << 30;

/// A line on stderr that cannot stop the process. Once the file-size limit is set, stderr may be a regular file whose
/// write fails with EFBIG, and eprintln! would panic on that.
pub fn say(line: &str) {
    let _ = std::io::Write::write_fmt(&mut std::io::stderr(), format_args!("{line}\n"));
}

#[cfg(target_os = "linux")]
pub fn enter() -> Result<Applied, String> {
    use seccompiler::{
        SeccompAction, SeccompCmpArgLen, SeccompCmpOp, SeccompCondition, SeccompFilter,
        SeccompRule, TargetArch,
    };
    use std::collections::BTreeMap;

    let mut applied = Applied::default();
    let set = |res: libc::__rlimit_resource_t, v: u64| -> Result<(), String> {
        let lim = libc::rlimit {
            rlim_cur: v as libc::rlim_t,
            rlim_max: v as libc::rlim_t,
        };
        // SAFETY: plain setrlimit call with a valid pointer.
        if unsafe { libc::setrlimit(res, &lim) } != 0 {
            return Err(format!("setrlimit: {}", std::io::Error::last_os_error()));
        }
        Ok(())
    };
    set(libc::RLIMIT_CORE, 0)?;
    set(libc::RLIMIT_AS, MEMORY_LIMIT)?;
    // A write past the limit must fail with EFBIG; the default disposition kills the process with SIGXFSZ, which
    // would take a log file on stderr down with it.
    // SAFETY: signal with a constant disposition.
    unsafe { libc::signal(libc::SIGXFSZ, libc::SIG_IGN) };
    set(libc::RLIMIT_FSIZE, 0)?;
    applied
        .notes
        .push("rlimits (no core, no file writes, 6 GiB address space)".into());

    // SAFETY: prctl with constant arguments.
    if unsafe { libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) } != 0 {
        return Err(format!("no_new_privs: {}", std::io::Error::last_os_error()));
    }

    #[cfg(target_arch = "x86_64")]
    let arch = TargetArch::x86_64;
    #[cfg(target_arch = "aarch64")]
    let arch = TargetArch::aarch64;
    #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
    {
        applied
            .notes
            .push("seccomp not available on this architecture".into());
        return Ok(applied);
    }

    // Memory, time, signals, stdio and exit: the only things a render needs once its threads exist.
    let allowed: &[i64] = &[
        libc::SYS_read,
        libc::SYS_write,
        libc::SYS_close,
        libc::SYS_mmap,
        libc::SYS_munmap,
        libc::SYS_mremap,
        libc::SYS_mprotect,
        libc::SYS_madvise,
        libc::SYS_brk,
        libc::SYS_futex,
        libc::SYS_clock_gettime,
        libc::SYS_clock_nanosleep,
        libc::SYS_nanosleep,
        libc::SYS_sched_yield,
        libc::SYS_rt_sigaction,
        libc::SYS_rt_sigprocmask,
        libc::SYS_rt_sigreturn,
        libc::SYS_sigaltstack,
        libc::SYS_exit,
        libc::SYS_exit_group,
        libc::SYS_getrandom,
        libc::SYS_gettid,
        libc::SYS_getpid,
        libc::SYS_tgkill,
        libc::SYS_restart_syscall,
        // What a freshly started thread does (the JPEG decoder starts a few per image).
        libc::SYS_set_robust_list,
        libc::SYS_rseq,
        libc::SYS_set_tid_address,
        libc::SYS_sched_getaffinity,
    ];
    let mut rules: BTreeMap<i64, Vec<SeccompRule>> = allowed.iter().map(|n| (*n, vec![])).collect();
    // `clone` only creates threads: it must share the address space and thread group and join no new namespace, so
    // it can never fork a process. (`clone3` is not listed: it fails with ENOSYS and libc falls back to `clone`.)
    let thread_bits = (libc::CLONE_VM | libc::CLONE_SIGHAND | libc::CLONE_THREAD) as u64;
    let namespace_bits = (libc::CLONE_NEWNS
        | libc::CLONE_NEWCGROUP
        | libc::CLONE_NEWUTS
        | libc::CLONE_NEWIPC
        | libc::CLONE_NEWUSER
        | libc::CLONE_NEWPID
        | libc::CLONE_NEWNET) as u64;
    let cond = |mask: u64, value: u64| {
        SeccompCondition::new(
            0,
            SeccompCmpArgLen::Qword,
            SeccompCmpOp::MaskedEq(mask),
            value,
        )
    };
    let clone_rule = SeccompRule::new(vec![
        cond(thread_bits, thread_bits).map_err(|e| format!("seccomp condition: {e}"))?,
        cond(namespace_bits, 0).map_err(|e| format!("seccomp condition: {e}"))?,
    ])
    .map_err(|e| format!("seccomp rule: {e}"))?;
    rules.insert(libc::SYS_clone, vec![clone_rule]);
    let filter = SeccompFilter::new(
        rules,
        // Anything else fails with ENOSYS instead of killing the process: a libc that probes an extra call falls
        // back, and nothing that reaches the outside world is on the list.
        SeccompAction::Errno(libc::ENOSYS as u32),
        SeccompAction::Allow,
        arch,
    )
    .map_err(|e| format!("seccomp filter: {e}"))?;
    let program: seccompiler::BpfProgram = filter
        .try_into()
        .map_err(|e: seccompiler::BackendError| format!("seccomp compile: {e}"))?;
    seccompiler::apply_filter_all_threads(&program).map_err(|e| format!("seccomp apply: {e}"))?;
    applied.notes.push(format!(
        "seccomp allowlist ({} syscalls, others ENOSYS)",
        allowed.len()
    ));
    Ok(applied)
}

#[cfg(windows)]
pub fn enter() -> Result<Applied, String> {
    use windows_sys::Win32::System::JobObjects::*;
    use windows_sys::Win32::System::Threading::GetCurrentProcess;
    let mut applied = Applied::default();
    // SAFETY: documented job object calls with correctly sized, zero-initialised structures.
    unsafe {
        let job = CreateJobObjectW(std::ptr::null(), std::ptr::null());
        if job.is_null() {
            return Err("CreateJobObject failed".into());
        }
        let mut info: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
        info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_PROCESS_MEMORY
            | JOB_OBJECT_LIMIT_ACTIVE_PROCESS
            | JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
        info.BasicLimitInformation.ActiveProcessLimit = 1;
        info.ProcessMemoryLimit = MEMORY_LIMIT as usize;
        if SetInformationJobObject(
            job,
            JobObjectExtendedLimitInformation,
            &info as *const _ as *const _,
            std::mem::size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32,
        ) == 0
        {
            return Err("SetInformationJobObject failed".into());
        }
        if AssignProcessToJobObject(job, GetCurrentProcess()) == 0 {
            return Err("AssignProcessToJobObject failed (already in a job?)".into());
        }
    }
    applied
        .notes
        .push("job object (6 GiB, no child processes, kill on close)".into());
    Ok(applied)
}

#[cfg(not(any(target_os = "linux", windows)))]
pub fn enter() -> Result<Applied, String> {
    Ok(Applied {
        notes: vec![
            "no sandbox on this platform (the process still handles only stdin and stdout)".into(),
        ],
    })
}

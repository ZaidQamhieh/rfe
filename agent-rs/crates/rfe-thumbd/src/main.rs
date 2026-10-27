//! rfe-thumbd: speaks protocol v1 (protocol/sidecar.md) on stdin/stdout.
//!
//! `--no-sandbox` is for debugging only; the agent never passes it. `--sandbox-selftest` enters the sandbox, tries
//! to reach outside it and reports what was refused.

use rfe_thumbd::{sandbox, server};
use std::io::Write;

fn selftest() -> i32 {
    if let Err(e) = sandbox::enter() {
        println!("sandbox failed: {e}");
        return 1;
    }
    #[cfg(target_os = "linux")]
    {
        // SAFETY: each call is made with valid, NUL-terminated arguments and is expected to fail.
        let open = unsafe { libc::open(c"/etc/passwd".as_ptr(), libc::O_RDONLY) };
        println!(
            "open={}",
            if open < 0 {
                std::io::Error::last_os_error().raw_os_error().unwrap()
            } else {
                0
            }
        );
        let sock = unsafe { libc::socket(libc::AF_INET, libc::SOCK_STREAM, 0) };
        println!(
            "socket={}",
            if sock < 0 {
                std::io::Error::last_os_error().raw_os_error().unwrap()
            } else {
                0
            }
        );
        let argv = [c"/bin/true".as_ptr(), std::ptr::null()];
        let exec = unsafe { libc::execv(c"/bin/true".as_ptr(), argv.as_ptr()) };
        println!(
            "exec={}",
            if exec < 0 {
                std::io::Error::last_os_error().raw_os_error().unwrap()
            } else {
                0
            }
        );
        let fork = unsafe { libc::fork() };
        println!(
            "fork={}",
            if fork < 0 {
                std::io::Error::last_os_error().raw_os_error().unwrap()
            } else {
                0
            }
        );
    }
    // Rendering still works.
    let png = include_bytes!("../tests/data/pixel.png");
    match rfe_thumbd::render::render(png, 64) {
        Ok(t) => println!("render={}x{}", t.width, t.height),
        Err(_) => println!("render=failed"),
    }
    let _ = std::io::stdout().flush();
    0
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--sandbox-selftest") {
        std::process::exit(selftest());
    }
    let sandboxed = !args.iter().any(|a| a == "--no-sandbox");
    let workers = std::thread::available_parallelism()
        .map(|n| n.get())
        .unwrap_or(2)
        .min(4);
    let result = server::serve(
        std::io::stdin().lock(),
        std::io::stdout(),
        workers,
        move || {
            if sandboxed {
                let applied = sandbox::enter()?;
                sandbox::say(&format!(
                    "rfe-thumbd: sandbox: {}",
                    applied.notes.join("; ")
                ));
            }
            Ok(())
        },
    );
    if let Err(e) = result {
        eprintln!("rfe-thumbd: {e}");
        std::process::exit(1);
    }
}

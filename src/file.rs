// Copied from CedrusDB

#![allow(dead_code)]

pub(crate) use std::os::unix::io::RawFd as Fd;

use nix::errno::Errno;
use nix::fcntl::{open, openat, OFlag};
use nix::sys::stat::Mode;
use nix::unistd::{close, fsync, mkdir};

pub struct File {
    fd: Fd,
    fid: u64,
}

impl File {
    pub fn open_file(rootfd: Fd, fname: &str, truncate: bool) -> nix::Result<Fd> {
        openat(
            rootfd,
            fname,
            (if truncate { OFlag::O_TRUNC } else { OFlag::empty() }) | OFlag::O_RDWR,
            Mode::S_IRUSR | Mode::S_IWUSR,
        )
    }

    pub fn create_file(rootfd: Fd, fname: &str) -> Fd {
        openat(
            rootfd,
            fname,
            OFlag::O_CREAT | OFlag::O_RDWR,
            Mode::S_IRUSR | Mode::S_IWUSR,
        )
        .unwrap()
    }

    fn _get_fname(fid: u64) -> String {
        format!("{:08x}.fw", fid)
    }

    pub fn new(fid: u64, flen: u64, rootfd: Fd) -> nix::Result<Self> {
        let fname = Self::_get_fname(fid);
        let fd = match Self::open_file(rootfd, &fname, false) {
            Ok(fd) => fd,
            Err(e) => match e {
                Errno::ENOENT => {
                    let fd = Self::create_file(rootfd, &fname);
                    nix::unistd::ftruncate(fd, flen as nix::libc::off_t)?;
                    fd
                }
                e => return Err(e),
            },
        };
        Ok(File { fd, fid })
    }

    pub fn get_fd(&self) -> Fd {
        self.fd
    }
    pub fn get_fid(&self) -> u64 {
        self.fid
    }
    pub fn get_fname(&self) -> String {
        Self::_get_fname(self.fid)
    }

    pub fn sync(&self) {
        fsync(self.fd).unwrap();
    }
}

impl Drop for File {
    fn drop(&mut self) {
        close(self.fd).unwrap();
    }
}

pub fn touch_dir(dirname: &str, rootfd: Fd) -> Result<Fd, Errno> {
    use nix::sys::stat::mkdirat;
    if mkdirat(rootfd, dirname, Mode::S_IRUSR | Mode::S_IWUSR | Mode::S_IXUSR).is_err() {
        let errno = nix::errno::from_i32(nix::errno::errno());
        if errno != nix::errno::Errno::EEXIST {
            return Err(errno)
        }
    }
    openat(rootfd, dirname, OFlag::O_DIRECTORY | OFlag::O_PATH, Mode::empty())
}

pub fn open_dir(path: &str, truncate: bool) -> Result<(Fd, bool), nix::Error> {
    let mut reset_header = truncate;
    if truncate {
        let _ = std::fs::remove_dir_all(path);
    }
    match mkdir(path, Mode::S_IRUSR | Mode::S_IWUSR | Mode::S_IXUSR) {
        Err(e) => {
            if truncate {
                return Err(e)
            }
        }
        Ok(_) => {
            // the DB did not exist
            reset_header = true
        }
    }
    Ok((
        match open(path, OFlag::O_DIRECTORY | OFlag::O_PATH, Mode::empty()) {
            Ok(fd) => fd,
            Err(e) => return Err(e),
        },
        reset_header,
    ))
}

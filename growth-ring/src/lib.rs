//! Simple and modular write-ahead-logging implementation.
//!
//! # Examples
//!
//! ```
//! use growthring::{WALStoreAIO, wal::WALLoader};
//! use futures::executor::block_on;
//! let mut loader = WALLoader::new();
//! loader.file_nbit(9).block_nbit(8);
//!
//!
//! // Start with empty WAL (truncate = true).
//! let store = WALStoreAIO::new("./walfiles", true, None, None).unwrap();
//! let mut wal = block_on(loader.load(store, |_, _| {Ok(())}, 0)).unwrap();
//! // Write a vector of records to WAL.
//! for f in wal.grow(vec!["record1(foo)", "record2(bar)", "record3(foobar)"]).into_iter() {
//!     let ring_id = block_on(f).unwrap().1;
//!     println!("WAL recorded record to {:?}", ring_id);
//! }
//!
//!
//! // Load from WAL (truncate = false).
//! let store = WALStoreAIO::new("./walfiles", false, None, None).unwrap();
//! let mut wal = block_on(loader.load(store, |payload, ringid| {
//!     // redo the operations in your application
//!     println!("recover(payload={}, ringid={:?})",
//!              std::str::from_utf8(&payload).unwrap(),
//!              ringid);
//!     Ok(())
//! }, 0)).unwrap();
//! // We saw some log playback, even there is no failure.
//! // Let's try to grow the WAL to create many files.
//! let ring_ids = wal.grow((1..100).into_iter().map(|i| "a".repeat(i)).collect::<Vec<_>>())
//!                   .into_iter().map(|f| block_on(f).unwrap().1).collect::<Vec<_>>();
//! // Then assume all these records are not longer needed. We can tell WALWriter by the `peel`
//! // method.
//! block_on(wal.peel(ring_ids, 0)).unwrap();
//! // There will only be one remaining file in ./walfiles.
//!
//! let store = WALStoreAIO::new("./walfiles", false, None, None).unwrap();
//! let wal = block_on(loader.load(store, |payload, _| {
//!     println!("payload.len() = {}", payload.len());
//!     Ok(())
//! }, 0)).unwrap();
//! // After each recovery, the ./walfiles is empty.
//! ```

#[macro_use]
extern crate scan_fmt;
pub mod wal;

use aiofut::{AIOBuilder, AIOManager};
use async_trait::async_trait;
use libc::off_t;
#[cfg(target_os = "linux")]
use nix::fcntl::{fallocate, open, openat, FallocateFlags, OFlag};
#[cfg(not(target_os = "linux"))]
use nix::fcntl::{open, openat, OFlag};
use nix::sys::stat::Mode;
use nix::unistd::{close, ftruncate, mkdir, unlinkat, UnlinkatFlags};
use std::os::unix::io::RawFd;
use std::sync::Arc;
use wal::{WALBytes, WALFile, WALPos, WALStore};

pub struct WALFileAIO {
    fd: RawFd,
    aiomgr: Arc<AIOManager>,
}

impl WALFileAIO {
    pub fn new(rootfd: RawFd, filename: &str, aiomgr: Arc<AIOManager>) -> Result<Self, ()> {
        openat(
            rootfd,
            filename,
            OFlag::O_CREAT | OFlag::O_RDWR,
            Mode::S_IRUSR | Mode::S_IWUSR,
        )
        .and_then(|fd| Ok(WALFileAIO { fd, aiomgr }))
        .or_else(|_| Err(()))
    }
}

impl Drop for WALFileAIO {
    fn drop(&mut self) {
        close(self.fd).unwrap();
    }
}

#[async_trait(?Send)]
impl WALFile for WALFileAIO {
    #[cfg(target_os = "linux")]
    async fn allocate(&self, offset: WALPos, length: usize) -> Result<(), ()> {
        // TODO: is there any async version of fallocate?
        return fallocate(
            self.fd,
            FallocateFlags::FALLOC_FL_ZERO_RANGE,
            offset as off_t,
            length as off_t,
        )
        .and_then(|_| Ok(()))
        .or_else(|_| Err(()));
    }
    #[cfg(not(target_os = "linux"))]
    // TODO: macos support is possible here, but possibly unnecessary
    async fn allocate(&self, _offset: WALPos, _length: usize) -> Result<(), ()> {
        Ok(())
    }

    fn truncate(&self, length: usize) -> Result<(), ()> {
        ftruncate(self.fd, length as off_t).or_else(|_| Err(()))
    }

    async fn write(&self, offset: WALPos, data: WALBytes) -> Result<(), ()> {
        let (res, data) = self.aiomgr.write(self.fd, offset, data, None).await;
        res.or_else(|_| Err(())).and_then(|nwrote| {
            if nwrote == data.len() {
                Ok(())
            } else {
                Err(())
            }
        })
    }

    async fn read(&self, offset: WALPos, length: usize) -> Result<Option<WALBytes>, ()> {
        let (res, data) = self.aiomgr.read(self.fd, offset, length, None).await;
        res.or_else(|_| Err(()))
            .and_then(|nread| Ok(if nread == length { Some(data) } else { None }))
    }
}

pub struct WALStoreAIO {
    rootfd: RawFd,
    aiomgr: Arc<AIOManager>,
}

unsafe impl Send for WALStoreAIO {}

impl WALStoreAIO {
    pub fn new(
        wal_dir: &str,
        truncate: bool,
        rootfd: Option<RawFd>,
        aiomgr: Option<AIOManager>,
    ) -> Result<Self, ()> {
        let aiomgr = Arc::new(
            aiomgr
                .ok_or(Err(()))
                .or_else(|_: Result<AIOManager, ()>| AIOBuilder::default().build().or(Err(())))?,
        );

        if truncate {
            let _ = std::fs::remove_dir_all(wal_dir);
        }
        let walfd;
        match rootfd {
            None => {
                match mkdir(wal_dir, Mode::S_IRUSR | Mode::S_IWUSR | Mode::S_IXUSR) {
                    Err(e) => {
                        if truncate {
                            panic!("error while creating directory: {}", e)
                        }
                    }
                    Ok(_) => (),
                }
                walfd = match open(wal_dir, oflags(), Mode::empty()) {
                    Ok(fd) => fd,
                    Err(_) => panic!("error while opening the WAL directory"),
                }
            }
            Some(fd) => {
                let dirstr = std::ffi::CString::new(wal_dir).unwrap();
                let ret = unsafe {
                    libc::mkdirat(
                        fd,
                        dirstr.as_ptr(),
                        libc::S_IRUSR | libc::S_IWUSR | libc::S_IXUSR,
                    )
                };
                if ret != 0 {
                    if truncate {
                        panic!("error while creating directory")
                    }
                }
                walfd = match nix::fcntl::openat(fd, wal_dir, oflags(), Mode::empty()) {
                    Ok(fd) => fd,
                    Err(_) => panic!("error while opening the WAL directory"),
                }
            }
        }
        Ok(WALStoreAIO {
            rootfd: walfd,
            aiomgr,
        })
    }
}

/// Return OS specific open flags for opening files
/// TODO: Switch to a rust idiomatic directory scanning approach
/// TODO: This shouldn't need to escape growth-ring (no pub)
pub fn oflags() -> OFlag {
    #[cfg(target_os = "linux")]
    return OFlag::O_DIRECTORY | OFlag::O_PATH;
    #[cfg(not(target_os = "linux"))]
    return OFlag::O_DIRECTORY;
}

#[async_trait(?Send)]
impl WALStore for WALStoreAIO {
    type FileNameIter = std::vec::IntoIter<String>;

    async fn open_file(&self, filename: &str, _touch: bool) -> Result<Box<dyn WALFile>, ()> {
        let filename = filename.to_string();
        WALFileAIO::new(self.rootfd, &filename, self.aiomgr.clone())
            .and_then(|f| Ok(Box::new(f) as Box<dyn WALFile>))
    }

    async fn remove_file(&self, filename: String) -> Result<(), ()> {
        unlinkat(
            Some(self.rootfd),
            filename.as_str(),
            UnlinkatFlags::NoRemoveDir,
        )
        .or_else(|_| Err(()))
    }

    fn enumerate_files(&self) -> Result<Self::FileNameIter, ()> {
        let mut logfiles = Vec::new();
        for ent in nix::dir::Dir::openat(self.rootfd, "./", OFlag::empty(), Mode::empty())
            .unwrap()
            .iter()
        {
            logfiles.push(ent.unwrap().file_name().to_str().unwrap().to_string())
        }
        Ok(logfiles.into_iter())
    }
}

impl Drop for WALStoreAIO {
    fn drop(&mut self) {
        nix::unistd::close(self.rootfd).ok();
    }
}

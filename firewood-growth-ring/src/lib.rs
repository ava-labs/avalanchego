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
//! let store = WALStoreAIO::new("./walfiles", true, None).unwrap();
//! let mut wal = block_on(loader.load(store, |_, _| {Ok(())}, 0)).unwrap();
//! // Write a vector of records to WAL.
//! for f in wal.grow(vec!["record1(foo)", "record2(bar)", "record3(foobar)"]).into_iter() {
//!     let ring_id = block_on(f).unwrap().1;
//!     println!("WAL recorded record to {:?}", ring_id);
//! }
//!
//!
//! // Load from WAL (truncate = false).
//! let store = WALStoreAIO::new("./walfiles", false, None).unwrap();
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
//! let store = WALStoreAIO::new("./walfiles", false, None).unwrap();
//! let wal = block_on(loader.load(store, |payload, _| {
//!     println!("payload.len() = {}", payload.len());
//!     Ok(())
//! }, 0)).unwrap();
//! // After each recovery, the ./walfiles is empty.
//! ```

#[macro_use]
extern crate scan_fmt;
pub mod wal;
pub mod walerror;

use async_trait::async_trait;
use firewood_libaio::{AIOBuilder, AIOManager};
use libc::off_t;
#[cfg(target_os = "linux")]
use nix::fcntl::{fallocate, FallocateFlags, OFlag};
#[cfg(not(target_os = "linux"))]
use nix::fcntl::{open, openat, OFlag};
use nix::unistd::{close, ftruncate};
use std::fs;
use std::os::fd::IntoRawFd;
use std::os::unix::io::RawFd;
use std::os::unix::prelude::OpenOptionsExt;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use wal::{WALBytes, WALFile, WALPos, WALStore};
use walerror::WALError;

pub struct WALFileAIO {
    fd: RawFd,
    aiomgr: Arc<AIOManager>,
}

impl WALFileAIO {
    pub fn new(root_dir: &Path, filename: &str, aiomgr: Arc<AIOManager>) -> Result<Self, WALError> {
        fs::OpenOptions::new()
            .read(true)
            .write(true)
            .truncate(false)
            .create(true)
            .mode(0o600)
            .open(root_dir.join(filename))
            .map(|f| {
                let fd = f.into_raw_fd();
                WALFileAIO { fd, aiomgr }
            })
            .map_err(|e| WALError::IOError(Arc::new(e)))
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
    async fn allocate(&self, offset: WALPos, length: usize) -> Result<(), WALError> {
        // TODO: is there any async version of fallocate?
        return fallocate(
            self.fd,
            FallocateFlags::FALLOC_FL_ZERO_RANGE,
            offset as off_t,
            length as off_t,
        )
        .map(|_| ())
        .map_err(Into::into);
    }
    #[cfg(not(target_os = "linux"))]
    // TODO: macos support is possible here, but possibly unnecessary
    async fn allocate(&self, _offset: WALPos, _length: usize) -> Result<(), WALError> {
        Ok(())
    }

    async fn truncate(&self, length: usize) -> Result<(), WALError> {
        ftruncate(self.fd, length as off_t).map_err(From::from)
    }

    async fn write(&self, offset: WALPos, data: WALBytes) -> Result<(), WALError> {
        let (res, data) = self.aiomgr.write(self.fd, offset, data, None).await;
        res.map_err(Into::into).and_then(|nwrote| {
            if nwrote == data.len() {
                Ok(())
            } else {
                Err(WALError::Other(format!(
                    "partial write; wrote {nwrote} expected {} for fd {}",
                    data.len(),
                    self.fd
                )))
            }
        })
    }

    async fn read(&self, offset: WALPos, length: usize) -> Result<Option<WALBytes>, WALError> {
        let (res, data) = self.aiomgr.read(self.fd, offset, length, None).await;
        res.map_err(From::from)
            .map(|nread| if nread == length { Some(data) } else { None })
    }
}

pub struct WALStoreAIO {
    root_dir: PathBuf,
    aiomgr: Arc<AIOManager>,
}

unsafe impl Send for WALStoreAIO {}

impl WALStoreAIO {
    pub fn new<P: AsRef<Path>>(
        wal_dir: P,
        truncate: bool,
        aiomgr: Option<AIOManager>,
    ) -> Result<Self, WALError> {
        let aio = match aiomgr {
            Some(aiomgr) => Arc::new(aiomgr),
            None => Arc::new(AIOBuilder::default().build()?),
        };

        if truncate {
            if let Err(e) = fs::remove_dir_all(&wal_dir) {
                if e.kind() != std::io::ErrorKind::NotFound {
                    return Err(From::from(e));
                }
            }
            fs::create_dir(&wal_dir)?;
        } else if !wal_dir.as_ref().exists() {
            // create WAL dir
            fs::create_dir(&wal_dir)?;
        }

        Ok(WALStoreAIO {
            root_dir: wal_dir.as_ref().to_path_buf(),
            aiomgr: aio,
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

    async fn open_file(&self, filename: &str, _touch: bool) -> Result<Box<dyn WALFile>, WALError> {
        WALFileAIO::new(&self.root_dir, filename, self.aiomgr.clone())
            .map(|f| Box::new(f) as Box<dyn WALFile>)
    }

    async fn remove_file(&self, filename: String) -> Result<(), WALError> {
        let file_to_remove = self.root_dir.join(filename);
        fs::remove_file(file_to_remove).map_err(From::from)
    }

    fn enumerate_files(&self) -> Result<Self::FileNameIter, WALError> {
        let mut filenames = Vec::new();
        for path in fs::read_dir(&self.root_dir)?.filter_map(|entry| entry.ok()) {
            filenames.push(path.file_name().into_string().unwrap());
        }
        Ok(filenames.into_iter())
    }
}

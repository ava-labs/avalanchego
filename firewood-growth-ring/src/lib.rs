//! Simple and modular write-ahead-logging implementation.
//!
//! # Examples
//!
//! ```no_run
//! use growthring::{WalStoreAio, wal::WalLoader};
//! use futures::executor::block_on;
//! let mut loader = WalLoader::new();
//! loader.file_nbit(9).block_nbit(8);
//!
//!
//! // Start with empty WAL (truncate = true).
//! let store = WalStoreAio::new("/tmp/walfiles", true, None).unwrap();
//! let mut wal = block_on(loader.load(store, |_, _| {Ok(())}, 0)).unwrap();
//! // Write a vector of records to WAL.
//! for f in wal.grow(vec!["record1(foo)", "record2(bar)", "record3(foobar)"]).into_iter() {
//!     let ring_id = block_on(f).unwrap().1;
//!     println!("WAL recorded record to {:?}", ring_id);
//! }
//!
//!
//! // Load from WAL (truncate = false).
//! let store = WalStoreAio::new("/tmp/walfiles", false, None).unwrap();
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
//! // Then assume all these records are not longer needed. We can tell WalWriter by the `peel`
//! // method.
//! block_on(wal.peel(ring_ids, 0)).unwrap();
//! // There will only be one remaining file in /tmp/walfiles.
//!
//! let store = WalStoreAio::new("/tmp/walfiles", false, None).unwrap();
//! let wal = block_on(loader.load(store, |payload, _| {
//!     println!("payload.len() = {}", payload.len());
//!     Ok(())
//! }, 0)).unwrap();
//! // After each recovery, the /tmp/walfiles is empty.
//! ```

#[macro_use]
extern crate scan_fmt;
pub mod wal;
pub mod walerror;

use async_trait::async_trait;
use firewood_libaio::{AioBuilder, AioManager};
use nix::fcntl::OFlag;
use std::fs;
use std::os::fd::AsRawFd;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use tokio::fs::{File, OpenOptions};
use wal::{WalBytes, WalFile, WalPos, WalStore};
use walerror::WalError;

pub struct WalFileAio {
    file: File,
    aio_manager: Arc<AioManager>,
}

impl WalFileAio {
    async fn open_file<P: AsRef<Path>>(path: P) -> Result<File, std::io::Error> {
        OpenOptions::new()
            .read(true)
            .write(true)
            .truncate(false)
            .create(true)
            .mode(0o600)
            .open(path)
            .await
    }

    fn new(file: File, aio_manager: Arc<AioManager>) -> Self {
        Self { file, aio_manager }
    }
}

#[async_trait(?Send)]
impl WalFile for WalFileAio {
    async fn allocate(&self, offset: WalPos, length: usize) -> Result<(), WalError> {
        self.file
            .set_len(offset + length as u64)
            .await
            .map_err(Into::into)
    }

    async fn truncate(&self, len: usize) -> Result<(), WalError> {
        self.file.set_len(len as u64).await.map_err(Into::into)
    }

    async fn write(&self, offset: WalPos, data: WalBytes) -> Result<(), WalError> {
        let fd = self.file.as_raw_fd();
        let (res, data) = self.aio_manager.write(fd, offset, data, None).await;
        res.map_err(Into::into).and_then(|nwrote| {
            if nwrote == data.len() {
                Ok(())
            } else {
                Err(WalError::Other(format!(
                    "partial write; wrote {nwrote} expected {} for fd {}",
                    data.len(),
                    fd
                )))
            }
        })
    }

    async fn read(&self, offset: WalPos, length: usize) -> Result<Option<WalBytes>, WalError> {
        let fd = self.file.as_raw_fd();
        let (res, data) = self.aio_manager.read(fd, offset, length, None).await;
        res.map_err(From::from)
            .map(|nread| if nread == length { Some(data) } else { None })
    }
}

pub struct WalStoreAio {
    root_dir: PathBuf,
    aiomgr: Arc<AioManager>,
}

unsafe impl Send for WalStoreAio {}

impl WalStoreAio {
    pub fn new<P: AsRef<Path>>(
        wal_dir: P,
        truncate: bool,
        aiomgr: Option<AioManager>,
    ) -> Result<Self, WalError> {
        let aio = match aiomgr {
            Some(aiomgr) => Arc::new(aiomgr),
            None => Arc::new(AioBuilder::default().build()?),
        };

        if truncate {
            if let Err(e) = fs::remove_dir_all(&wal_dir) {
                if e.kind() != std::io::ErrorKind::NotFound {
                    return Err(From::from(e));
                }
            }
            fs::create_dir(&wal_dir)?;
        } else if !wal_dir.as_ref().exists() {
            // create Wal dir
            fs::create_dir(&wal_dir)?;
        }

        Ok(WalStoreAio {
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
impl WalStore for WalStoreAio {
    type FileNameIter = std::vec::IntoIter<String>;

    async fn open_file(&self, filename: &str, _touch: bool) -> Result<Box<dyn WalFile>, WalError> {
        let path = self.root_dir.join(filename);

        let file = WalFileAio::open_file(path).await?;

        Ok(Box::new(WalFileAio::new(file, self.aiomgr.clone())))
    }

    async fn remove_file(&self, filename: String) -> Result<(), WalError> {
        let file_to_remove = self.root_dir.join(filename);
        fs::remove_file(file_to_remove).map_err(From::from)
    }

    fn enumerate_files(&self) -> Result<Self::FileNameIter, WalError> {
        let mut filenames = Vec::new();
        for path in fs::read_dir(&self.root_dir)?.filter_map(|entry| entry.ok()) {
            filenames.push(path.file_name().into_string().unwrap());
        }
        Ok(filenames.into_iter())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn truncation_makes_a_file_smaller() {
        const HALF_LENGTH: usize = 512;

        let walfile_path = get_walfile_path(file!(), line!());

        tokio::fs::remove_file(&walfile_path).await.ok();

        let aio_manager = AioBuilder::default().build().unwrap();

        let walfile = WalFileAio::open_file(walfile_path).await.unwrap();

        let walfile_aio = WalFileAio::new(walfile, Arc::new(aio_manager));

        let first_half = vec![1u8; HALF_LENGTH];
        let second_half = vec![2u8; HALF_LENGTH];

        let data = first_half
            .iter()
            .copied()
            .chain(second_half.iter().copied())
            .collect();

        walfile_aio.write(0, data).await.unwrap();
        walfile_aio.truncate(HALF_LENGTH).await.unwrap();

        let result = walfile_aio.read(0, HALF_LENGTH).await.unwrap();

        assert_eq!(result, Some(first_half.into()))
    }

    #[tokio::test]
    async fn truncation_extends_a_file_with_zeros() {
        const LENGTH: usize = 512;

        let walfile_path = get_walfile_path(file!(), line!());

        tokio::fs::remove_file(&walfile_path).await.ok();

        let aio_manager = AioBuilder::default().build().unwrap();

        let walfile = WalFileAio::open_file(walfile_path).await.unwrap();

        let walfile_aio = WalFileAio::new(walfile, Arc::new(aio_manager));

        walfile_aio
            .write(0, vec![1u8; LENGTH].into())
            .await
            .unwrap();

        walfile_aio.truncate(2 * LENGTH).await.unwrap();

        let result = walfile_aio.read(LENGTH as u64, LENGTH).await.unwrap();

        assert_eq!(result, Some(vec![0u8; LENGTH].into()))
    }

    fn get_walfile_path(file: &str, line: u32) -> PathBuf {
        Path::new("/tmp").join(format!("{}_{}", file.replace('/', "-"), line))
    }
}

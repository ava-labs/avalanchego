// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Copied from CedrusDB

use std::fs::{create_dir, remove_dir_all};
use std::ops::Deref;
use std::os::fd::{AsRawFd, OwnedFd};

use std::path::{Path, PathBuf};
use std::{io::ErrorKind, os::unix::prelude::OpenOptionsExt};

use nix::fcntl::Flockable;

pub struct File {
    fd: OwnedFd,
}

impl AsRawFd for File {
    fn as_raw_fd(&self) -> std::os::unix::prelude::RawFd {
        self.fd.as_raw_fd()
    }
}

// SAFETY: Docs for Flockable say it's safe if T is not Clone,
// and File is not clone
unsafe impl Flockable for File {}

#[derive(PartialEq, Eq)]
pub enum Options {
    Truncate,
    NoTruncate,
}

impl File {
    pub fn open_file(
        rootpath: PathBuf,
        fname: &str,
        options: Options,
    ) -> Result<OwnedFd, std::io::Error> {
        let mut filepath = rootpath;
        filepath.push(fname);
        Ok(std::fs::File::options()
            .truncate(options == Options::Truncate)
            .read(true)
            .write(true)
            .mode(0o600)
            .open(filepath)?
            .into())
    }

    pub fn create_file(rootpath: PathBuf, fname: &str) -> Result<OwnedFd, std::io::Error> {
        let mut filepath = rootpath;
        filepath.push(fname);
        Ok(std::fs::File::options()
            .create(true)
            .truncate(true)
            .read(true)
            .write(true)
            .mode(0o600)
            .open(filepath)?
            .into())
    }

    fn _get_fname(fid: u64) -> String {
        format!("{fid:08x}.fw")
    }

    pub fn new<P: AsRef<Path>>(fid: u64, _flen: u64, rootdir: P) -> Result<Self, std::io::Error> {
        let fname = Self::_get_fname(fid);
        let fd = match Self::open_file(rootdir.as_ref().to_path_buf(), &fname, Options::NoTruncate)
        {
            Ok(fd) => fd,
            Err(e) => match e.kind() {
                ErrorKind::NotFound => Self::create_file(rootdir.as_ref().to_path_buf(), &fname)?,
                _ => return Err(e),
            },
        };
        Ok(File { fd })
    }
}

impl Deref for File {
    type Target = OwnedFd;

    fn deref(&self) -> &Self::Target {
        &self.fd
    }
}

pub(crate) fn touch_dir(dirname: &str, rootdir: &Path) -> Result<PathBuf, std::io::Error> {
    let path = rootdir.join(dirname);
    if let Err(e) = std::fs::create_dir(&path) {
        // ignore already-exists error
        if e.kind() != ErrorKind::AlreadyExists {
            return Err(e);
        }
    }
    Ok(path)
}

pub(crate) fn open_dir<P: AsRef<Path>>(
    path: P,
    options: Options,
) -> Result<(PathBuf, bool), std::io::Error> {
    let truncate = options == Options::Truncate;

    if truncate {
        let _ = remove_dir_all(path.as_ref());
    }

    match create_dir(path.as_ref()) {
        Err(e) if truncate || e.kind() != ErrorKind::AlreadyExists => Err(e),
        // the DB already exists
        Err(_) => Ok((path.as_ref().to_path_buf(), false)),
        Ok(_) => Ok((path.as_ref().to_path_buf(), true)),
    }
}

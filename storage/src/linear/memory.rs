// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 3 occurrences after enabling the lint."
)]
#![expect(
    clippy::indexing_slicing,
    reason = "Found 1 occurrences after enabling the lint."
)]

use super::{FileIoError, OffsetReader, ReadableStorage, WritableStorage};
use metrics::counter;
use std::io::Cursor;
use std::sync::Mutex;

#[derive(Debug, Default)]
/// An in-memory impelementation of [`WritableStorage`] and [`ReadableStorage`]
pub struct MemStore {
    bytes: Mutex<Vec<u8>>,
}

impl MemStore {
    /// Create a new, empty [`MemStore`]
    #[must_use]
    pub const fn new(bytes: Vec<u8>) -> Self {
        Self {
            bytes: Mutex::new(bytes),
        }
    }
}

impl WritableStorage for MemStore {
    fn write(&self, offset: u64, object: &[u8]) -> Result<usize, FileIoError> {
        let offset = offset as usize;
        let mut guard = self.bytes.lock().expect("poisoned lock");
        if offset + object.len() > guard.len() {
            guard.resize(offset + object.len(), 0);
        }
        guard[offset..offset + object.len()].copy_from_slice(object);
        Ok(object.len())
    }
}

impl ReadableStorage for MemStore {
    fn stream_from(&self, addr: u64) -> Result<Box<dyn OffsetReader>, FileIoError> {
        counter!("firewood.read_node", "from" => "memory").increment(1);
        let bytes = self
            .bytes
            .lock()
            .expect("poisoned lock")
            .get(addr as usize..)
            .unwrap_or_default()
            .to_owned();

        Ok(Box::new(Cursor::new(bytes)))
    }

    fn size(&self) -> Result<u64, FileIoError> {
        Ok(self.bytes.lock().expect("poisoned lock").len() as u64)
    }
}

#[expect(clippy::unwrap_used)]
#[cfg(test)]
mod test {
    use super::*;
    use test_case::test_case;

    #[test_case(&[(0,&[1, 2, 3])],(0,&[1, 2, 3]); "write to empty store")]
    #[test_case(&[(0,&[1, 2, 3])],(1,&[2, 3]); "read from middle of store")]
    #[test_case(&[(0,&[1, 2, 3])],(2,&[3]); "read from end of store")]
    #[test_case(&[(0,&[1, 2, 3])],(3,&[]); "read past end of store")]
    #[test_case(&[(0,&[1, 2, 3]),(3,&[4,5,6])],(0,&[1, 2, 3,4,5,6]); "write to end of store")]
    #[test_case(&[(0,&[1, 2, 3]),(0,&[4])],(0,&[4,2,3]); "overwrite start of store")]
    #[test_case(&[(0,&[1, 2, 3]),(1,&[4])],(0,&[1,4,3]); "overwrite middle of store")]
    #[test_case(&[(0,&[1, 2, 3]),(2,&[4])],(0,&[1,2,4]); "overwrite end of store")]
    #[test_case(&[(0,&[1, 2, 3]),(2,&[4,5])],(0,&[1,2,4,5]); "overwrite/extend end of store")]
    fn test_in_mem_write_linear_store(writes: &[(u64, &[u8])], expected: (u64, &[u8])) {
        let store = MemStore {
            bytes: Mutex::new(vec![]),
        };
        assert_eq!(store.size().unwrap(), 0);

        for write in writes {
            store.write(write.0, write.1).unwrap();
        }

        let mut reader = store.stream_from(expected.0).unwrap();
        let mut read_bytes = vec![];
        reader.read_to_end(&mut read_bytes).unwrap();
        assert_eq!(read_bytes, expected.1);
    }
}

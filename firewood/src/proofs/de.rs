// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(feature = "ethhash")]
use firewood_storage::HashType;
use firewood_storage::{Children, TrieHash, ValueDigest};
use integer_encoding::VarInt;

use crate::{
    proof::{Proof, ProofNode},
    proofs::{
        bitmap::ChildrenMap,
        header::{Header, InvalidHeader},
        proof_type::ProofType,
        reader::{ProofReader, ReadError, ReadItem, V0Reader, Version0},
    },
    v2::api::FrozenRangeProof,
};

impl FrozenRangeProof {
    /// Parses a `FrozenRangeProof` from the given byte slice.
    ///
    /// Currently only V0 proofs are supported. See [`FrozenRangeProof::write_to_vec`]
    /// for the serialization format.
    ///
    /// # Errors
    ///
    /// Returns a [`ReadError`] if the data is invalid. See the enum variants for
    /// the possible reasons.
    pub fn from_slice(data: &[u8]) -> Result<Self, ReadError> {
        let mut reader = ProofReader::new(data);

        let header = reader.read_item::<Header>()?;
        header
            .validate(Some(ProofType::Range))
            .map_err(ReadError::InvalidHeader)?;

        match header.version {
            0 => {
                let mut reader = V0Reader::new(reader, header);
                let this = reader.read_v0_item()?;
                if reader.remainder().is_empty() {
                    Ok(this)
                } else {
                    Err(reader.invalid_item(
                        "trailing bytes",
                        "no data after the proof",
                        format!("{} bytes", reader.remainder().len()),
                    ))
                }
            }
            found => Err(ReadError::InvalidHeader(
                InvalidHeader::UnsupportedVersion { found },
            )),
        }
    }
}

impl<T: Version0> Version0 for Box<[T]> {
    fn read_v0_item(reader: &mut V0Reader<'_>) -> Result<Self, ReadError> {
        let num_items = reader
            .read_item::<usize>()
            .map_err(|err| err.set_item("array length"))?;

        // FIXME: we must somehow validate `num_items` matches what is expected
        // An incorrect, or unexpectedly large value could lead to DoS via OOM
        // or panicing
        (0..num_items).map(|_| reader.read_v0_item()).collect()
    }
}

impl Version0 for FrozenRangeProof {
    fn read_v0_item(reader: &mut V0Reader<'_>) -> Result<Self, ReadError> {
        let start_proof = reader.read_v0_item()?;
        let end_proof = reader.read_v0_item()?;
        let key_values = reader.read_v0_item()?;

        Ok(Self::new(
            Proof::new(start_proof),
            Proof::new(end_proof),
            key_values,
        ))
    }
}

impl Version0 for ProofNode {
    fn read_v0_item(reader: &mut V0Reader<'_>) -> Result<Self, ReadError> {
        let key = reader.read_item()?;
        let partial_len = reader.read_item()?;
        let value_digest = reader.read_item()?;

        let children_map = reader.read_item::<ChildrenMap>()?;

        let mut child_hashes = Children::new();
        for idx in children_map.iter_indices() {
            child_hashes[idx] = Some(reader.read_item()?);
        }

        Ok(ProofNode {
            key,
            partial_len,
            value_digest,
            child_hashes,
        })
    }
}

impl Version0 for (Box<[u8]>, Box<[u8]>) {
    fn read_v0_item(reader: &mut V0Reader<'_>) -> Result<Self, ReadError> {
        Ok((reader.read_item()?, reader.read_item()?))
    }
}

impl<'a> ReadItem<'a> for Header {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader
            .read_chunk::<{ size_of::<Header>() }>()
            .map_err(|err| err.set_item("header"))
            .copied()
            .map(bytemuck::cast)
    }
}

impl<'a> ReadItem<'a> for usize {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        match u64::decode_var(reader.remainder()) {
            Some((n, size)) => {
                reader.advance(size);
                Ok(n as usize)
            }
            None if reader.remainder().is_empty() => Err(reader.incomplete_item("varint", 1)),
            #[expect(clippy::indexing_slicing)]
            None => Err(reader.invalid_item(
                "varint",
                "byte with no MSB within 9 bytes",
                format!(
                    "{:?}",
                    &reader.remainder()[..reader.remainder().len().min(10)]
                ),
            )),
        }
    }
}

impl<'a> ReadItem<'a> for &'a [u8] {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        let len = reader.read_item::<usize>()?;
        reader.read_slice(len)
    }
}

impl<'a> ReadItem<'a> for Box<[u8]> {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader.read_item::<&[u8]>().map(Box::from)
    }
}

impl<'a> ReadItem<'a> for u8 {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader
            .read_chunk::<1>()
            .map(|&[b]| b)
            .map_err(|err| err.set_item("u8"))
    }
}

impl<'a, T: ReadItem<'a>> ReadItem<'a> for Option<T> {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        match reader
            .read_item::<u8>()
            .map_err(|err| err.set_item("option discriminant"))?
        {
            0 => Ok(None),
            1 => Ok(Some(reader.read_item::<T>()?)),
            found => Err(reader.invalid_item("option discriminant", "0 or 1", found)),
        }
    }
}

impl<'a> ReadItem<'a> for ValueDigest<&'a [u8]> {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        match reader
            .read_item::<u8>()
            .map_err(|err| err.set_item("value digest discriminant"))?
        {
            0 => Ok(ValueDigest::Value(reader.read_item()?)),
            #[cfg(not(feature = "ethhash"))]
            1 => Ok(ValueDigest::Hash(reader.read_item()?)),
            found => Err(reader.invalid_item(
                "value digest discriminant",
                "0 (value) or 1 (hash)",
                found,
            )),
        }
    }
}

impl<'a> ReadItem<'a> for ValueDigest<Box<[u8]>> {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader.read_item::<ValueDigest<&[u8]>>().map(|vd| match vd {
            ValueDigest::Value(v) => ValueDigest::Value(v.into()),
            #[cfg(not(feature = "ethhash"))]
            ValueDigest::Hash(h) => ValueDigest::Hash(h),
        })
    }
}

impl<'a> ReadItem<'a> for TrieHash {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader
            .read_chunk::<{ size_of::<TrieHash>() }>()
            .map_err(|err| err.set_item("trie hash"))
            .copied()
            .map(TrieHash::from)
    }
}

impl<'a> ReadItem<'a> for ChildrenMap {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        reader
            .read_chunk::<{ size_of::<ChildrenMap>() }>()
            .map_err(|err| err.set_item("children map"))
            .copied()
            .map(bytemuck::cast)
    }
}

#[cfg(feature = "ethhash")]
impl<'a> ReadItem<'a> for HashType {
    fn read_item(reader: &mut ProofReader<'a>) -> Result<Self, ReadError> {
        match reader
            .read_item::<u8>()
            .map_err(|err| err.set_item("hash type discriminant"))?
        {
            0 => Ok(HashType::Hash(reader.read_item()?)),
            1 => Ok(HashType::Rlp(reader.read_item::<&[u8]>()?.into())),
            found => {
                Err(reader.invalid_item("hash type discriminant", "0 (hash) or 1 (rlp)", found))
            }
        }
    }
}

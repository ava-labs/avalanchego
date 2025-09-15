// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![cfg_attr(
    not(feature = "ethhash"),
    expect(
        clippy::arithmetic_side_effects,
        reason = "Found 1 occurrences after enabling the lint."
    )
)]

use crate::hashednode::{HasUpdate, Hashable, Preimage};
use crate::{TrieHash, ValueDigest};
/// Merkledb compatible hashing algorithm.
use integer_encoding::VarInt;
use sha2::{Digest, Sha256};

const MAX_VARINT_SIZE: usize = 10;
const BITS_PER_NIBBLE: u64 = 4;

impl HasUpdate for Sha256 {
    fn update<T: AsRef<[u8]>>(&mut self, data: T) {
        sha2::Digest::update(self, data);
    }
}

impl<T: Hashable> Preimage for T {
    fn to_hash(&self) -> TrieHash {
        let mut hasher = Sha256::new();

        self.write(&mut hasher);
        hasher.finalize().into()
    }

    fn write(&self, buf: &mut impl HasUpdate) {
        let children = self.children();

        let num_children = children.iter().filter(|c| c.is_some()).count() as u64;

        add_varint_to_buf(buf, num_children);

        for (index, hash) in children.iter().enumerate() {
            if let Some(hash) = hash {
                add_varint_to_buf(buf, index as u64);
                buf.update(hash);
            }
        }

        // Add value digest (if any) to hash pre-image
        add_value_digest_to_buf(buf, self.value_digest());

        // Add key length (in bits) to hash pre-image
        let mut key = self.full_path();
        // let mut key = key.as_ref().iter();
        let key_bit_len = BITS_PER_NIBBLE * key.clone().count() as u64;
        add_varint_to_buf(buf, key_bit_len);

        // Add key to hash pre-image
        while let Some(high_nibble) = key.next() {
            let low_nibble = key.next().unwrap_or(0);
            let byte = (high_nibble << 4) | low_nibble;
            buf.update([byte]);
        }
    }
}

fn add_value_digest_to_buf<H: HasUpdate, T: AsRef<[u8]>>(
    buf: &mut H,
    value_digest: Option<ValueDigest<T>>,
) {
    let Some(value_digest) = value_digest else {
        let value_exists: u8 = 0;
        buf.update([value_exists]);
        return;
    };

    let value_exists: u8 = 1;
    buf.update([value_exists]);

    match value_digest {
        ValueDigest::Value(value) if value.as_ref().len() >= 32 => {
            let hash = Sha256::digest(value);
            add_len_and_value_to_buf(buf, hash);
        }
        ValueDigest::Value(value) => {
            add_len_and_value_to_buf(buf, value);
        }
        ValueDigest::Hash(hash) => {
            add_len_and_value_to_buf(buf, hash);
        }
    }
}

#[inline]
/// Writes the length of `value` and `value` to `buf`.
fn add_len_and_value_to_buf<H: HasUpdate, V: AsRef<[u8]>>(buf: &mut H, value: V) {
    let value_len = value.as_ref().len();
    buf.update([value_len as u8]);
    buf.update(value);
}

#[inline]
/// Encodes `value` as a varint and writes it to `buf`.
fn add_varint_to_buf<H: HasUpdate>(buf: &mut H, value: u64) {
    let mut buf_arr = [0u8; MAX_VARINT_SIZE];
    let len = value.encode_var(&mut buf_arr);
    buf.update(
        buf_arr
            .get(..len)
            .expect("length is always less than MAX_VARINT_SIZE"),
    );
}

// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Ethereum compatible hashing algorithm.

#![cfg_attr(
    feature = "ethhash",
    expect(
        clippy::indexing_slicing,
        reason = "Found 4 occurrences after enabling the lint."
    )
)]
#![cfg_attr(
    feature = "ethhash",
    expect(
        clippy::too_many_lines,
        reason = "Found 1 occurrences after enabling the lint."
    )
)]

use std::iter::once;

use crate::logger::warn;
use crate::{
    HashType, Hashable, Preimage, TrieHash, ValueDigest, hashednode::HasUpdate, logger::trace,
};
use bitfield::bitfield;
use bytes::BytesMut;
use sha3::{Digest, Keccak256};
use smallvec::SmallVec;

use rlp::{Rlp, RlpStream};

impl HasUpdate for Keccak256 {
    fn update<T: AsRef<[u8]>>(&mut self, data: T) {
        sha3::Digest::update(self, data);
    }
}

// Takes a set of nibbles and converts them to a set of bytes that we can hash
// The input consists of nibbles, but there may be an invalid nibble at the end of 0x10
// which indicates that we need to set bit 5 of the first output byte
// The input may also have an odd number of nibbles, in which case the first output byte
// will have bit 4 set and the low nibble will be the low nibble of the first byte
// Restated: 00ABCCCC
// 0 is always 0
// A is 1 if this is a leaf
// B is 1 if the input had an odd number of nibbles
// CCCC is the first nibble if B is 1, otherwise it is all 0s

fn nibbles_to_eth_compact<T: AsRef<[u8]>>(nibbles: T, is_leaf: bool) -> SmallVec<[u8; 32]> {
    // This is a bitfield that represents the first byte of the output, documented above
    bitfield! {
        struct CompactFirstByte(u8);
        impl Debug;
        impl new;
        u8;
        is_leaf, set_is_leaf: 5;
        odd_nibbles, set_odd_nibbles: 4;
        low_nibble, set_low_nibble: 3, 0;
    }

    let nibbles = nibbles.as_ref();

    // nibble_pairs points to the first nibble that will be combined with the next nibble
    // so we skip the first byte if there's an odd length and set the odd_nibbles bit to true
    let (nibble_pairs, first_byte) = if nibbles.len() & 1 == 1 {
        let low_nibble = nibbles[0];
        debug_assert!(low_nibble < 16);
        (
            &nibbles[1..],
            CompactFirstByte::new(is_leaf, true, low_nibble),
        )
    } else {
        (nibbles, CompactFirstByte::new(is_leaf, false, 0))
    };

    // at this point, we can be sure that nibble_pairs has an even length
    debug_assert!(nibble_pairs.len() % 2 == 0);

    // now assemble everything: the first byte, and the nibble pairs compacted back together
    once(first_byte.0)
        .chain(
            nibble_pairs
                .chunks(2)
                .map(|chunk| (chunk[0] << 4) | chunk[1]),
        )
        .collect()
}

impl<T: Hashable> Preimage for T {
    fn to_hash(&self) -> HashType {
        // first collect the thing that would be hashed, and if it's smaller than a hash,
        // just use it directly
        let mut collector = SmallVec::with_capacity(32);
        self.write(&mut collector);

        if crate::logger::trace_enabled() {
            if self.key().size_hint().0 == 64 {
                trace!("SIZE WAS 64 {}", hex::encode(&collector));
            } else {
                trace!(
                    "SIZE WAS {1} {0}",
                    hex::encode(&collector),
                    self.key().size_hint().0
                );
            }
        }

        if collector.len() >= 32 {
            HashType::Hash(Keccak256::digest(collector).into())
        } else {
            HashType::Rlp(collector)
        }
    }

    fn write(&self, buf: &mut impl HasUpdate) {
        let is_account = self.key().size_hint().0 == 64;
        trace!("is_account: {is_account}");

        let children = self.children().count();

        if children == 0 {
            // since there are no children, this must be a leaf
            // we append two items, the partial_path, encoded, and the value
            // note that leaves must always have a value, so we know there
            // will be 2 items

            let mut rlp = RlpStream::new_list(2);

            rlp.append(&&*nibbles_to_eth_compact(
                self.partial_path().collect::<Box<_>>(),
                true,
            ));

            if is_account {
                // we are a leaf that is at depth 32
                match self.value_digest() {
                    Some(ValueDigest::Value(bytes)) => {
                        let new_hash = Keccak256::digest(rlp::NULL_RLP).as_slice().to_vec();
                        let bytes_mut = BytesMut::from(bytes);
                        if let Some(result) = replace_hash(bytes_mut, new_hash) {
                            rlp.append(&&*result);
                        } else {
                            rlp.append(&bytes);
                        }
                    }
                    Some(ValueDigest::Hash(hash)) => {
                        rlp.append(&hash);
                    }
                    None => {
                        rlp.append_empty_data();
                    }
                }
            } else {
                match self.value_digest() {
                    Some(ValueDigest::Value(bytes)) => rlp.append(&bytes),
                    Some(ValueDigest::Hash(hash)) => rlp.append(&hash),
                    None => rlp.append_empty_data(),
                };
            }

            let bytes = rlp.out();
            trace!(
                "partial path {:?}",
                hex::encode(self.partial_path().collect::<Box<_>>())
            );
            trace!("serialized leaf-rlp: {:?}", hex::encode(&bytes));
            buf.update(&bytes);
        } else {
            // for a branch, there are always 16 children and a value
            // Child::None we encode as RLP empty_data (0x80)
            let mut rlp = RlpStream::new_list(17);
            let mut child_iter = self.children().peekable();
            for index in 0..=15 {
                if let Some(&(child_index, digest)) = child_iter.peek() {
                    if child_index == index {
                        match digest {
                            HashType::Hash(hash) => rlp.append(&hash.as_slice()),
                            HashType::Rlp(rlp_bytes) => rlp.append_raw(rlp_bytes, 1),
                        };
                        child_iter.next();
                    } else {
                        rlp.append_empty_data();
                    }
                } else {
                    // exhausted all indexes
                    rlp.append_empty_data();
                }
            }

            if let Some(digest) = self.value_digest() {
                if is_account {
                    rlp.append_empty_data();
                } else {
                    rlp.append(&*digest);
                }
            } else {
                rlp.append_empty_data();
            }
            let bytes = rlp.out();
            trace!("pass 1 bytes {:02X?}", hex::encode(&bytes));

            // we've collected all the children in bytes

            #[allow(clippy::let_and_return)]
            let updated_bytes = if is_account {
                // need to get the value again
                if let Some(ValueDigest::Value(rlp_encoded_bytes)) = self.value_digest() {
                    // rlp_encoded__bytes needs to be decoded
                    // TODO: Handle corruption
                    // needs to be the hash of the RLP encoding of the root node that
                    // would have existed here (instead of this account node)
                    // the "root node" is actually this branch node iff there is
                    // more than one child. If there is only one child, then the
                    // child is actually the root node, so we need the hash of that
                    // child here.
                    let replacement_hash = if children == 1 {
                        // we need to treat this child like it's a root node, so the partial path is
                        // actually one longer than it is reported
                        match self.children().next().expect("we know there is one").1 {
                            HashType::Hash(hash) => hash.clone(),
                            HashType::Rlp(rlp_bytes) => {
                                let mut rlp = RlpStream::new_list(2);
                                rlp.append(&&*nibbles_to_eth_compact(
                                    self.partial_path().collect::<Box<_>>(),
                                    true,
                                ));
                                rlp.append_raw(rlp_bytes, 1);
                                let bytes = rlp.out();
                                TrieHash::from(Keccak256::digest(bytes))
                            }
                        }
                    } else {
                        TrieHash::from(Keccak256::digest(bytes))
                    };
                    trace!("replacement hash {:?}", hex::encode(&replacement_hash));

                    let bytes = replace_hash(rlp_encoded_bytes, replacement_hash)
                        .unwrap_or_else(|| BytesMut::from(rlp_encoded_bytes));
                    trace!("updated encoded value {:02X?}", hex::encode(&bytes));
                    bytes
                } else {
                    // treat like non-account since it didn't have a value
                    warn!(
                        "Account node {:x?} without value",
                        self.key().collect::<Vec<_>>()
                    );
                    bytes.as_ref().into()
                }
            } else {
                bytes.as_ref().into()
            };

            let partial_path = self.partial_path().collect::<Box<_>>();
            if partial_path.is_empty() {
                trace!("pass 2=bytes {:02X?}", hex::encode(&updated_bytes));
                buf.update(updated_bytes);
            } else {
                let mut final_bytes = RlpStream::new_list(2);
                final_bytes.append(&&*nibbles_to_eth_compact(partial_path, is_account));
                // if the RLP is short enough, we can use it as-is, otherwise we hash it
                // to make the maximum length 32 bytes
                if updated_bytes.len() > 31 && !is_account {
                    let hashed_bytes = Keccak256::digest(updated_bytes);
                    final_bytes.append(&hashed_bytes.as_slice());
                } else {
                    final_bytes.append(&updated_bytes);
                }
                let final_bytes = final_bytes.out();
                trace!("pass 2 bytes {:02X?}", hex::encode(&final_bytes));
                buf.update(final_bytes);
            }
        }
    }
}

// TODO: we could be super fancy and just plunk the correct bytes into the existing BytesMut
fn replace_hash<T: AsRef<[u8]>, U: AsRef<[u8]>>(bytes: T, new_hash: U) -> Option<BytesMut> {
    // rlp_encoded_bytes needs to be decoded
    let rlp = Rlp::new(bytes.as_ref());
    let mut list = rlp.as_list().ok()?;
    let replace = list.get_mut(2)?;
    *replace = Vec::from(new_hash.as_ref());

    trace!("inbound bytes: {}", hex::encode(bytes.as_ref()));
    trace!("list length was {}", list.len());
    trace!("replacement hash {:?}", hex::encode(&new_hash));

    let mut rlp = RlpStream::new_list(list.len());
    for item in list {
        rlp.append(&item);
    }
    let bytes = rlp.out();
    trace!("updated encoded value {:02X?}", hex::encode(&bytes));
    Some(bytes)
}

#[cfg(test)]
mod test {
    use test_case::test_case;

    #[test_case(&[], false, &[0x00])]
    #[test_case(&[], true, &[0x20])]
    #[test_case(&[1, 2, 3, 4, 5], false, &[0x11, 0x23, 0x45])]
    #[test_case(&[0, 1, 2, 3, 4, 5], false, &[0x00, 0x01, 0x23, 0x45])]
    #[test_case(&[15, 1, 12, 11, 8], true, &[0x3f, 0x1c, 0xb8])]
    #[test_case(&[0, 15, 1, 12, 11, 8], true, &[0x20, 0x0f, 0x1c, 0xb8])]
    fn test_hex_to_compact(hex: &[u8], has_value: bool, expected_compact: &[u8]) {
        assert_eq!(
            &*super::nibbles_to_eth_compact(hex, has_value),
            expected_compact
        );
    }
}

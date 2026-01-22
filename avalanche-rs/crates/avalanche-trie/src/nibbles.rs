//! Nibble representation for trie keys.
//!
//! In a Merkle Patricia Trie, keys are represented as sequences of nibbles
//! (4-bit values). This module provides efficient nibble manipulation and
//! hex-prefix encoding as specified in the Ethereum Yellow Paper.

use smallvec::SmallVec;

/// A sequence of nibbles (4-bit values).
/// Uses SmallVec for stack allocation of small keys.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Nibbles {
    data: SmallVec<[u8; 64]>,
}

impl Nibbles {
    /// Creates an empty nibble sequence.
    pub fn new() -> Self {
        Self {
            data: SmallVec::new(),
        }
    }

    /// Creates nibbles from a byte slice, converting each byte to two nibbles.
    pub fn from_bytes(bytes: &[u8]) -> Self {
        let mut data = SmallVec::with_capacity(bytes.len() * 2);
        for byte in bytes {
            data.push(byte >> 4);
            data.push(byte & 0x0f);
        }
        Self { data }
    }

    /// Creates nibbles from raw nibble values.
    pub fn from_nibbles(nibbles: &[u8]) -> Self {
        Self {
            data: SmallVec::from_slice(nibbles),
        }
    }

    /// Returns the number of nibbles.
    pub fn len(&self) -> usize {
        self.data.len()
    }

    /// Returns true if empty.
    pub fn is_empty(&self) -> bool {
        self.data.is_empty()
    }

    /// Returns the nibble at the given index.
    pub fn get(&self, index: usize) -> Option<u8> {
        self.data.get(index).copied()
    }

    /// Returns the first nibble.
    pub fn first(&self) -> Option<u8> {
        self.data.first().copied()
    }

    /// Returns a slice of nibbles from the given offset.
    pub fn slice_from(&self, offset: usize) -> Self {
        if offset >= self.data.len() {
            return Self::new();
        }
        Self {
            data: SmallVec::from_slice(&self.data[offset..]),
        }
    }

    /// Returns a slice of nibbles up to the given length.
    pub fn slice_to(&self, len: usize) -> Self {
        let end = len.min(self.data.len());
        Self {
            data: SmallVec::from_slice(&self.data[..end]),
        }
    }

    /// Returns the common prefix length with another nibble sequence.
    pub fn common_prefix_len(&self, other: &Nibbles) -> usize {
        self.data
            .iter()
            .zip(other.data.iter())
            .take_while(|(a, b)| a == b)
            .count()
    }

    /// Returns the common prefix with another nibble sequence.
    pub fn common_prefix(&self, other: &Nibbles) -> Self {
        let len = self.common_prefix_len(other);
        self.slice_to(len)
    }

    /// Appends a nibble to the end.
    pub fn push(&mut self, nibble: u8) {
        debug_assert!(nibble < 16);
        self.data.push(nibble);
    }

    /// Appends nibbles from another sequence.
    pub fn extend(&mut self, other: &Nibbles) {
        self.data.extend_from_slice(&other.data);
    }

    /// Concatenates two nibble sequences.
    pub fn concat(&self, other: &Nibbles) -> Self {
        let mut result = self.clone();
        result.extend(other);
        result
    }

    /// Returns the underlying nibble data.
    pub fn as_slice(&self) -> &[u8] {
        &self.data
    }

    /// Encodes nibbles using hex-prefix encoding.
    ///
    /// Hex-prefix encoding prepends a nibble that encodes:
    /// - Bit 0: 1 if the path has odd length
    /// - Bit 1: 1 if this is a leaf node (terminator)
    ///
    /// If the path has odd length, the first nibble of the path is
    /// packed with the prefix nibble. Otherwise, a zero nibble is added.
    pub fn encode_hex_prefix(&self, is_leaf: bool) -> Vec<u8> {
        let odd_len = self.data.len() % 2 == 1;
        let prefix = if is_leaf { 2 } else { 0 } + if odd_len { 1 } else { 0 };

        let mut result = Vec::with_capacity((self.data.len() + 2) / 2);

        if odd_len {
            // Pack prefix with first nibble
            result.push((prefix << 4) | self.data[0]);
            for chunk in self.data[1..].chunks(2) {
                result.push((chunk[0] << 4) | chunk[1]);
            }
        } else {
            // Prefix nibble + zero nibble
            result.push(prefix << 4);
            for chunk in self.data.chunks(2) {
                result.push((chunk[0] << 4) | chunk[1]);
            }
        }

        result
    }

    /// Decodes hex-prefix encoded bytes into nibbles.
    /// Returns (nibbles, is_leaf).
    pub fn decode_hex_prefix(encoded: &[u8]) -> Option<(Self, bool)> {
        if encoded.is_empty() {
            return None;
        }

        let prefix = encoded[0] >> 4;
        let is_leaf = prefix & 2 != 0;
        let odd_len = prefix & 1 != 0;

        let mut nibbles = SmallVec::new();

        if odd_len {
            // First nibble is packed with prefix
            nibbles.push(encoded[0] & 0x0f);
        }

        for byte in &encoded[1..] {
            nibbles.push(byte >> 4);
            nibbles.push(byte & 0x0f);
        }

        Some((Self { data: nibbles }, is_leaf))
    }

    /// Converts nibbles back to bytes (must have even length).
    pub fn to_bytes(&self) -> Option<Vec<u8>> {
        if self.data.len() % 2 != 0 {
            return None;
        }

        let mut bytes = Vec::with_capacity(self.data.len() / 2);
        for chunk in self.data.chunks(2) {
            bytes.push((chunk[0] << 4) | chunk[1]);
        }
        Some(bytes)
    }
}

impl Default for Nibbles {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Display for Nibbles {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for nibble in &self.data {
            write!(f, "{:x}", nibble)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_from_bytes() {
        let nibbles = Nibbles::from_bytes(&[0x12, 0x34, 0xab]);
        assert_eq!(nibbles.len(), 6);
        assert_eq!(nibbles.as_slice(), &[1, 2, 3, 4, 10, 11]);
    }

    #[test]
    fn test_slice_operations() {
        let nibbles = Nibbles::from_bytes(&[0x12, 0x34]);

        let slice = nibbles.slice_from(1);
        assert_eq!(slice.as_slice(), &[2, 3, 4]);

        let slice = nibbles.slice_to(2);
        assert_eq!(slice.as_slice(), &[1, 2]);
    }

    #[test]
    fn test_common_prefix() {
        let a = Nibbles::from_bytes(&[0x12, 0x34]);
        let b = Nibbles::from_bytes(&[0x12, 0x56]);

        assert_eq!(a.common_prefix_len(&b), 2);

        let prefix = a.common_prefix(&b);
        assert_eq!(prefix.as_slice(), &[1, 2]);
    }

    #[test]
    fn test_hex_prefix_leaf_even() {
        let nibbles = Nibbles::from_nibbles(&[1, 2, 3, 4]);
        let encoded = nibbles.encode_hex_prefix(true);
        assert_eq!(encoded, vec![0x20, 0x12, 0x34]);

        let (decoded, is_leaf) = Nibbles::decode_hex_prefix(&encoded).unwrap();
        assert!(is_leaf);
        assert_eq!(decoded.as_slice(), &[1, 2, 3, 4]);
    }

    #[test]
    fn test_hex_prefix_leaf_odd() {
        let nibbles = Nibbles::from_nibbles(&[1, 2, 3]);
        let encoded = nibbles.encode_hex_prefix(true);
        assert_eq!(encoded, vec![0x31, 0x23]);

        let (decoded, is_leaf) = Nibbles::decode_hex_prefix(&encoded).unwrap();
        assert!(is_leaf);
        assert_eq!(decoded.as_slice(), &[1, 2, 3]);
    }

    #[test]
    fn test_hex_prefix_extension_even() {
        let nibbles = Nibbles::from_nibbles(&[1, 2, 3, 4]);
        let encoded = nibbles.encode_hex_prefix(false);
        assert_eq!(encoded, vec![0x00, 0x12, 0x34]);

        let (decoded, is_leaf) = Nibbles::decode_hex_prefix(&encoded).unwrap();
        assert!(!is_leaf);
        assert_eq!(decoded.as_slice(), &[1, 2, 3, 4]);
    }

    #[test]
    fn test_hex_prefix_extension_odd() {
        let nibbles = Nibbles::from_nibbles(&[1, 2, 3]);
        let encoded = nibbles.encode_hex_prefix(false);
        assert_eq!(encoded, vec![0x11, 0x23]);

        let (decoded, is_leaf) = Nibbles::decode_hex_prefix(&encoded).unwrap();
        assert!(!is_leaf);
        assert_eq!(decoded.as_slice(), &[1, 2, 3]);
    }

    #[test]
    fn test_to_bytes() {
        let nibbles = Nibbles::from_bytes(&[0x12, 0x34, 0xab]);
        let bytes = nibbles.to_bytes().unwrap();
        assert_eq!(bytes, vec![0x12, 0x34, 0xab]);

        // Odd length should fail
        let odd = Nibbles::from_nibbles(&[1, 2, 3]);
        assert!(odd.to_bytes().is_none());
    }

    #[test]
    fn test_concat() {
        let a = Nibbles::from_nibbles(&[1, 2]);
        let b = Nibbles::from_nibbles(&[3, 4]);
        let c = a.concat(&b);
        assert_eq!(c.as_slice(), &[1, 2, 3, 4]);
    }
}

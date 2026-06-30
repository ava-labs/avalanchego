// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Minimal RLP encoder/decoder tailored to firewood's usage.
//!
//! Covers only the shapes firewood needs: encoding flat lists, decoding flat
//! lists of byte-string children (borrowed, no `Vec<Vec<u8>>`), and splicing
//! one byte-string field of a list (`replace_list_field`, used for the
//! account `storageRoot` rewrite). No nested-list decoding, no
//! `Encodable`/`Decodable` traits, no streaming builder.

use thiserror::Error;

/// RLP encoding of an empty byte string (`0x80`).
pub const NULL_RLP: &[u8] = &[0x80];

/// Errors produced while decoding RLP.
#[derive(Debug, Error, PartialEq, Eq)]
pub enum RlpError {
    /// Input ended before the current item finished.
    #[error("RLP input truncated")]
    Truncated,
    /// A length prefix exceeds `usize`.
    #[error("RLP length prefix overflows")]
    Overflow,
    /// Expected a byte-string item, found a list.
    #[error("expected a byte string, found a list")]
    ExpectedBytes,
    /// Expected a list item, found a byte string.
    #[error("expected a list, found a byte string")]
    ExpectedList,
    /// Index past the last element of a list.
    #[error("RLP index {0} out of range")]
    IndexOutOfRange(usize),
    /// Bytes followed the parsed item.
    #[error("trailing bytes after RLP item")]
    TrailingBytes,
    /// Length prefix or single-byte form was not used in its minimal encoding.
    #[error("non-minimal RLP length encoding")]
    NonMinimalLength,
    /// Byte-string longer than the caller's bound when parsing a variable-
    /// length unsigned integer.
    #[error("RLP byte string is {actual} bytes, max {max}")]
    TooLong {
        /// Length of the input byte string.
        actual: usize,
        /// Maximum length the caller accepts.
        max: usize,
    },
    /// Byte-string length did not match the caller's fixed-size expectation.
    #[error("RLP byte string is {actual} bytes, expected {expected}")]
    WrongLength {
        /// Length of the input byte string.
        actual: usize,
        /// Expected exact length.
        expected: usize,
    },
}

/// One child of a list passed to [`encode_list`].
#[derive(Copy, Clone, Debug)]
pub enum RlpItem<'a> {
    /// A byte-string child; the appropriate RLP header is added by the encoder.
    Bytes(&'a [u8]),
    /// A pre-encoded RLP item whose bytes are copied verbatim.
    Raw(&'a [u8]),
    /// The empty byte-string marker (`0x80`).
    Empty,
}

/// Encode a slice of children as one RLP list. Single allocation.
#[must_use]
#[inline]
pub fn encode_list(items: &[RlpItem<'_>]) -> Box<[u8]> {
    let payload_len: usize = items.iter().map(RlpItem::encoded_len).sum();
    // safe: header_len <= 9 and payload_len already fits in memory.
    let total = header_len(payload_len).wrapping_add(payload_len);
    let mut out = Vec::with_capacity(total);
    write_header(&mut out, payload_len, 0xc0);
    for item in items {
        item.write_into(&mut out);
    }
    debug_assert_eq!(out.len(), total);
    out.into_boxed_slice()
}

/// A parsed RLP list, holding a slice into the caller's buffer.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RlpList<'a> {
    payload: &'a [u8],
}

impl<'a> RlpList<'a> {
    /// Parse `bytes` as a single RLP list item.
    ///
    /// # Errors
    ///
    /// Returns [`RlpError::ExpectedList`] if `bytes` encodes a byte string,
    /// [`RlpError::Truncated`] if the input ends mid-item, and
    /// [`RlpError::TrailingBytes`] if extra bytes follow the list.
    #[inline]
    pub fn parse(bytes: &'a [u8]) -> Result<Self, RlpError> {
        let header = parse_header(bytes)?;
        if header.kind != ItemKind::List {
            return Err(RlpError::ExpectedList);
        }
        let total = header.total_len()?;
        if total > bytes.len() {
            return Err(RlpError::Truncated);
        }
        if total < bytes.len() {
            return Err(RlpError::TrailingBytes);
        }
        let payload = bytes
            .get(header.header_len..total)
            .ok_or(RlpError::Truncated)?;
        Ok(Self { payload })
    }

    /// Collect the list's children into a vector of byte-string slices.
    ///
    /// # Errors
    ///
    /// Returns [`RlpError::ExpectedBytes`] if any child is itself a list,
    /// or any header-decoding error if the payload is malformed.
    #[inline]
    pub fn fields(&self) -> Result<Vec<&'a [u8]>, RlpError> {
        let mut out = Vec::new();
        let mut rest = self.payload;
        while !rest.is_empty() {
            let (field, after) = next_field(rest)?;
            out.push(field);
            rest = after;
        }
        Ok(out)
    }

    /// Return the `i`-th child as a byte-string slice.
    ///
    /// # Errors
    ///
    /// Returns [`RlpError::IndexOutOfRange`] if the list has fewer than
    /// `i + 1` children, or [`RlpError::ExpectedBytes`] if the target
    /// child is itself a list.
    #[inline]
    pub fn nth_bytes(&self, i: usize) -> Result<&'a [u8], RlpError> {
        let mut rest = self.payload;
        for _ in 0..i {
            if rest.is_empty() {
                return Err(RlpError::IndexOutOfRange(i));
            }
            let (_, after) = next_field(rest)?;
            rest = after;
        }
        if rest.is_empty() {
            return Err(RlpError::IndexOutOfRange(i));
        }
        let (field, _) = next_field(rest)?;
        Ok(field)
    }
}

/// Read one byte-string child from the start of a non-empty `payload`,
/// returning the field bytes and the remainder. Errors if the next item is
/// a list or has a malformed header.
#[inline]
fn next_field(payload: &[u8]) -> Result<(&[u8], &[u8]), RlpError> {
    let header = parse_header(payload)?;
    let total = header.total_len()?;
    let item = payload.get(..total).ok_or(RlpError::Truncated)?;
    let after = payload.get(total..).unwrap_or(&[]);
    if header.kind != ItemKind::Bytes {
        return Err(RlpError::ExpectedBytes);
    }
    let field = item.get(header.header_len..).ok_or(RlpError::Truncated)?;
    Ok((field, after))
}

/// Return a new RLP list identical to `input` except that the byte-string
/// at `field_index` is replaced by `replacement`. Equal-length replacement
/// (the 32-byte hash case) is one allocation plus two `memcpy`s.
///
/// # Errors
///
/// Returns an [`RlpError`] if `input` is malformed, is not a list, has fewer
/// than `field_index + 1` children, or the target child is itself a list.
#[inline]
pub fn replace_list_field(
    input: &[u8],
    field_index: usize,
    replacement: &[u8],
) -> Result<Box<[u8]>, RlpError> {
    let outer = parse_header(input)?;
    if outer.kind != ItemKind::List {
        return Err(RlpError::ExpectedList);
    }
    let total = outer.total_len()?;
    if total > input.len() {
        return Err(RlpError::Truncated);
    }
    if total < input.len() {
        return Err(RlpError::TrailingBytes);
    }
    let payload = input
        .get(outer.header_len..total)
        .ok_or(RlpError::Truncated)?;

    let mut offset = 0usize;
    let mut idx = 0usize;
    let (item_start, item_end) = loop {
        let rest = payload.get(offset..).ok_or(RlpError::Truncated)?;
        if rest.is_empty() {
            return Err(RlpError::IndexOutOfRange(field_index));
        }
        let item = parse_header(rest)?;
        let item_total = item.total_len()?;
        // Reject items whose header advertises more bytes than `rest` actually
        // contains. Without this, a malicious header could wrap `next` below
        // and produce silently corrupted output.
        if item_total > rest.len() {
            return Err(RlpError::Truncated);
        }
        let next = offset.wrapping_add(item_total); // bounded by payload.len()
        if idx == field_index {
            if item.kind != ItemKind::Bytes {
                return Err(RlpError::ExpectedBytes);
            }
            break (offset, next);
        }
        offset = next;
        idx = idx.checked_add(1).ok_or(RlpError::Overflow)?;
    };

    // All wrapping ops below stay non-wrapping by construction:
    //   item_end > item_start, so the subtract is fine;
    //   the result is at most payload.len(), so the rest fits in memory.
    let old_field_encoded_len = item_end.wrapping_sub(item_start);
    let new_field_encoded_len = bytes_encoded_len(replacement);
    let new_payload_len = payload
        .len()
        .wrapping_sub(old_field_encoded_len)
        .wrapping_add(new_field_encoded_len);
    let new_total = header_len(new_payload_len).wrapping_add(new_payload_len);
    let mut out = Vec::with_capacity(new_total);
    write_header(&mut out, new_payload_len, 0xc0);
    let prefix = payload.get(..item_start).ok_or(RlpError::Truncated)?;
    let suffix = payload.get(item_end..).ok_or(RlpError::Truncated)?;
    out.extend_from_slice(prefix);
    write_bytes(&mut out, replacement);
    out.extend_from_slice(suffix);
    debug_assert_eq!(out.len(), new_total);
    Ok(out.into_boxed_slice())
}

/// Right-align an RLP byte string into an `N`-byte big-endian buffer, padding
/// the high bytes with zero.
///
/// Matches the shape of RLP-encoded unsigned integers: leading zeros are
/// stripped on the wire, so a `u64` of value 1 arrives as the single byte
/// `0x01` — this helper widens it back to the fixed-size form the caller
/// wants.
///
/// # Errors
///
/// Returns [`RlpError::TooLong`] if `bytes.len() > N`.
#[inline]
pub fn parse_be_uint<const N: usize>(bytes: &[u8]) -> Result<[u8; N], RlpError> {
    if bytes.len() > N {
        return Err(RlpError::TooLong {
            actual: bytes.len(),
            max: N,
        });
    }
    let mut out = [0u8; N];
    for (slot, &b) in out.iter_mut().rev().zip(bytes.iter().rev()) {
        *slot = b;
    }
    Ok(out)
}

/// Convert an RLP byte string of exactly `N` bytes into `[u8; N]`.
///
/// # Errors
///
/// Returns [`RlpError::WrongLength`] if `bytes.len() != N`.
#[inline]
pub fn parse_fixed<const N: usize>(bytes: &[u8]) -> Result<[u8; N], RlpError> {
    bytes.try_into().map_err(|_| RlpError::WrongLength {
        actual: bytes.len(),
        expected: N,
    })
}

// ---------- internals ----------

#[derive(Copy, Clone, Debug, PartialEq, Eq)]
enum ItemKind {
    Bytes,
    List,
}

#[derive(Copy, Clone, Debug)]
struct ItemHeader {
    kind: ItemKind,
    header_len: usize,
    payload_len: usize,
}

impl ItemHeader {
    #[inline]
    fn total_len(&self) -> Result<usize, RlpError> {
        self.header_len
            .checked_add(self.payload_len)
            .ok_or(RlpError::Overflow)
    }
}

impl RlpItem<'_> {
    #[inline]
    fn encoded_len(&self) -> usize {
        match self {
            RlpItem::Bytes(b) => bytes_encoded_len(b),
            RlpItem::Raw(r) => r.len(),
            RlpItem::Empty => 1,
        }
    }

    #[inline]
    fn write_into(&self, out: &mut Vec<u8>) {
        match self {
            RlpItem::Bytes(b) => write_bytes(out, b),
            RlpItem::Raw(r) => out.extend_from_slice(r),
            RlpItem::Empty => out.push(0x80),
        }
    }
}

#[inline]
fn parse_header(input: &[u8]) -> Result<ItemHeader, RlpError> {
    let &first = input.first().ok_or(RlpError::Truncated)?;
    // All `wrapping_sub`s below are bounded by their match arms.
    match first {
        0x00..=0x7f => Ok(ItemHeader {
            kind: ItemKind::Bytes,
            header_len: 0,
            payload_len: 1,
        }),
        0x80..=0xb7 => {
            let payload_len = first.wrapping_sub(0x80) as usize;
            // The 0x81 0x?? form must use the single-byte form when ?? < 0x80.
            if payload_len == 1
                && let Some(&b) = input.get(1)
                && b < 0x80
            {
                return Err(RlpError::NonMinimalLength);
            }
            Ok(ItemHeader {
                kind: ItemKind::Bytes,
                header_len: 1,
                payload_len,
            })
        }
        0xb8..=0xbf => parse_long_header(input, first.wrapping_sub(0xb7) as usize, ItemKind::Bytes),
        0xc0..=0xf7 => Ok(ItemHeader {
            kind: ItemKind::List,
            header_len: 1,
            payload_len: first.wrapping_sub(0xc0) as usize,
        }),
        0xf8..=0xff => parse_long_header(input, first.wrapping_sub(0xf7) as usize, ItemKind::List),
    }
}

#[inline]
fn parse_long_header(
    input: &[u8],
    len_of_len: usize,
    kind: ItemKind,
) -> Result<ItemHeader, RlpError> {
    if len_of_len > size_of::<usize>() {
        return Err(RlpError::Overflow);
    }
    let header_len = 1usize.wrapping_add(len_of_len); // <= 9
    let len_bytes = input.get(1..header_len).ok_or(RlpError::Truncated)?;
    if len_bytes.first().copied() == Some(0) {
        return Err(RlpError::NonMinimalLength);
    }
    let payload_len = read_be_usize(len_bytes);
    if payload_len < 56 {
        return Err(RlpError::NonMinimalLength);
    }
    Ok(ItemHeader {
        kind,
        header_len,
        payload_len,
    })
}

#[inline]
fn read_be_usize(bytes: &[u8]) -> usize {
    // Caller checks `bytes.len() <= size_of::<usize>()`, so the shift never
    // loses bits.
    bytes
        .iter()
        .fold(0usize, |acc, &b| (acc << 8) | usize::from(b))
}

#[inline]
fn write_bytes(out: &mut Vec<u8>, b: &[u8]) {
    if b.len() == 1 && b.first().is_some_and(|&v| v < 0x80) {
        out.extend_from_slice(b);
    } else {
        write_header(out, b.len(), 0x80);
        out.extend_from_slice(b);
    }
}

#[inline]
fn bytes_encoded_len(b: &[u8]) -> usize {
    if b.len() == 1 && b.first().is_some_and(|&v| v < 0x80) {
        1
    } else {
        // header_len <= 9 + b.len() (already in memory)
        header_len(b.len()).wrapping_add(b.len())
    }
}

#[inline]
const fn header_len(payload_len: usize) -> usize {
    if payload_len < 56 {
        1
    } else {
        1usize.wrapping_add(minimal_be_len(payload_len)) // <= 9
    }
}

/// Write the RLP header for a payload. `short_base` is `0x80` for byte
/// strings and `0xc0` for lists; the long-form base is `short_base + 55`.
#[inline]
fn write_header(out: &mut Vec<u8>, payload_len: usize, short_base: u8) {
    if payload_len < 56 {
        out.push(short_base.wrapping_add(payload_len as u8));
    } else {
        let bytes = payload_len.to_be_bytes();
        let start = bytes.len().wrapping_sub(minimal_be_len(payload_len));
        let len_bytes = bytes.get(start..).unwrap_or(&[]);
        out.push(
            short_base
                .wrapping_add(55)
                .wrapping_add(len_bytes.len() as u8),
        );
        out.extend_from_slice(len_bytes);
    }
}

#[inline]
const fn minimal_be_len(n: usize) -> usize {
    if n == 0 {
        0
    } else {
        // n != 0, so leading_zeros < usize::BITS.
        let nonzero_bits = usize::BITS.wrapping_sub(n.leading_zeros()) as usize;
        nonzero_bits.div_ceil(8)
    }
}

#[cfg(test)]
#[expect(
    clippy::unwrap_used,
    clippy::indexing_slicing,
    reason = "tests use unwrap and direct indexing to keep assertions readable"
)]
mod tests {
    use super::*;
    use test_case::test_case;

    fn encode_bytes(b: &[u8]) -> Box<[u8]> {
        let mut out = Vec::with_capacity(bytes_encoded_len(b));
        write_bytes(&mut out, b);
        out.into_boxed_slice()
    }

    // ---------- single-item byte-string encoding ----------

    #[test]
    fn encode_empty_string() {
        assert_eq!(&*encode_bytes(&[]), NULL_RLP);
    }

    #[test]
    fn encode_single_byte_below_0x80() {
        // single bytes < 0x80 encode as themselves
        for b in 0u8..0x80 {
            assert_eq!(&*encode_bytes(&[b]), &[b]);
        }
    }

    #[test]
    fn encode_single_byte_at_or_above_0x80() {
        // single bytes >= 0x80 use the length-prefixed form
        for b in 0x80u8..=0xff {
            assert_eq!(&*encode_bytes(&[b]), &[0x81, b]);
        }
    }

    /// Verify byte-string header shapes: short form (`< 56`) and long form.
    #[test_case(55, &[0x80 + 55] ; "max short form")]
    #[test_case(56, &[0xb8, 56] ; "min long form, 1-byte length")]
    #[test_case(255, &[0xb8, 255] ; "max 1-byte length")]
    #[test_case(256, &[0xb9, 0x01, 0x00] ; "min 2-byte length")]
    #[test_case(65536, &[0xba, 0x01, 0x00, 0x00] ; "min 3-byte length")]
    fn encode_byte_string_header_shapes(len: usize, expected_header: &[u8]) {
        let payload = vec![0xaa; len];
        let encoded = encode_bytes(&payload);
        assert_eq!(&encoded[..expected_header.len()], expected_header);
        assert_eq!(&encoded[expected_header.len()..], payload.as_slice());
    }

    // ---------- list encoding ----------

    #[test]
    fn encode_empty_list() {
        let out = encode_list(&[]);
        assert_eq!(&*out, &[0xc0]);
    }

    #[test]
    fn encode_short_list_with_bytes_and_empty() {
        // 2-element list: ["dog", ""]
        let out = encode_list(&[RlpItem::Bytes(b"dog"), RlpItem::Empty]);
        // payload = 0x83 d o g 0x80 = 5 bytes; header = 0xc0 + 5 = 0xc5
        assert_eq!(&*out, &[0xc5, 0x83, b'd', b'o', b'g', 0x80]);
    }

    #[test]
    fn encode_long_list() {
        // Pick children to push payload past 55 bytes.
        let one = vec![0u8; 30];
        let two = vec![1u8; 30];
        let out = encode_list(&[RlpItem::Bytes(&one), RlpItem::Bytes(&two)]);
        // each child: 1 byte header (0x80 + 30) + 30 bytes = 31 bytes
        // total payload = 62; header = 0xf8 0x3e
        assert_eq!(&out[..2], &[0xf8, 62]);
        assert_eq!(out.len(), 64);
    }

    #[test]
    fn encode_list_with_raw_item() {
        // Raw children are copied verbatim without an additional header.
        let raw = encode_bytes(b"cat");
        let out = encode_list(&[RlpItem::Raw(&raw), RlpItem::Bytes(b"dog")]);
        let expected = encode_list(&[RlpItem::Bytes(b"cat"), RlpItem::Bytes(b"dog")]);
        assert_eq!(out, expected);
    }

    // ---------- decoding ----------

    #[test]
    fn parse_empty_list() {
        let list = RlpList::parse(&[0xc0]).unwrap();
        assert!(list.fields().unwrap().is_empty());
    }

    #[test]
    fn parse_list_round_trip() {
        let bytes = encode_list(&[RlpItem::Bytes(b"foo"), RlpItem::Bytes(b"bar")]);
        let list = RlpList::parse(&bytes).unwrap();
        assert_eq!(list.fields().unwrap(), vec![&b"foo"[..], &b"bar"[..]]);
    }

    #[test]
    fn parse_nth_bytes() {
        let bytes = encode_list(&[
            RlpItem::Bytes(b"a"),
            RlpItem::Bytes(b"bb"),
            RlpItem::Bytes(b"ccc"),
            RlpItem::Bytes(b"dddd"),
        ]);
        let list = RlpList::parse(&bytes).unwrap();
        assert_eq!(list.nth_bytes(0).unwrap(), b"a");
        assert_eq!(list.nth_bytes(3).unwrap(), b"dddd");
        assert_eq!(list.nth_bytes(4), Err(RlpError::IndexOutOfRange(4)));
    }

    #[test]
    fn parse_long_string_in_list() {
        // 100-byte string forces long-form bytes header
        let big = vec![0xab; 100];
        let bytes = encode_list(&[RlpItem::Bytes(&big), RlpItem::Bytes(b"end")]);
        let list = RlpList::parse(&bytes).unwrap();
        let items = list.fields().unwrap();
        assert_eq!(items, vec![big.as_slice(), b"end"]);
    }

    #[test_case(&[0x80], RlpError::ExpectedList ; "byte-string header")]
    #[test_case(&[0x42], RlpError::ExpectedList ; "single-byte item")]
    #[test_case(&[0xc0, 0x42], RlpError::TrailingBytes ; "extra byte after empty list")]
    #[test_case(&[], RlpError::Truncated ; "no input")]
    #[test_case(&[0xf8], RlpError::Truncated ; "long-list header without length byte")]
    #[test_case(&[0xc5, 0x83, b'd'], RlpError::Truncated ; "list payload shorter than claimed")]
    fn parse_rejects_bad_input(input: &[u8], expected: RlpError) {
        assert_eq!(RlpList::parse(input), Err(expected));
    }

    #[test]
    fn fields_rejects_nested_list() {
        // List containing a list: 0xc1 0xc0 = list of length 1 containing empty list
        let list = RlpList::parse(&[0xc1, 0xc0]).unwrap();
        assert_eq!(list.fields(), Err(RlpError::ExpectedBytes));
    }

    /// Each input wraps an inner byte-string item that violates a minimal-
    /// length rule. `fields()` should surface the inner item's error.
    #[test_case(&[0xc2, 0x81, 0x42] ; "0x81 b where b < 0x80 should be the bare byte")]
    #[test_case(&{
        let mut b = vec![0xf8, 57, 0xb8, 0x37];
        b.extend(std::iter::repeat_n(0u8, 55));
        b
    } ; "long-form length 55 fits in short form")]
    #[test_case(&{
        let mut b = vec![0xf9, 0x01, 0x02, 0xb9, 0x00, 0xff];
        b.extend(std::iter::repeat_n(0u8, 0xff));
        b
    } ; "long-form length with leading zero")]
    fn parse_rejects_non_minimal_length(buf: &[u8]) {
        let list = RlpList::parse(buf).unwrap();
        assert_eq!(list.fields(), Err(RlpError::NonMinimalLength));
    }

    // ---------- replace_list_field ----------

    fn account_rlp() -> Vec<u8> {
        encode_list(&[
            RlpItem::Bytes(&[0x01]),
            RlpItem::Bytes(&[0x02]),
            RlpItem::Bytes(&[0xaa; 32]),
            RlpItem::Bytes(&[0xbb; 32]),
        ])
        .into_vec()
    }

    /// Replace field 2 of a 4-field account RLP with replacements of
    /// equal, shorter, and longer length, exercising the three header-
    /// resize paths.
    #[test_case(&[0xcc; 32] ; "equal length (32-byte hash)")]
    #[test_case(&[0x55] ; "shorter")]
    #[test_case(&[0x42; 100] ; "longer (forces long-form list header)")]
    fn replace_field_succeeds(replacement: &[u8]) {
        let original = account_rlp();
        let updated = replace_list_field(&original, 2, replacement).unwrap();
        let list = RlpList::parse(&updated).unwrap();
        let items = list.fields().unwrap();
        assert_eq!(
            items,
            vec![&[0x01][..], &[0x02][..], replacement, &[0xbb; 32][..]]
        );
    }

    #[test]
    fn replace_field_equal_length_preserves_total_length() {
        let original = account_rlp();
        let updated = replace_list_field(&original, 2, &[0xcc; 32]).unwrap();
        assert_eq!(updated.len(), original.len());
    }

    #[test_case(&account_rlp(), 4, &[0], RlpError::IndexOutOfRange(4) ; "index past end")]
    #[test_case(&encode_bytes(b"hello"), 0, &[0], RlpError::ExpectedList ; "outer is not a list")]
    #[test_case(&[0xc1, 0xc0], 0, &[0], RlpError::ExpectedBytes ; "target child is itself a list")]
    #[test_case(&{
        // outer claims 9-byte payload (matches); inner header advertises
        // usize::MAX - 100 bytes that aren't there. Without the bounds
        // check this would wrap arithmetic and corrupt output silently.
        let mut b = vec![0xc9, 0xbf];
        b.extend_from_slice(&(usize::MAX - 100).to_be_bytes());
        b
    }, 0, &[0; 32], RlpError::Truncated ; "inner item declares oversized length")]
    fn replace_field_rejects_bad_input(
        input: &[u8],
        index: usize,
        replacement: &[u8],
        expected: RlpError,
    ) {
        assert_eq!(replace_list_field(input, index, replacement), Err(expected));
    }

    // ---------- parse_be_uint / parse_fixed ----------

    #[test_case(&[], [0u8; 8] ; "empty pads to all zeros")]
    #[test_case(&[0xaa, 0xbb, 0xcc], [0, 0, 0, 0, 0, 0xaa, 0xbb, 0xcc] ; "short right-aligns")]
    #[test_case(&[0, 1, 2, 3, 4, 5, 6, 7], [0, 1, 2, 3, 4, 5, 6, 7] ; "exact length is identity")]
    fn parse_be_uint_pads(input: &[u8], expected: [u8; 8]) {
        assert_eq!(parse_be_uint::<8>(input).unwrap(), expected);
    }

    #[test_case(9 ; "one over")]
    #[test_case(16 ; "double")]
    fn parse_be_uint_rejects_overlong(len: usize) {
        let bytes = vec![0u8; len];
        assert_eq!(
            parse_be_uint::<8>(&bytes),
            Err(RlpError::TooLong {
                actual: len,
                max: 8
            })
        );
    }

    #[test]
    fn parse_fixed_exact() {
        let bytes = [0xabu8; 32];
        assert_eq!(parse_fixed::<32>(&bytes).unwrap(), bytes);
    }

    #[test_case(31; "too short")]
    #[test_case(33; "too long")]
    fn parse_fixed_rejects_wrong_length(len: usize) {
        let bytes = vec![0u8; len];
        assert_eq!(
            parse_fixed::<32>(&bytes),
            Err(RlpError::WrongLength {
                actual: len,
                expected: 32,
            })
        );
    }

    // ---------- parity with the upstream `rlp` crate ----------

    use ::rlp::RlpStream;

    fn upstream_encode(items: &[&[u8]]) -> Vec<u8> {
        let mut s = RlpStream::new_list(items.len());
        for item in items {
            s.append(&item.to_vec());
        }
        s.out().to_vec()
    }

    /// Encode the same flat byte-string list with both impls and confirm
    /// the bytes match exactly.
    #[test_case(&[] ; "empty list")]
    #[test_case(&[&[][..]] ; "list of one empty string")]
    #[test_case(&[&[0x42][..]] ; "single low byte child")]
    #[test_case(&[&[0xff][..]] ; "single high byte child")]
    #[test_case(&[b"hello", b"world"] ; "two short strings")]
    #[test_case(&[&[0u8; 100][..], &[1u8; 1][..]] ; "long child + short child")]
    #[test_case(&[&[0xff; 32][..]; 17] ; "17-element MPT branch shape")]
    fn parity_encode_matches_upstream(case: &[&[u8]]) {
        let items: Vec<RlpItem<'_>> = case.iter().map(|b| RlpItem::Bytes(b)).collect();
        assert_eq!(encode_list(&items).into_vec(), upstream_encode(case));
    }

    /// Encode with the upstream crate, decode with `RlpList`, expect the
    /// original byte-string children back.
    #[test_case(&[] ; "empty list")]
    #[test_case(&[&[0x42][..]] ; "single-byte child")]
    #[test_case(&[b"hello", b"world"] ; "two short strings")]
    #[test_case(&[&[0u8; 100][..], &[1u8; 1][..]] ; "long child + short child")]
    #[test_case(&[&[0xff; 32][..]; 17] ; "17-element MPT branch shape")]
    fn parity_decode_matches_upstream(case: &[&[u8]]) {
        let bytes = upstream_encode(case);
        let list = RlpList::parse(&bytes).unwrap();
        assert_eq!(list.fields().unwrap(), case.to_vec());
    }
}

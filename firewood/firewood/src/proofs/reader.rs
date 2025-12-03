// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::proofs::header::{Header, InvalidHeader};

pub(super) trait ReadItem<'a>: Sized {
    /// Reads an item from the given reader, or terrminates with an error.
    fn read_item(data: &mut ProofReader<'a>) -> Result<Self, ReadError>;
}

pub(super) trait Version0: Sized {
    fn read_v0_item(reader: &mut V0Reader<'_>) -> Result<Self, ReadError>;
}

pub(super) struct ProofReader<'a> {
    data: &'a [u8],
    offset: usize,
}

impl<'a> ProofReader<'a> {
    #[must_use]
    pub const fn new(data: &'a [u8]) -> Self {
        Self { data, offset: 0 }
    }

    pub fn read_chunk<const N: usize>(&mut self) -> Result<&'a [u8; N], ReadError> {
        if let Some((chunk, _)) = self.remainder().split_first_chunk::<N>() {
            #[expect(clippy::arithmetic_side_effects)]
            {
                self.offset += N;
            }
            Ok(chunk)
        } else {
            Err(self.incomplete_item(std::any::type_name::<[u8; N]>(), N))
        }
    }

    pub fn read_slice(&mut self, n: usize) -> Result<&'a [u8], ReadError> {
        if self.remainder().len() >= n {
            let (slice, _) = self.remainder().split_at(n);
            #[expect(clippy::arithmetic_side_effects)]
            {
                self.offset += n;
            }
            Ok(slice)
        } else {
            Err(self.incomplete_item("byte slice", n))
        }
    }

    pub(super) fn read_item<T: ReadItem<'a>>(&mut self) -> Result<T, ReadError> {
        T::read_item(self)
    }

    pub fn remainder(&self) -> &'a [u8] {
        #![expect(clippy::indexing_slicing)]
        &self.data[self.offset..]
    }

    pub fn advance(&mut self, n: usize) {
        #![expect(clippy::arithmetic_side_effects)]
        debug_assert!(self.offset + n <= self.data.len());
        self.offset += n;
    }

    #[must_use]
    pub const fn incomplete_item(&self, item: &'static str, expected: usize) -> ReadError {
        ReadError::IncompleteItem {
            item,
            offset: self.offset,
            expected,
            #[expect(clippy::arithmetic_side_effects)]
            found: self.data.len() - self.offset,
        }
    }

    #[must_use]
    pub fn invalid_item(
        &self,
        item: &'static str,
        expected: &'static str,
        found: impl ToString,
    ) -> ReadError {
        ReadError::InvalidItem {
            item,
            offset: self.offset,
            expected,
            found: found.to_string(),
        }
    }
}

pub(super) struct V0Reader<'a> {
    inner: ProofReader<'a>,
    header: Header,
}

impl<'a> V0Reader<'a> {
    #[must_use]
    pub fn new(inner: ProofReader<'a>, header: Header) -> Self {
        let this = Self { inner, header };
        debug_assert_eq!(this.header().version, 0);
        this
    }

    pub fn read_v0_item<T: Version0>(&mut self) -> Result<T, ReadError> {
        T::read_v0_item(self)
    }

    pub const fn header(&self) -> &Header {
        &self.header
    }
}

impl<'a> std::ops::Deref for V0Reader<'a> {
    type Target = ProofReader<'a>;

    fn deref(&self) -> &Self::Target {
        &self.inner
    }
}

impl std::ops::DerefMut for V0Reader<'_> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.inner
    }
}

/// Error that ocurred while reading an item from the byte stream.
#[derive(Debug, thiserror::Error)]
pub enum ReadError {
    /// Insufficient data in the byte stream.
    #[error("incomplete {item} at offset {offset}: expected {expected} bytes, but found {found}")]
    IncompleteItem {
        /// The specific item that was trying to parse.
        item: &'static str,
        /// The offset in the byte stream where the error ocurred.
        offset: usize,
        /// The expected length of the input (for this item).
        expected: usize,
        /// The number of bytes found in the byte stream.
        found: usize,
    },
    /// An item was invalid after parsing.
    #[error("invalid {item} at offset {offset}: expected {expected}, but found {found}")]
    InvalidItem {
        /// The item that was trying to parse.
        item: &'static str,
        /// The offset in the byte stream where the error ocurred.
        offset: usize,
        /// A hint at what was expected.
        expected: &'static str,
        /// Message indicating what was actually found.
        found: String,
    },
    /// Failed to validate the header.
    #[error("invalid header: {0}")]
    InvalidHeader(InvalidHeader),
}

impl ReadError {
    pub(super) const fn set_item(mut self, item: &'static str) -> Self {
        match &mut self {
            Self::IncompleteItem { item: e_item, .. } | Self::InvalidItem { item: e_item, .. } => {
                *e_item = item;
            }
            Self::InvalidHeader(_) => {}
        }
        self
    }
}

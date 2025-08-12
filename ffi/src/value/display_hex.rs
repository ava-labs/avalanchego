// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

/// Implementation of `Display` for a slice that displays the bytes in hexadecimal format.
///
/// If the `precision` is set, it will display only that many bytes in hex,
/// followed by an ellipsis and the number of remaining bytes.
pub struct DisplayHex<'a>(pub(super) &'a [u8]);

impl fmt::Display for DisplayHex<'_> {
    #[inline]
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        #![expect(clippy::indexing_slicing, clippy::arithmetic_side_effects)]

        match f.precision() {
            Some(p) if p < self.0.len() => {
                display_hex_bytes(&self.0[..p], f)?;
                f.write_fmt(format_args!("... ({} remaining bytes)", self.0.len() - p))?;
            }
            _ => display_hex_bytes(self.0, f)?,
        }

        Ok(())
    }
}

#[inline]
fn display_hex_bytes(bytes: &[u8], f: &mut fmt::Formatter<'_>) -> fmt::Result {
    const WIDTH: usize = size_of::<usize>() * 2;

    // SAFETY: it is trivially safe to transmute integer types, as long as the
    // offset is aligned, which `align_to` guarantees.
    let (before, aligned, after) = unsafe { bytes.align_to::<usize>() };

    for &byte in before {
        write!(f, "{byte:02x}")?;
    }

    for &word in aligned {
        let word = usize::from_be(word);
        write!(f, "{word:0WIDTH$x}")?;
    }

    for &byte in after {
        write!(f, "{byte:02x}")?;
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(feature = "ethhash")]
    use firewood::v2::api::HashKeyExt;
    use test_case::test_case;

    #[test_case(&[], "", None; "empty slice")]
    #[test_case(&[], "", Some(42); "empty slice with precision")]
    #[test_case(b"abc", "616263", None; "short slice")]
    #[test_case(b"abc", "61... (2 remaining bytes)", Some(1); "short slice with precision")]
    #[test_case(b"abc", "616263", Some(16); "short slice with long precision")]
    #[test_case(firewood_storage::TrieHash::empty().as_ref(), "0000000000000000000000000000000000000000000000000000000000000000", None; "empty trie hash")]
    #[test_case(firewood_storage::TrieHash::empty().as_ref(), "00000000000000000000000000000000... (16 remaining bytes)", Some(16); "empty trie hash with precision")]
    #[cfg_attr(feature = "ethhash", test_case(firewood_storage::TrieHash::default_root_hash().as_deref().expect("feature = \"ethhash\""), "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421", None; "empty rlp hash"))]
    #[cfg_attr(feature = "ethhash", test_case(firewood_storage::TrieHash::default_root_hash().as_ref().expect("feature = \"ethhash\""), "56e81f171bcc55a6ff8345e692c0f86e... (16 remaining bytes)", Some(16); "empty rlp hash with precision"))]
    fn test_display_hex(input: &[u8], expected: &str, precision: Option<usize>) {
        let input = DisplayHex(input);
        if let Some(p) = precision {
            assert_eq!(format!("{input:.p$}"), expected);
        } else {
            assert_eq!(format!("{input}"), expected);
        }
    }
}

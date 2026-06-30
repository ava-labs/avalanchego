// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Internal utilities for working with iterators.

/// Writes a limited number of items to a writer, separated by a specified separator.
///
/// - If `limit` is `Some(0)`, it will only write the number of hidden items.
/// - If `limit` is `Some(n)`, it will write at most `n` items, followed by a message
///   indicating how many more items were not written.
/// - If `limit` is `None`, it will write all items without any limit.
///   Caution: if limit is None, this function will not work with iterators that do not terminate.
///
/// # Arguments
/// - `writer`: The writer to which the items will be written.
/// - `iter`: An iterator of items that implement `std::fmt::Display`.
/// - `sep`: A separator that will be used between items.
/// - `limit`: An optional limit on the number of items to write.
///
/// # Returns
/// A `std::fmt::Result` indicating success or failure of the write operation.
pub(crate) fn write_limited_with_sep(
    writer: &mut (impl std::fmt::Write + ?Sized),
    iter: impl IntoIterator<Item: std::fmt::Display>,
    sep: impl std::fmt::Display,
    limit: Option<usize>,
) -> std::fmt::Result {
    match limit {
        Some(0) => {
            let hidden_count = iter.into_iter().count();
            write!(writer, "({hidden_count} hidden)")
        }
        Some(limit) => {
            let mut iter = iter.into_iter();
            let to_display_iter = iter.by_ref().take(limit);
            write_all_with_sep(writer, to_display_iter, &sep)?;

            let hidden_count = iter.count();
            if hidden_count > 0 {
                write!(writer, "{sep}... ({hidden_count} more hidden)")?;
            }
            Ok(())
        }
        None => write_all_with_sep(writer, iter, &sep),
    }
}

// Helper function that writes all items in the iterator.
// Caution: this function will not work with iterators that do not terminate.
fn write_all_with_sep(
    writer: &mut (impl std::fmt::Write + ?Sized),
    iter: impl IntoIterator<Item: std::fmt::Display>,
    sep: &impl std::fmt::Display,
) -> std::fmt::Result {
    let mut iter = iter.into_iter();
    if let Some(item) = iter.next() {
        write!(writer, "{item}")?;
        for item in iter {
            write!(writer, "{sep}{item}")?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use super::*;
    use test_case::test_case;

    #[test_case(Some(0usize), "(4 hidden)"; "with limit 0")]
    #[test_case(Some(2usize), "apple, banana, ... (2 more hidden)"; "with limit 2")]
    #[test_case(None, "apple, banana, cherry, date"; "without limit")]
    fn test_write_iter(limit: Option<usize>, expected: &str) {
        let mut output = String::new();
        let items = ["apple", "banana", "cherry", "date"];
        write_limited_with_sep(&mut output, &items, ", ", limit).unwrap();
        assert_eq!(output, expected);
    }
}

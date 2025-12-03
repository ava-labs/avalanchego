// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

/// An extension trait for extendable collections to handle iterators that yield
/// `Result<T, E>`.
pub(crate) trait TryExtend<T>: Extend<T> {
    /// Lazily collect an iterator over results into an extendable collection of the Ok value.
    ///
    /// Returns early if an error is encountered returning that error.
    fn try_extend<I: IntoIterator<Item = Result<T, E>>, E>(&mut self, iter: I) -> Result<(), E> {
        let mut error = None;

        self.extend(Shunt {
            iter: iter.into_iter(),
            error: &mut error,
        });

        if let Some(e) = error { Err(e) } else { Ok(()) }
    }
}

impl<C: Extend<T>, T> TryExtend<T> for C {}

struct Shunt<'a, I, E> {
    iter: I,
    error: &'a mut Option<E>,
}

impl<I: Iterator<Item = Result<T, E>>, T, E> Iterator for Shunt<'_, I, E> {
    type Item = T;

    fn next(&mut self) -> Option<Self::Item> {
        if self.error.is_some() {
            return None;
        }

        match self.iter.next() {
            Some(Ok(item)) => Some(item),
            Some(Err(e)) => {
                *self.error = Some(e);
                None
            }
            None => None,
        }
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        if self.error.is_some() {
            (0, Some(0))
        } else {
            let (_, upper) = self.iter.size_hint();
            (0, upper)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_try_extend() {
        let input = [Ok(0), Ok(1), Err("error"), Ok(3)];
        let mut collection = Vec::new();

        let mut iter = input.into_iter();
        let result = collection.try_extend(iter.by_ref());
        assert_eq!(result, Err("error"));
        assert_eq!(iter.next(), Some(Ok(3)));
        assert_eq!(iter.next(), None);
        assert_eq!(*collection, [0, 1]);
        // vec should allocate for 4 elements because of the size hint
        assert_eq!(collection.capacity(), 4);
    }
}

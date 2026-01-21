//! Error handling utilities.

use std::fmt;

/// A collection of errors that can be accumulated.
///
/// This is useful when you want to collect multiple errors
/// without stopping at the first one.
///
/// # Examples
///
/// ```
/// use avalanche_utils::errors::Errors;
///
/// let mut errs = Errors::new();
/// errs.add("first error");
/// errs.add("second error");
///
/// if errs.errored() {
///     println!("Errors: {}", errs);
/// }
/// ```
#[derive(Default)]
pub struct Errors {
    errors: Vec<String>,
}

impl Errors {
    /// Creates a new empty error collector.
    #[must_use]
    pub fn new() -> Self {
        Self { errors: Vec::new() }
    }

    /// Adds an error to the collection.
    pub fn add<E: fmt::Display>(&mut self, error: E) {
        self.errors.push(error.to_string());
    }

    /// Adds an error if the result is an error.
    pub fn add_result<T, E: fmt::Display>(&mut self, result: Result<T, E>) -> Option<T> {
        match result {
            Ok(v) => Some(v),
            Err(e) => {
                self.add(e);
                None
            }
        }
    }

    /// Returns `true` if any errors have been collected.
    #[must_use]
    pub fn errored(&self) -> bool {
        !self.errors.is_empty()
    }

    /// Returns the number of errors.
    #[must_use]
    pub fn len(&self) -> usize {
        self.errors.len()
    }

    /// Returns `true` if no errors have been collected.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.errors.is_empty()
    }

    /// Returns the collected errors as a slice.
    #[must_use]
    pub fn errors(&self) -> &[String] {
        &self.errors
    }

    /// Converts to a Result, returning Ok if no errors, Err otherwise.
    pub fn into_result(self) -> Result<(), ErrorCollection> {
        if self.errors.is_empty() {
            Ok(())
        } else {
            Err(ErrorCollection(self.errors))
        }
    }
}

impl fmt::Display for Errors {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.errors.len() {
            0 => write!(f, "no errors"),
            1 => write!(f, "{}", self.errors[0]),
            n => {
                write!(f, "{} errors: ", n)?;
                for (i, err) in self.errors.iter().enumerate() {
                    if i > 0 {
                        write!(f, "; ")?;
                    }
                    write!(f, "{}", err)?;
                }
                Ok(())
            }
        }
    }
}

impl fmt::Debug for Errors {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Errors")
            .field("errors", &self.errors)
            .finish()
    }
}

/// A collection of error strings, used as an error type.
#[derive(Debug)]
pub struct ErrorCollection(Vec<String>);

impl fmt::Display for ErrorCollection {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.0.len() {
            0 => write!(f, "no errors"),
            1 => write!(f, "{}", self.0[0]),
            n => {
                write!(f, "{} errors: ", n)?;
                for (i, err) in self.0.iter().enumerate() {
                    if i > 0 {
                        write!(f, "; ")?;
                    }
                    write!(f, "{}", err)?;
                }
                Ok(())
            }
        }
    }
}

impl std::error::Error for ErrorCollection {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_errors() {
        let errs = Errors::new();
        assert!(!errs.errored());
        assert!(errs.is_empty());
    }

    #[test]
    fn test_add_error() {
        let mut errs = Errors::new();
        errs.add("test error");
        assert!(errs.errored());
        assert_eq!(errs.len(), 1);
    }

    #[test]
    fn test_add_result_ok() {
        let mut errs = Errors::new();
        let value = errs.add_result(Ok::<i32, &str>(42));
        assert_eq!(value, Some(42));
        assert!(!errs.errored());
    }

    #[test]
    fn test_add_result_err() {
        let mut errs = Errors::new();
        let value = errs.add_result(Err::<i32, &str>("error"));
        assert_eq!(value, None);
        assert!(errs.errored());
    }

    #[test]
    fn test_display_no_errors() {
        let errs = Errors::new();
        assert_eq!(errs.to_string(), "no errors");
    }

    #[test]
    fn test_display_one_error() {
        let mut errs = Errors::new();
        errs.add("single error");
        assert_eq!(errs.to_string(), "single error");
    }

    #[test]
    fn test_display_multiple_errors() {
        let mut errs = Errors::new();
        errs.add("error 1");
        errs.add("error 2");
        let s = errs.to_string();
        assert!(s.contains("2 errors"));
        assert!(s.contains("error 1"));
        assert!(s.contains("error 2"));
    }

    #[test]
    fn test_into_result_ok() {
        let errs = Errors::new();
        assert!(errs.into_result().is_ok());
    }

    #[test]
    fn test_into_result_err() {
        let mut errs = Errors::new();
        errs.add("error");
        assert!(errs.into_result().is_err());
    }
}

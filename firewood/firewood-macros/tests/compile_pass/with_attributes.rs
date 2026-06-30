// Test that metrics macro works with other function attributes
use firewood_macros::metrics;

mod registry {
    pub const TEST_WITH_DOC: &str = "test_with_doc_total";
    pub const TEST_WITH_DOC_DURATION_SECONDS: &str = "test_with_doc_duration_seconds";
    pub const TEST_INLINE: &str = "test_inline_total";
    pub const TEST_INLINE_DURATION_SECONDS: &str = "test_inline_duration_seconds";
    pub const TEST_EXPECTED: &str = "test_expected_total";
    pub const TEST_EXPECTED_DURATION_SECONDS: &str = "test_expected_duration_seconds";
}

#[derive(Debug)]
struct TestError;

impl std::fmt::Display for TestError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "TestError")
    }
}

impl std::error::Error for TestError {}

#[metrics(TEST_WITH_DOC)]
/// This function has documentation
pub fn documented_function() -> Result<i32, TestError> {
    Ok(42)
}

#[inline]
#[metrics(TEST_INLINE)]
fn inline_function() -> Result<(), TestError> {
    Ok(())
}

#[expect(dead_code)]
#[metrics(TEST_EXPECTED)]
fn function_with_expect() -> Result<bool, TestError> {
    Ok(true)
}

fn main() {}

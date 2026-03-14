// Test that metrics macro works with other function attributes
use firewood_macros::metrics;

#[derive(Debug)]
struct TestError;

impl std::fmt::Display for TestError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "TestError")
    }
}

impl std::error::Error for TestError {}

#[metrics("test.with_doc", "documented function")]
/// This function has documentation
pub fn documented_function() -> Result<i32, TestError> {
    Ok(42)
}

#[inline]
#[metrics("test.inline")]
fn inline_function() -> Result<(), TestError> {
    Ok(())
}

#[allow(dead_code)]
#[metrics("test.allowed", "function with allow attribute")]
fn function_with_allow() -> Result<bool, TestError> {
    Ok(true)
}

fn main() {}

// Test that basic metrics macro usage compiles correctly
use firewood_macros::metrics;

mod registry {
    pub const TEST_BASIC: &str = "test_basic_total";
    pub const TEST_BASIC_DURATION_SECONDS: &str = "test_basic_duration_seconds";
    pub const TEST_WITH_DESCRIPTION: &str = "test_with_description_total";
    pub const TEST_WITH_DESCRIPTION_DURATION_SECONDS: &str =
        "test_with_description_duration_seconds";
    pub const TEST_COMPLEX: &str = "test_complex_total";
    pub const TEST_COMPLEX_DURATION_SECONDS: &str = "test_complex_duration_seconds";
}

#[metrics(TEST_BASIC)]
fn test_basic_function() -> Result<(), &'static str> {
    Ok(())
}

#[metrics(TEST_WITH_DESCRIPTION)]
fn test_function_with_description() -> Result<String, Box<dyn std::error::Error>> {
    Ok("success".to_owned())
}

#[metrics(TEST_COMPLEX)]
async fn test_async_function() -> Result<Vec<u8>, std::io::Error> {
    Ok(vec![1, 2, 3])
}

fn main() {
    // These functions should compile but we don't need to call them
    // since this is just a compilation test
}

// Test that basic metrics macro usage compiles correctly
use firewood_macros::metrics;

#[metrics("test.basic")]
fn test_basic_function() -> Result<(), &'static str> {
    Ok(())
}

#[metrics("test.with_description", "test operation")]
fn test_function_with_description() -> Result<String, Box<dyn std::error::Error>> {
    Ok("success".to_string())
}

#[metrics("test.complex")]
async fn test_async_function() -> Result<Vec<u8>, std::io::Error> {
    Ok(vec![1, 2, 3])
}

fn main() {
    // These functions should compile but we don't need to call them
    // since this is just a compilation test
}

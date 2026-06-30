// Test that metrics macro fails with invalid arguments
use firewood_macros::metrics;

#[metrics(123)]
fn function_with_invalid_arg() -> Result<(), &'static str> {
    Ok(())
}

fn main() {}

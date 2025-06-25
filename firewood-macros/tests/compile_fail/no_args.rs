// Test that metrics macro fails when no arguments are provided
use firewood_macros::metrics;

#[metrics()]
fn function_without_args() -> Result<(), &'static str> {
    Ok(())
}

fn main() {}

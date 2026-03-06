// Test that metrics macro fails when function doesn't return Result
use firewood_macros::metrics;

#[metrics("test.invalid")]
fn function_without_result() -> i32 {
    42
}

fn main() {}

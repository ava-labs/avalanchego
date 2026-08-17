// Test that metrics macro fails when function doesn't return Result
use firewood_macros::metrics;

#[metrics(TEST_INVALID)]
fn function_without_result() -> i32 {
    42
}

fn main() {}

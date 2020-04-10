static mut COUNT: i32 = 0;

extern "C" {
    fn extSum(x: i32, y: i32) -> i32;
}

#[no_mangle]
pub extern fn sum(x: i32, y: i32) -> i32 {
    unsafe {extSum(x,y)}
}

#[no_mangle]
pub extern fn inc() {
    unsafe {COUNT += 1}
}

#[no_mangle]
pub extern fn getCount() -> i32 {
    unsafe {return COUNT}
}
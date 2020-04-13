static mut COUNT: i32 = 0;

extern "C" {
    fn externalDec(count: i32) -> i32;
}

#[no_mangle]
pub extern fn dec() {
    unsafe {COUNT = externalDec(COUNT)}
}

#[no_mangle]
pub extern fn inc() {
    unsafe {COUNT += 1}
}

#[no_mangle]
pub extern fn getCount() -> i32 {
    unsafe {return COUNT}
}

#[no_mangle]
pub extern fn add(x: i32) {
    unsafe {COUNT += x}
}
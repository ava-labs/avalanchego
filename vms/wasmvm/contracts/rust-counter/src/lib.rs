use lazy_static::lazy_static;
use std::sync::Mutex;

lazy_static! {
    static ref COUNT: Mutex<i32> = Mutex::new(0);
}

extern "C" {
    fn externalDec(count: i32) -> i32;
}

#[no_mangle]
pub extern fn dec() {
    unsafe {
        let mut count = COUNT.lock().unwrap();
        *count = externalDec(*count);
    }
}

#[no_mangle]
pub extern fn inc() {
    unsafe {*COUNT.lock().unwrap() += 1}
}

#[no_mangle]
pub extern fn getCount() -> i32 {
    unsafe {return *COUNT.lock().unwrap()}
}

#[no_mangle]
pub extern fn add(x: i32) {
    unsafe {*COUNT.lock().unwrap() += x}
}
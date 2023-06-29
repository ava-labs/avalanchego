#[allow(dead_code)]
pub fn get_raw_bytes<T: Sized>(data: &T) -> Vec<u8> {
    unsafe {
        std::slice::from_raw_parts(data as *const T as *const u8, std::mem::size_of::<T>()).to_vec()
    }
}

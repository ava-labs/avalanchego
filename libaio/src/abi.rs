// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// libaio ABI adapted from:
// https://raw.githubusercontent.com/jsgf/libaio-rust/master/src/aioabi.rs
#![allow(dead_code)]

pub use libc::timespec;
use libc::{c_int, c_long, size_t};
use std::mem::zeroed;

#[repr(C)]
pub enum IoCmd {
    PRead = 0,
    PWrite = 1,
    FSync = 2,
    FdSync = 3,
    // 4 was the experimental IOCB_CMD_PREADX,
    Poll = 5,
    Noop = 6,
    PReadV = 7,
    PWriteV = 8,
}

pub const IOCB_FLAG_RESFD: u32 = 1 << 0;
pub const IOCB_FLAG_IOPRIO: u32 = 1 << 1;

// Taken from linux/include/linux/aio_abi.h
// This is a kernel ABI, so there should be no need to worry about it changing.
#[repr(C)]
pub struct IoCb {
    pub aio_data: u64, // ends up in io_event.data
    // NOTE: the order of aio_key and aio_rw_flags could be byte-order depedent
    pub aio_key: u32,
    pub aio_rw_flags: u32,

    pub aio_lio_opcode: u16,
    pub aio_reqprio: u16,
    pub aio_fildes: u32,

    pub aio_buf: u64,
    pub aio_nbytes: u64,
    pub aio_offset: u64,

    pub aio_reserved2: u64,
    pub aio_flags: u32,
    pub aio_resfd: u32,
}

impl Default for IoCb {
    fn default() -> IoCb {
        IoCb {
            aio_lio_opcode: IoCmd::Noop as u16,
            aio_fildes: (-1_i32) as u32,
            ..unsafe { zeroed() }
        }
    }
}

#[derive(Clone)]
#[repr(C)]
pub struct IoEvent {
    pub data: u64,
    pub obj: u64,
    pub res: i64,
    pub res2: i64,
}

impl Default for IoEvent {
    fn default() -> IoEvent {
        unsafe { zeroed() }
    }
}

pub enum IoContext {}
pub type IoContextPtr = *mut IoContext;

#[repr(C)]
pub struct IoVector {
    pub iov_base: *mut u8,
    pub iov_len: size_t,
}

#[link(name = "aio", kind = "static")]
extern "C" {
    pub fn io_queue_init(maxevents: c_int, ctxp: *mut IoContextPtr) -> c_int;
    pub fn io_queue_release(ctx: IoContextPtr) -> c_int;
    pub fn io_queue_run(ctx: IoContextPtr) -> c_int;
    pub fn io_setup(maxevents: c_int, ctxp: *mut IoContextPtr) -> c_int;
    pub fn io_destroy(ctx: IoContextPtr) -> c_int;
    pub fn io_submit(ctx: IoContextPtr, nr: c_long, ios: *mut *mut IoCb) -> c_int;
    pub fn io_cancel(ctx: IoContextPtr, iocb: *mut IoCb, evt: *mut IoEvent) -> c_int;
    pub fn io_getevents(
        ctx_id: IoContextPtr,
        min_nr: c_long,
        nr: c_long,
        events: *mut IoEvent,
        timeout: *mut timespec,
    ) -> c_int;
    pub fn io_set_eventfd(iocb: *mut IoCb, eventfd: c_int);
}

#[cfg(test)]
mod test {
    use std::mem::size_of;

    #[test]
    fn test_sizes() {
        // Check against kernel ABI
        assert!(size_of::<super::IoEvent>() == 32);
        assert!(size_of::<super::IoCb>() == 64);
    }
}

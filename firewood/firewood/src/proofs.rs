// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod bitmap;
mod de;
mod header;
mod proof_type;
mod reader;
mod ser;
#[cfg(test)]
mod tests;

pub use self::header::InvalidHeader;
pub use self::reader::ReadError;

mod magic {
    pub const PROOF_HEADER: &[u8; 8] = b"fwdproof";

    pub const PROOF_VERSION: u8 = 0;

    #[cfg(not(feature = "ethhash"))]
    pub const HASH_MODE: u8 = 0;
    #[cfg(feature = "ethhash")]
    pub const HASH_MODE: u8 = 1;

    pub const fn hash_mode_name(v: u8) -> &'static str {
        match v {
            0 => "sha256",
            1 => "keccak256",
            _ => "unknown",
        }
    }

    #[cfg(not(feature = "branch_factor_256"))]
    pub const BRANCH_FACTOR: u8 = 16;
    #[cfg(feature = "branch_factor_256")]
    pub const BRANCH_FACTOR: u8 = 0; // 256 wrapped to 0

    pub const fn widen_branch_factor(v: u8) -> u16 {
        match v {
            0 => 256,
            _ => v as u16,
        }
    }
}

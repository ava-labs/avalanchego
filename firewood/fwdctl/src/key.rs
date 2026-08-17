// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::api;

#[cfg(feature = "ethhash")]
use sha3::{Digest, Keccak256};

#[derive(Debug, Args)]
pub struct KeyArgument {
    /// The key. Used as UTF-8 unless `--hex` or an Ethereum hashing option is set
    #[arg(
        required = true,
        value_name = "KEY",
        help = "Key to operate on (UTF-8 by default; decoded or hashed when a key mode is set)"
    )]
    key: String,

    /// Decode KEY as hexadecimal bytes
    #[arg(long)]
    hex: bool,

    /// Hash KEY as a 20-byte hex Ethereum account address
    #[cfg(feature = "ethhash")]
    #[arg(long, conflicts_with_all = ["hex", "storage"])]
    account: bool,

    /// Hash KEY as a 20-byte hex Ethereum account address and SLOT as a 32-byte hex storage key
    #[cfg(feature = "ethhash")]
    #[arg(long, value_name = "SLOT", conflicts_with_all = ["hex", "account"])]
    storage: Option<String>,
}

impl KeyArgument {
    /// Converts the CLI key input into the key stored by Firewood.
    ///
    /// # Errors
    ///
    /// Returns an invalid-input error when an Ethereum account or storage key
    /// is not valid hexadecimal of the required length.
    pub fn database_key(&self) -> Result<Vec<u8>, api::Error> {
        if self.hex {
            let hex_input = self.key.strip_prefix("0x").unwrap_or(&self.key);
            return hex::decode(hex_input)
                .map_err(|error| invalid_input(format!("key must be hexadecimal: {error}")));
        }

        #[cfg(feature = "ethhash")]
        {
            if self.account {
                return Ok(keccak256(decode_hex_exact("account", &self.key, 20)?).to_vec());
            }

            if let Some(storage) = &self.storage {
                let account_hash = keccak256(decode_hex_exact("account", &self.key, 20)?);
                let storage_hash = keccak256(decode_hex_exact("storage key", storage, 32)?);
                let mut key = Vec::with_capacity(64);
                key.extend_from_slice(&account_hash);
                key.extend_from_slice(&storage_hash);
                return Ok(key);
            }
        }

        Ok(self.key.as_bytes().to_vec())
    }
}

#[cfg(feature = "ethhash")]
fn decode_hex_exact(
    label: &str,
    input: &str,
    expected_bytes: usize,
) -> Result<Vec<u8>, api::Error> {
    let hex_input = input.strip_prefix("0x").unwrap_or(input);
    let expected_digits = expected_bytes.saturating_mul(2);

    if hex_input.len() != expected_digits {
        let actual = if hex_input.len().is_multiple_of(2) {
            format!("{} bytes", hex_input.len() / 2)
        } else {
            format!("{} hex digits", hex_input.len())
        };
        return Err(invalid_input(format!(
            "{label} must be exactly {expected_bytes} bytes ({expected_digits} hex digits); got {actual}"
        )));
    }

    hex::decode(hex_input).map_err(|error| {
        invalid_input(format!(
            "{label} must be exactly {expected_bytes} bytes of hexadecimal: {error}"
        ))
    })
}

#[cfg(feature = "ethhash")]
fn keccak256(input: impl AsRef<[u8]>) -> [u8; 32] {
    Keccak256::digest(input).into()
}

fn invalid_input(message: String) -> api::Error {
    std::io::Error::new(std::io::ErrorKind::InvalidInput, message).into()
}

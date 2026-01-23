//! EVM precompiled contracts.
//!
//! Implements standard Ethereum precompiles plus Avalanche-specific ones:
//! - Standard: ecrecover, sha256, ripemd160, identity, modexp, etc.
//! - Avalanche: Native minter, fee manager, reward manager, warp messenger

use alloy_primitives::{Address, Bytes, B256, U256};
use k256::ecdsa::{RecoveryId, Signature, VerifyingKey};
use sha2::{Digest, Sha256};
use sha3::Keccak256;

/// Precompile result.
pub type PrecompileResult = Result<PrecompileOutput, PrecompileError>;

/// Precompile output.
#[derive(Debug, Clone)]
pub struct PrecompileOutput {
    /// Output data.
    pub data: Bytes,
    /// Gas used.
    pub gas_used: u64,
}

/// Precompile error.
#[derive(Debug, Clone, thiserror::Error)]
pub enum PrecompileError {
    #[error("out of gas")]
    OutOfGas,
    #[error("invalid input")]
    InvalidInput,
    #[error("internal error: {0}")]
    Internal(String),
}

/// Precompile addresses.
pub mod addresses {
    use alloy_primitives::Address;

    /// ECRecover (0x01)
    pub const ECRECOVER: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01,
    ]);

    /// SHA256 (0x02)
    pub const SHA256: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02,
    ]);

    /// RIPEMD160 (0x03)
    pub const RIPEMD160: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x03,
    ]);

    /// Identity (0x04)
    pub const IDENTITY: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x04,
    ]);

    /// ModExp (0x05)
    pub const MODEXP: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x05,
    ]);

    /// BN256Add (0x06)
    pub const BN256_ADD: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x06,
    ]);

    /// BN256Mul (0x07)
    pub const BN256_MUL: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x07,
    ]);

    /// BN256Pairing (0x08)
    pub const BN256_PAIRING: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x08,
    ]);

    /// Blake2F (0x09)
    pub const BLAKE2F: Address = Address::new([
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x09,
    ]);

    // Avalanche-specific precompiles (0x0100+)

    /// Native Minter (0x0200000000000000000000000000000000000001)
    pub const NATIVE_MINTER: Address = Address::new([
        0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01,
    ]);

    /// Fee Manager (0x0200000000000000000000000000000000000003)
    pub const FEE_MANAGER: Address = Address::new([
        0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x03,
    ]);

    /// Reward Manager (0x0200000000000000000000000000000000000004)
    pub const REWARD_MANAGER: Address = Address::new([
        0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x04,
    ]);

    /// Warp Messenger (0x0200000000000000000000000000000000000005)
    pub const WARP_MESSENGER: Address = Address::new([
        0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x05,
    ]);
}

/// Precompile executor.
pub struct Precompiles;

impl Precompiles {
    /// Checks if an address is a precompile.
    pub fn is_precompile(addr: &Address) -> bool {
        let bytes = addr.0.0;

        // Standard precompiles 0x01-0x09
        if bytes[..19] == [0u8; 19] && bytes[19] >= 1 && bytes[19] <= 9 {
            return true;
        }

        // Avalanche precompiles 0x02...
        if bytes[0] == 0x02 && bytes[1..19] == [0u8; 18] {
            return true;
        }

        false
    }

    /// Executes a precompile.
    pub fn execute(addr: &Address, input: &[u8], gas_limit: u64) -> PrecompileResult {
        use addresses::*;

        match *addr {
            ECRECOVER => Self::ecrecover(input, gas_limit),
            SHA256 => Self::sha256(input, gas_limit),
            RIPEMD160 => Self::ripemd160(input, gas_limit),
            IDENTITY => Self::identity(input, gas_limit),
            MODEXP => Self::modexp(input, gas_limit),
            BN256_ADD => Self::bn256_add(input, gas_limit),
            BN256_MUL => Self::bn256_mul(input, gas_limit),
            BN256_PAIRING => Self::bn256_pairing(input, gas_limit),
            BLAKE2F => Self::blake2f(input, gas_limit),
            NATIVE_MINTER => Self::native_minter(input, gas_limit),
            FEE_MANAGER => Self::fee_manager(input, gas_limit),
            REWARD_MANAGER => Self::reward_manager(input, gas_limit),
            WARP_MESSENGER => Self::warp_messenger(input, gas_limit),
            _ => Err(PrecompileError::Internal("unknown precompile".to_string())),
        }
    }

    /// ECRecover: Recover signer from signature.
    fn ecrecover(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 3000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Pad input to 128 bytes
        let mut padded = [0u8; 128];
        let len = input.len().min(128);
        padded[..len].copy_from_slice(&input[..len]);

        let hash = B256::from_slice(&padded[0..32]);
        let v = padded[63];
        let r = &padded[64..96];
        let s = &padded[96..128];

        // Recovery ID is v - 27
        let recovery_id = match v {
            27 => RecoveryId::new(false, false),
            28 => RecoveryId::new(true, false),
            _ => {
                return Ok(PrecompileOutput {
                    data: Bytes::from(vec![0u8; 32]),
                    gas_used: GAS_COST,
                });
            }
        };

        // Try to create signature from r and s
        let r_bytes: [u8; 32] = r.try_into().unwrap_or([0u8; 32]);
        let s_bytes: [u8; 32] = s.try_into().unwrap_or([0u8; 32]);

        let signature = match Signature::from_scalars(r_bytes, s_bytes) {
            Ok(sig) => sig,
            Err(_) => {
                return Ok(PrecompileOutput {
                    data: Bytes::from(vec![0u8; 32]),
                    gas_used: GAS_COST,
                });
            }
        };

        match VerifyingKey::recover_from_prehash(hash.as_slice(), &signature, recovery_id) {
            Ok(pubkey) => {
                let pubkey_bytes = pubkey.to_encoded_point(false);
                let pubkey_hash = keccak256(&pubkey_bytes.as_bytes()[1..]);
                let mut result = [0u8; 32];
                result[12..32].copy_from_slice(&pubkey_hash.as_slice()[12..32]);
                Ok(PrecompileOutput {
                    data: Bytes::from(result.to_vec()),
                    gas_used: GAS_COST,
                })
            }
            Err(_) => Ok(PrecompileOutput {
                data: Bytes::from(vec![0u8; 32]),
                gas_used: GAS_COST,
            }),
        }
    }

    /// SHA256 hash.
    fn sha256(input: &[u8], gas_limit: u64) -> PrecompileResult {
        let gas_cost = 60 + 12 * ((input.len() as u64 + 31) / 32);

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        let hash = Sha256::digest(input);
        Ok(PrecompileOutput {
            data: Bytes::from(hash.to_vec()),
            gas_used: gas_cost,
        })
    }

    /// RIPEMD160 hash.
    fn ripemd160(input: &[u8], gas_limit: u64) -> PrecompileResult {
        let gas_cost = 600 + 120 * ((input.len() as u64 + 31) / 32);

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        use ripemd::Ripemd160;
        let hash = Ripemd160::digest(input);

        // Left-pad to 32 bytes
        let mut result = [0u8; 32];
        result[12..32].copy_from_slice(&hash);

        Ok(PrecompileOutput {
            data: Bytes::from(result.to_vec()),
            gas_used: gas_cost,
        })
    }

    /// Identity: returns input as-is.
    fn identity(input: &[u8], gas_limit: u64) -> PrecompileResult {
        let gas_cost = 15 + 3 * ((input.len() as u64 + 31) / 32);

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        Ok(PrecompileOutput {
            data: Bytes::from(input.to_vec()),
            gas_used: gas_cost,
        })
    }

    /// ModExp: modular exponentiation.
    fn modexp(input: &[u8], gas_limit: u64) -> PrecompileResult {
        // Parse lengths
        let mut padded = vec![0u8; input.len().max(96)];
        padded[..input.len()].copy_from_slice(input);

        let base_len = U256::from_be_slice(&padded[0..32]).saturating_to::<usize>();
        let exp_len = U256::from_be_slice(&padded[32..64]).saturating_to::<usize>();
        let mod_len = U256::from_be_slice(&padded[64..96]).saturating_to::<usize>();

        // Gas calculation (simplified)
        let max_len = base_len.max(mod_len);
        let words = (max_len + 7) / 8;
        let gas_cost = (words * words * exp_len.max(1)) as u64 / 3;
        let gas_cost = gas_cost.max(200);

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        // Extract values
        let data_start = 96;
        let base_end = data_start + base_len;
        let exp_end = base_end + exp_len;
        let mod_end = exp_end + mod_len;

        let mut extended = padded.clone();
        extended.resize(mod_end.max(padded.len()), 0);

        let base = &extended[data_start..base_end.min(extended.len())];
        let exp = &extended[base_end..exp_end.min(extended.len())];
        let modulus = &extended[exp_end..mod_end.min(extended.len())];

        // Compute modexp using simple implementation
        if modulus.iter().all(|&b| b == 0) {
            return Ok(PrecompileOutput {
                data: Bytes::from(vec![0u8; mod_len]),
                gas_used: gas_cost,
            });
        }

        let base_num = U256::from_be_slice(base);
        let exp_num = U256::from_be_slice(exp);
        let mod_num = U256::from_be_slice(modulus);

        let result = mod_exp(base_num, exp_num, mod_num);
        let mut result_bytes = vec![0u8; mod_len];
        result.to_be_bytes_vec().iter().rev().enumerate().for_each(|(i, &b)| {
            if i < mod_len {
                result_bytes[mod_len - 1 - i] = b;
            }
        });

        Ok(PrecompileOutput {
            data: Bytes::from(result_bytes),
            gas_used: gas_cost,
        })
    }

    /// BN256 Add (alt_bn128).
    fn bn256_add(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 150;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Stub implementation - would use bn256 crate in production
        let result = vec![0u8; 64];
        Ok(PrecompileOutput {
            data: Bytes::from(result),
            gas_used: GAS_COST,
        })
    }

    /// BN256 Scalar Mul.
    fn bn256_mul(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 6000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Stub implementation
        let result = vec![0u8; 64];
        Ok(PrecompileOutput {
            data: Bytes::from(result),
            gas_used: GAS_COST,
        })
    }

    /// BN256 Pairing check.
    fn bn256_pairing(input: &[u8], gas_limit: u64) -> PrecompileResult {
        let num_pairs = input.len() / 192;
        let gas_cost = 45000 + 34000 * num_pairs as u64;

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        // Stub: return success (1)
        let mut result = vec![0u8; 32];
        result[31] = 1;
        Ok(PrecompileOutput {
            data: Bytes::from(result),
            gas_used: gas_cost,
        })
    }

    /// Blake2F compression.
    fn blake2f(input: &[u8], gas_limit: u64) -> PrecompileResult {
        if input.len() != 213 {
            return Err(PrecompileError::InvalidInput);
        }

        let rounds = u32::from_be_bytes([input[0], input[1], input[2], input[3]]);
        let gas_cost = rounds as u64;

        if gas_limit < gas_cost {
            return Err(PrecompileError::OutOfGas);
        }

        // Stub implementation
        let result = vec![0u8; 64];
        Ok(PrecompileOutput {
            data: Bytes::from(result),
            gas_used: gas_cost,
        })
    }

    // Avalanche-specific precompiles

    /// Native Minter: mint native tokens.
    fn native_minter(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 10000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Requires admin role - return success for now
        Ok(PrecompileOutput {
            data: Bytes::new(),
            gas_used: GAS_COST,
        })
    }

    /// Fee Manager: manage dynamic fees.
    fn fee_manager(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 5000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Returns current fee config
        Ok(PrecompileOutput {
            data: Bytes::new(),
            gas_used: GAS_COST,
        })
    }

    /// Reward Manager: manage staking rewards.
    fn reward_manager(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 5000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        Ok(PrecompileOutput {
            data: Bytes::new(),
            gas_used: GAS_COST,
        })
    }

    /// Warp Messenger: cross-subnet messaging.
    fn warp_messenger(input: &[u8], gas_limit: u64) -> PrecompileResult {
        const GAS_COST: u64 = 20000;

        if gas_limit < GAS_COST {
            return Err(PrecompileError::OutOfGas);
        }

        // Parse function selector
        if input.len() < 4 {
            return Err(PrecompileError::InvalidInput);
        }

        let selector = &input[0..4];

        match selector {
            // sendWarpMessage(bytes)
            [0x3e, 0x61, 0x28, 0xc8] => {
                // Return message hash
                let hash = keccak256(&input[4..]);
                Ok(PrecompileOutput {
                    data: Bytes::from(hash.to_vec()),
                    gas_used: GAS_COST,
                })
            }
            // getVerifiedWarpMessage(uint32)
            [0x6f, 0x05, 0x6a, 0x11] => {
                // Return empty message for now
                Ok(PrecompileOutput {
                    data: Bytes::new(),
                    gas_used: GAS_COST,
                })
            }
            // getVerifiedWarpBlockHash(uint32)
            [0x0b, 0x68, 0xdc, 0x00] => {
                Ok(PrecompileOutput {
                    data: Bytes::from(vec![0u8; 32]),
                    gas_used: GAS_COST,
                })
            }
            _ => Err(PrecompileError::InvalidInput),
        }
    }
}

/// Keccak256 hash.
fn keccak256(data: &[u8]) -> B256 {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

/// Modular exponentiation.
fn mod_exp(base: U256, exp: U256, modulus: U256) -> U256 {
    if modulus.is_zero() {
        return U256::ZERO;
    }
    if exp.is_zero() {
        return U256::from(1) % modulus;
    }

    let mut result = U256::from(1);
    let mut base = base % modulus;
    let mut exp = exp;

    while exp > U256::ZERO {
        if exp & U256::from(1) == U256::from(1) {
            result = (result * base) % modulus;
        }
        exp >>= 1;
        base = (base * base) % modulus;
    }

    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_precompile() {
        assert!(Precompiles::is_precompile(&addresses::ECRECOVER));
        assert!(Precompiles::is_precompile(&addresses::SHA256));
        assert!(Precompiles::is_precompile(&addresses::WARP_MESSENGER));
        assert!(!Precompiles::is_precompile(&Address::ZERO));
    }

    #[test]
    fn test_sha256() {
        let input = b"hello world";
        let result = Precompiles::sha256(input, 1000).unwrap();
        assert_eq!(result.data.len(), 32);
    }

    #[test]
    fn test_identity() {
        let input = b"test data";
        let result = Precompiles::identity(input, 1000).unwrap();
        assert_eq!(result.data.as_ref(), input);
    }

    #[test]
    fn test_ripemd160() {
        let input = b"hello";
        let result = Precompiles::ripemd160(input, 10000).unwrap();
        assert_eq!(result.data.len(), 32);
    }

    #[test]
    fn test_out_of_gas() {
        let result = Precompiles::sha256(b"test", 10);
        assert!(matches!(result, Err(PrecompileError::OutOfGas)));
    }

    #[test]
    fn test_mod_exp() {
        let base = U256::from(2);
        let exp = U256::from(10);
        let modulus = U256::from(1000);
        let result = mod_exp(base, exp, modulus);
        assert_eq!(result, U256::from(24)); // 2^10 mod 1000 = 1024 mod 1000 = 24
    }

    #[test]
    fn test_warp_messenger() {
        // sendWarpMessage selector
        let mut input = vec![0x3e, 0x61, 0x28, 0xc8];
        input.extend_from_slice(b"test message");

        let result = Precompiles::warp_messenger(&input, 50000).unwrap();
        assert_eq!(result.data.len(), 32); // Returns message hash
    }
}

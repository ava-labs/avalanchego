//! Codec manager for versioned serialization.
//!
//! This module provides codec versioning compatible with the Go implementation.
//! The codec manager tracks multiple codec versions and handles the version
//! prefix in serialized messages.

use std::collections::HashMap;

use crate::{Pack, Packer, Unpack, UnpackError, Unpacker};

/// Current default codec version.
pub const DEFAULT_CODEC_VERSION: u16 = 0;

/// Maximum supported codec version.
pub const MAX_CODEC_VERSION: u16 = 0;

/// Type ID for registered types (4 bytes).
pub type TypeId = u32;

/// A codec with type registration for union/interface types.
#[derive(Debug, Default)]
pub struct Codec {
    /// Type name to ID mapping.
    type_ids: HashMap<&'static str, TypeId>,
    /// ID to type name mapping (for debugging).
    type_names: HashMap<TypeId, &'static str>,
    /// Next available type ID.
    next_id: TypeId,
}

impl Codec {
    /// Creates a new empty codec.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Registers a type and returns its ID.
    ///
    /// Types must be registered in the same order as in the Go implementation
    /// to ensure compatible type IDs.
    pub fn register_type(&mut self, name: &'static str) -> TypeId {
        let id = self.next_id;
        self.type_ids.insert(name, id);
        self.type_names.insert(id, name);
        self.next_id += 1;
        id
    }

    /// Returns the type ID for a type name.
    #[must_use]
    pub fn type_id(&self, name: &str) -> Option<TypeId> {
        self.type_ids.get(name).copied()
    }

    /// Returns the type name for a type ID.
    #[must_use]
    pub fn type_name(&self, id: TypeId) -> Option<&'static str> {
        self.type_names.get(&id).copied()
    }
}

/// Manages multiple codec versions.
#[derive(Debug, Default)]
pub struct CodecManager {
    /// Registered codecs by version.
    codecs: HashMap<u16, Codec>,
}

impl CodecManager {
    /// Creates a new codec manager.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Registers a codec for a specific version.
    pub fn register_codec(&mut self, version: u16, codec: Codec) {
        self.codecs.insert(version, codec);
    }

    /// Returns the codec for a specific version.
    #[must_use]
    pub fn codec(&self, version: u16) -> Option<&Codec> {
        self.codecs.get(&version)
    }

    /// Returns the codec for a specific version (mutable).
    pub fn codec_mut(&mut self, version: u16) -> Option<&mut Codec> {
        self.codecs.get_mut(&version)
    }

    /// Returns the current (latest) codec.
    #[must_use]
    pub fn current(&self) -> Option<&Codec> {
        self.codecs.get(&DEFAULT_CODEC_VERSION)
    }

    /// Serializes a value with version prefix.
    pub fn marshal<T: Pack>(&self, value: &T) -> Vec<u8> {
        self.marshal_version(DEFAULT_CODEC_VERSION, value)
    }

    /// Serializes a value with a specific version prefix.
    pub fn marshal_version<T: Pack>(&self, version: u16, value: &T) -> Vec<u8> {
        let mut packer = Packer::new(256);
        // Write version prefix
        packer.pack_short(version);
        // Write value
        value.pack(&mut packer);
        packer.into_bytes()
    }

    /// Deserializes a value, extracting the version.
    ///
    /// Returns the version and the deserialized value.
    pub fn unmarshal<T: Unpack>(&self, bytes: &[u8]) -> Result<(u16, T), UnpackError> {
        let mut unpacker = Unpacker::new(bytes);
        let version = unpacker.unpack_short()?;

        if version > MAX_CODEC_VERSION {
            return Err(UnpackError::InsufficientBytes {
                needed: 0,
                remaining: 0,
            });
        }

        let value = T::unpack(&mut unpacker)?;
        Ok((version, value))
    }
}

/// Trait for types that can be serialized with a type ID prefix.
pub trait TypedPack: Pack {
    /// Returns the type name for registration.
    fn type_name() -> &'static str;

    /// Packs this value with its type ID prefix.
    fn pack_typed(&self, packer: &mut Packer, type_id: TypeId) {
        packer.pack_int(type_id);
        self.pack(packer);
    }
}

/// Trait for types that can be deserialized based on type ID.
pub trait TypedUnpack: Unpack {
    /// Returns the expected type ID.
    fn expected_type_id() -> TypeId;
}

/// Helper for packing typed values.
pub fn pack_with_type_id<T: Pack>(packer: &mut Packer, type_id: TypeId, value: &T) {
    packer.pack_int(type_id);
    value.pack(packer);
}

/// Helper for unpacking type ID.
pub fn unpack_type_id(unpacker: &mut Unpacker) -> Result<TypeId, UnpackError> {
    unpacker.unpack_int()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_codec_type_registration() {
        let mut codec = Codec::new();

        let tx_id = codec.register_type("BaseTx");
        let create_id = codec.register_type("CreateAssetTx");

        assert_eq!(tx_id, 0);
        assert_eq!(create_id, 1);
        assert_eq!(codec.type_id("BaseTx"), Some(0));
        assert_eq!(codec.type_id("CreateAssetTx"), Some(1));
        assert_eq!(codec.type_name(0), Some("BaseTx"));
        assert_eq!(codec.type_name(1), Some("CreateAssetTx"));
    }

    #[test]
    fn test_codec_manager() {
        let mut manager = CodecManager::new();
        let codec = Codec::new();
        manager.register_codec(0, codec);

        assert!(manager.codec(0).is_some());
        assert!(manager.current().is_some());
    }

    #[test]
    fn test_versioned_marshal() {
        let manager = CodecManager::new();

        let value: u32 = 0x12345678;
        let bytes = manager.marshal(&value);

        // Should be: version (2 bytes) + value (4 bytes)
        assert_eq!(bytes, &[0x00, 0x00, 0x12, 0x34, 0x56, 0x78]);
    }

    #[test]
    fn test_versioned_unmarshal() {
        let manager = CodecManager::new();

        let bytes = [0x00, 0x00, 0x12, 0x34, 0x56, 0x78];
        let (version, value): (u16, u32) = manager.unmarshal(&bytes).unwrap();

        assert_eq!(version, 0);
        assert_eq!(value, 0x12345678);
    }
}

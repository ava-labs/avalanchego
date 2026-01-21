# avalanche-rs

A Rust implementation of the Avalanche consensus protocol, designed to be wire-compatible with the Go implementation (AvalancheGo).

## Status

**Phase 0: Foundation** - Core primitives and utilities are implemented.

## Crates

| Crate | Description | Status |
|-------|-------------|--------|
| `avalanche-ids` | Identifier types (ID, ShortID, NodeID) with CB58 encoding | Complete |
| `avalanche-codec` | Binary serialization compatible with Go wire format | Complete |
| `avalanche-utils` | Common utilities (Set, Bag, logging, errors, timers) | Complete |

## Building

```bash
cd avalanche-rs
cargo build
```

## Testing

```bash
cargo test
```

## Wire Format Compatibility

The codec uses big-endian byte order for all multi-byte integers, matching the Go implementation:

- `u8`: 1 byte
- `u16`: 2 bytes, big-endian
- `u32`: 4 bytes, big-endian
- `u64`: 8 bytes, big-endian
- `bool`: 1 byte (0x00 = false, 0x01 = true)
- `String`: 2-byte length prefix (u16) + UTF-8 bytes
- `Vec<T>`: 4-byte length prefix (u32) + elements
- Fixed arrays: elements only (no length prefix)

## ID Encoding

All ID types use CB58 encoding (Base58 with a 4-byte SHA-256 checksum):

```rust
use avalanche_ids::{Id, NodeId};

// Parse an ID from CB58
let id: Id = "11111111111111111111111111111111LpoYY".parse().unwrap();

// NodeIDs include a prefix
let node_id: NodeId = "NodeID-111111111111111111116DBWJs".parse().unwrap();
```

## License

BSD-3-Clause

## Contributing

See the main [RUST_REWRITE_PLAN.md](../RUST_REWRITE_PLAN.md) for the overall project roadmap.

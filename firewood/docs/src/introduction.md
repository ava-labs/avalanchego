# Introduction

Firewood is an embedded key-value store optimized for storing recent Merkleized
blockchain state with minimal overhead. It is built for the Avalanche C-Chain and
EVM-compatible blockchains that keep state in Merkle tries.

Unlike most state-management approaches, Firewood uses the trie structure directly
as its on-disk index rather than emulating a trie on top of a generic key-value
store such as LevelDB or RocksDB. It is compaction-less: trie nodes are written
directly to a single database file, addressed by their offset within that file.

This site is the front door to Firewood's documentation:

- **Getting Started** walks through setting up a development environment.
- **Concepts & Architecture** explains the trie storage model, revisions, and hashing.
- **Design Documents** records the designs behind Firewood's on-disk format and
  subsystems, and describes how new designs are proposed and reviewed.
- **Integration** covers the Go FFI and how AvalancheGo consumes Firewood.
- **Operations & Benchmarking** covers the `fwdctl` CLI and performance dashboards.
- **Reference** links to the generated Rust and Go API documentation.

> [!WARNING]
> Firewood is beta-level software. Its API is still evolving and stability is not
> yet guaranteed.

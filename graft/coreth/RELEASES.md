# Release Notes

## [v0.8.11](https://github.com/ava-labs/coreth/releases/tag/v0.8.11)

## [v0.8.10](https://github.com/ava-labs/coreth/releases/tag/v0.8.10)

- Add beta support for fast sync
- Bump trie tip buffer size to 32
- Fix bug in metrics initialization

## [v0.8.9](https://github.com/ava-labs/coreth/releases/tag/v0.8.9)

- Fix deadlock bug on shutdown causing historical re-generation on restart
- Add API endpoint to fetch running VM Config
- Add AvalancheGo custom log formatting to C-Chain logs
- Deprecate support for JS Tracer

## [v0.8.8](https://github.com/ava-labs/coreth/releases/tag/v0.8.8)

- Reduced log level of snapshot regeneration logs
- Enabled atomic tx replacement with higher gas fees
- Parallelize trie index re-generation

## [v0.8.7](https://github.com/ava-labs/coreth/releases/tag/v0.8.7)

- Optimize FeeHistory API
- Add protection to prevent accidental corruption of archival node trie index
- Add capability to restore complete trie index on best effort basis
- Round up fastcache sizes to utilize all mmap'd memory in chunks of 64MB

## [v0.8.6](https://github.com/ava-labs/coreth/releases/tag/v0.8.6)

- Migrate go-ethereum v1.10.16 changes
- Increase FeeHistory maximum historical limit to improve MetaMask UI on C-Chain
- Enable chain state metrics

## [v0.8.5](https://github.com/ava-labs/coreth/releases/tag/v0.8.5)

- Add support for offline pruning
- Refactor VM networking layer
- Enable metrics by default
- Mark RPC call specific metrics as expensive
- Add Abigen support for native asset call precompile
- Fix bug in BLOCKHASH opcode during traceBlock
- Fix bug in handling updated chain config on startup

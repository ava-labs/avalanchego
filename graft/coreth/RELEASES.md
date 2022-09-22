# Release Notes

## [v0.10.0](https://github.com/ava-labs/coreth/releases/tag/v0.10.0)

- Deprecate Native Asset Call and Native Asset Balance
- Deprecate Import/Export of non-AVAX Avalanche Native Tokens via Atomic Transactions
- Add failure reason to bad block API

## [v0.9.0](https://github.com/ava-labs/coreth/releases/tag/v0.9.0)

- Migrate to go-ethereum v1.10.23
- Add API to fetch Chain Config

## [v0.8.16](https://github.com/ava-labs/coreth/releases/tag/v0.8.16)

- Fix bug in `codeToFetch` database accessors that caused an error when starting/stopping state sync
- Bump go-ethereum version to v1.10.21
- Update gas price estimation to limit lookback window based on block timestamps
- Add metrics for processed/accepted gas
- Simplify syntactic block verification
- Ensure statedb errors during block processing are logged
- Remove deprecated gossiper/block building logic from pre-Apricot Phase 4
- Add marshal function for duration to improve config output

## [v0.8.15](https://github.com/ava-labs/coreth/releases/tag/v0.8.15)

- Add optional JSON logging
- Bump minimum go version to v1.18.1
- Add interface for suppporting stateful precompiles
- Remove legacy code format from the database
- Enable expensive metrics by default
- Fix atomic trie sync bug that could result in storing incorrect metadata
- Update state sync metrics to use counter for number of items received

## [v0.8.14](https://github.com/ava-labs/coreth/releases/tag/v0.8.14)

- Bump go-ethereum dependency to v1.10.20
- Update API names used to enable services in `eth-api` config flag. Prior names are supported but deprecated, please update your configuration [accordingly](https://docs.avax.network/nodes/maintain/chain-config-flags#c-chain-configs)
- Optimizes state sync by parallelizing trie syncing
- Adds `eth_syncing` API for compatibility. Note: This API is only accessible after bootstrapping and always returns `"false"`, since the node will no longer be syncing at that point.
- Adds metrics to atomic transaction mempool
- Adds metrics for incoming/outgoing mempool gossip

## [v0.8.13](https://github.com/ava-labs/coreth/releases/tag/v0.8.13)

- Bump go-ethereum dependency to v1.10.18
- Parallelize state sync code fetching
- Deprecated CB58 format for API calls

## [v0.8.12](https://github.com/ava-labs/coreth/releases/tag/v0.8.12)

- Add peer bandwidth tracking to optimize state sync message routing
- Fix leaf request handler bug to ensure the handler delivers a valid range proof
- Remove redundant proof keys from leafs response message format
- Improve state sync request retry logic
- Improve state sync handler metrics
- Improve state sync ETA

## [v0.8.11](https://github.com/ava-labs/coreth/releases/tag/v0.8.11)

- Improve state sync leaf request serving by optimistically reading leaves from snapshot
- Add acceptor queue within `core/blockchain.go`
- Cap size of TrieDB dirties cache during block acceptance to reduce commit size at 4096 block interval
- Refactor state sync block fetching
- Improve state sync metrics

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

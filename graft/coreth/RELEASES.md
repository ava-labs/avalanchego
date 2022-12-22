# Release Notes

## [v0.11.5](https://github.com/ava-labs/coreth/releases/tag/v0.11.5)

- Add support for eth_call over VM2VM messaging
- Add config flags for tx pool behavior

## [v0.11.4](https://github.com/ava-labs/coreth/releases/tag/v0.11.4)

- Add config option to perform database inspection on startup
- Add configurable transaction indexing to reduce disk usage
- Add special case to allow transactions using Nick's Method to bypass API level replay protection
- Add counter metrics for number of accepted/processed logs
- Improve header and logs caching using maximum accepted depth cache

## [v0.11.3](https://github.com/ava-labs/coreth/releases/tag/v0.11.3)

- Add counter for number of processed and accepted transactions
- Wait for state sync goroutines to complete on shutdown
- Bump go-ethereum dependency to v1.10.26
- Increase soft cap on transaction size limits
- Add back isForkIncompatible checks for all existing forks
- Clean up Apricot Phase 6 code

## [v0.11.2](https://github.com/ava-labs/coreth/releases/tag/v0.11.2)

- Add trie clean cache journaling to disk to improve processing time on restart
- Fix regression where snapshot could be marked as stale by async acceptor during block processing
- Add fine-grained block processing metrics

## [v0.11.1](https://github.com/ava-labs/coreth/releases/tag/v0.11.1)

- Add cache size config parameters for `trie-clean-cache`, `trie-dirty-cache`, `trie-dirty-commit-target`, and `snapshot-cache`
- Increase default `trie-clean-cache` size from 256 MB to 512 MB
- Increase default `snapshot-cache` size from 128 MB to 256 MB
- Add optional flag to skip chain config upgrade check on startup (allows VM to start after missing a network upgrade)
- Make Avalanche blockchainID (separate from EVM ChainID) available within the EVM
- Record block height when performing state sync
- Add support for VM-to-VM messaging
- Move `eth_getChainConfig` under the `BlockChainAPI`
- Simplify block builder timer logic to a simple retry delay
- Add Opentelemetry support
- Simplify caching logic for gas price estimation

## [v0.11.0](https://github.com/ava-labs/coreth/releases/tag/v0.11.0)

- Update Chain Config compatibility check to compare against last accepted block timestamp
- Bump go-ethereum dependency to v1.10.25
- Add Banff activation times for Mainnet and Fuji for October 18 4pm UTC and October 3 2pm UTC respectively
- Banff cleanup

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

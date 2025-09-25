# Release Notes

## Pending Release

- Removed deprecated flags `coreth-admin-api-enabled`, `coreth-admin-api-dir`, `tx-regossip-frequency`, `tx-lookup-limit`. Use `admin-api-enabled`, `admin-api-dir`, `regossip-frequency`, `transaction-history` instead.
- Enabled RPC batch limits by default, and configurable with `batch-request-limit` and `batch-max-response-size`.
- Implement ACP-226: Set expected block gas cost to 0 in Granite network upgrade, removing block gas cost requirements for block building.
- Implement ACP-226: Add `timeMilliseconds` (Unix uint64) timestamp to block header for Granite upgrade.
- Implement ACP-226: Add `minDelayExcess` (uint64) to block header for Granite upgrade.
- Update go version to 1.24.7

## [v0.15.3](https://github.com/ava-labs/coreth/releases/tag/v0.15.3)

- Removed legacy warp message handlers in favor of ACP-118 SDK handlers.
- Use `state-history` eth config flag to designate the number of recent states queryable.
- Added maximum number of addresses (1000) to be queried in a single filter.
- Moves atomic operations from plugin/evm to plugin/evm/atomic and wraps the plugin/evm/VM in `atomicvm` to separate the atomic operations from the EVM execution.
- Demoted unnecessary error log in `core/txpool/legacypool.go` to warning, displaying unexpected but valid behavior.
- Removed the `snowman-api-enabled` flag and the corresponding API implementation.
- Enable experimental `state-scheme` flag to specify Firewood as a state database.
- Added prometheus metrics for Firewood if it is enabled and expensive metrics are being used.
- Disable incompatible APIs for Firewood.

## [v0.15.2](https://github.com/ava-labs/coreth/releases/tag/v0.15.2)

## [v0.15.1](https://github.com/ava-labs/coreth/releases/tag/v0.15.1)

- Major refactor to use [`libevm`](https://github.com/ava-labs/libevm) for EVM execution, database access, types & chain configuration. This improves maintainability and enables keeping up with upstream changes more easily.
- Add metrics for ACP-176
- Removed the `"price-options-max-base-fee"` config flag
- Removed extra type support in "ethclient.BlockByHash", "ethclient.BlockByNumber".
- Moved extra types returned in `ethclient` package to a new package `plugin/evm/customethclient` which supports the same functionality as `ethclient` but with the new types registered in header and block.

## [v0.15.0](https://github.com/ava-labs/coreth/releases/tag/v0.15.0)

- Bump golang version to v1.23.6
- Bump golangci-lint to v1.63 and add linters
- Implement ACP-176
- Add `GasTarget` to the chain config to allow modifying the chain's `GasTarget` based on the ACP-176 rules

- Added `eth_suggestPriceOptions` API to suggest gas prices (slow, normal, fast) based on the current network conditions
- Added `"price-options-slow-fee-percentage"`, `"price-options-fast-fee-percentage"`, `"price-options-max-base-fee"`, and `"price-options-max-tip"` config flags to configure the new `eth_suggestPriceOptions` API

## [v0.14.1](https://github.com/ava-labs/coreth/releases/tag/v0.14.1)

- Removed deprecated `ExportKey`, `ExportAVAX`, `Export`, `ImportKey`, `ImportAVAX`, `Import` APIs
- IMPORTANT: `eth_getProof` calls for historical state will be rejected by default.
  - On archive nodes (`"pruning-enabled": false`): queries for historical proofs for state older than approximately 24 hours preceding the last accepted block will be rejected by default. This can be adjusted with the new option `historical-proof-query-window` which defines the number of blocks before the last accepted block which should be accepted for state proof queries, or set to `0` to accept any block number state query (previous behavior).
  - On `pruning` nodes: queries for proofs past the tip buffer (32 blocks) will be rejected. This is in support of moving to a path based storage scheme, which does not support historical state proofs.
- Remove API eth_getAssetBalance that was used to query ANT balances (deprecated since v0.10.0)
- Remove legacy gossip handler and metrics (deprecated since v0.10.0)
- Refactored trie_prefetcher.go to be structurally similar to [upstream](https://github.com/ethereum/go-ethereum/tree/v1.13.14).

## [v0.14.0](https://github.com/ava-labs/coreth/releases/tag/v0.14.0)

- Minor version update to correspond to avalanchego v1.12.0 / Etna.
- Remove unused historical opcodes CALLEX, BALANCEMC
- Remove unused pre-AP2 handling of genesis contract
- Fix to track tx size in block building
- Test fixes
- Update go version to 1.22

## [v0.13.8](https://github.com/ava-labs/coreth/releases/tag/v0.13.8)

- Update geth dependency to v1.13.14
- eupgrade: lowering the base fee to 1 nAVAX
- eupgrade/cancun: verify no blobs in header
- Supports ACP-118 message types
- Gets network upgrade timestamps from avalanchego
- Remove cross-chain handlers

## [v0.13.7](https://github.com/ava-labs/coreth/releases/tag/v0.13.7)

- Add EUpgrade base definitions
- Remove Block Status
- Fix and improve "GetBlockIDAtHeight"
- Bump golang version requirement to 1.21.12
- Bump AvalancheGo to v1.11.10-prerelease

## [v0.13.6](https://github.com/ava-labs/coreth/releases/tag/v0.13.6)

- rpc: truncate call error data logs
- logging: remove path prefix (up to coreth@version/) from logged file names.
- cleanup: removes pre-Durango scripts

## [v0.13.5](https://github.com/ava-labs/coreth/releases/tag/v0.13.5)

- Bump AvalancheGo to v1.11.7
- Bump golang version requirement to 1.21.12
- Switches timestamp log back to "timestamp" (as was before v0.13.4)
- Add missing fields to "toCallArg"
- Fix state sync ETA overflow
- Fix state sync crash bug

## [v0.13.4](https://github.com/ava-labs/coreth/releases/tag/v0.13.4)

- Fixes snapshot use when state sync was explicitly enabled
- Fixes v0.13.3 locking regression in async snapshot generation
- Update go-ethereum to v1.13.8
- Bump AvalancheGo to v1.11.6
- Bump golang version requirement to 1.21.10
- "timestamp" in logs is changed to "t"

## [v0.13.3](https://github.com/ava-labs/coreth/releases/tag/v0.13.3)

- Update go-ethereum to v1.13.2
- Bump AvalancheGo to v1.11.5
- Bump golang version requirement to 1.21.9
- Respect local flag in legacy tx pool
- Disable blobpool
- Testing improvements

## [v0.13.2](https://github.com/ava-labs/coreth/releases/tag/v0.13.2)

- Integrate stake weighted gossip selection
- Update go-ethereum to v1.12.2
- Force precompile modules registration in ethclient
- Bump Avalanchego to v1.11.3

## [v0.13.1](https://github.com/ava-labs/coreth/releases/tag/v0.13.1)

- Bump AvalancheGo to v1.11.2
- Remove Legacy Gossipper
- Tune default gossip parameters

## [v0.13.0](https://github.com/ava-labs/coreth/releases/tag/v0.13.0)

- Bump AvalancheGo to v1.11.1
- Bump minimum Go version to 1.21.7
- Add more error messages to warp backend

## [v0.12.10](https://github.com/ava-labs/coreth/releases/tag/v0.12.10)

- Add support for off-chain warp messages
- Add support for getBlockReceipts RPC API
- Fix issue with state sync for large blocks
- Migrating Push Gossip to avalanchego network SDK handlers

## [v0.12.9](https://github.com/ava-labs/coreth/releases/tag/v0.12.9)

- Add concurrent prefetching of trie nodes during block processing
- Add `skip-tx-indexing` flag to disable transaction indexing and unindexing
- Update acceptor tip before sending chain events to subscribers
- Add soft cap on total block data size for state sync block requests

## [v0.12.8](https://github.com/ava-labs/coreth/releases/tag/v0.12.8)

- Bump AvalancheGo to v1.10.15
- Fix crash in prestate tracer on memory read

## [v0.12.7](https://github.com/ava-labs/coreth/releases/tag/v0.12.7)

- Bump AvalancheGo to v1.10.14

## [v0.12.6](https://github.com/ava-labs/coreth/releases/tag/v0.12.6)

- Remove lock options from HTTP handlers
- Fix deadlock in `eth_getLogs` when matcher session hits a missing block
- Replace Kurtosis E2E tests with avctl test framework

## [v0.12.5](https://github.com/ava-labs/coreth/releases/tag/v0.12.5)

- Add P2P SDK Pull Gossip to mempool
- Fix hanging requests on shutdown that could cause ungraceful shutdown
- Increase batch size writing snapshot diff to disk
- Migrate geth changes from v1.11.4 through v1.12.0
- Bump AvalancheGo dependency to v1.10.10

## [v0.12.4](https://github.com/ava-labs/coreth/releases/tag/v0.12.4)

- Fix API handler crash for `lookupState` in `prestate` tracer
- Fix API handler crash for LOG edge cases in the `callTracer`
- Fix regression in `eth_getLogs` serving request for blocks containing no Ethereum transactions
- Export `CalculateDynamicFee`

## [v0.12.3](https://github.com/ava-labs/coreth/releases/tag/v0.12.3)

- Migrate go-ethereum changes through v1.11.4
- Downgrade API error log from `Warn` to `Info`

## [v0.12.2](https://github.com/ava-labs/coreth/releases/tag/v0.12.2)

- Increase default trie dirty cache size from 256MB to 512MB

## [v0.12.1](https://github.com/ava-labs/coreth/releases/tag/v0.12.1)

- Bump AvalancheGo dependency to v1.10.1
- Improve block building logic
- Use shorter ctx while reading snapshot to serve state sync requests
- Remove proposer activation time from gossiper
- Fail outstanding requests on shutdown
- Make state sync request sizes configurable

## [v0.12.0](https://github.com/ava-labs/coreth/releases/tag/v0.12.0)

- Increase C-Chain block gas limit to 15M in Cortina
- Add Mainnet and Fuji Cortina Activation timestamps

## [v0.11.9](https://github.com/ava-labs/coreth/releases/tag/v0.11.9)

- Downgrade SetPreference log from warn to debug

## [v0.11.8](https://github.com/ava-labs/coreth/releases/tag/v0.11.8)

- Fix shutdown hanging during state sync
- Add pre-check for imported UTXOs
- Fix bug in `BadBlockReason` output to display error string correctly
- Update golangci-lint version to v1.51.2

## [v0.11.7](https://github.com/ava-labs/coreth/releases/tag/v0.11.7)

- Enable state sync by default when syncing from an empty database
- Increase block gas limit to 15M for Cortina Network Upgrade
- Add back file tracer endpoint
- Add back JS tracer

## [v0.11.6](https://github.com/ava-labs/coreth/releases/tag/v0.11.6)

- Bump AvalancheGo to v1.9.6

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
- Add interface for supporting stateful precompiles
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

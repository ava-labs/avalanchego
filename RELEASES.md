# Release Notes

## [v1.9.16](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.16)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

- Removed unnecessary repoll after rejecting vertices
- Improved snowstorm lookup error handling
- Removed rejected vertices from the Avalanche frontier more aggressively
- Reduced default health check values for processing decisions

## [v1.9.15](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.15)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

- Fixed `x/merkledb.ChangeProof#getLargestKey` to correctly handle no changes
- Added test for `avm/txs/executor.SemanticVerifier#verifyFxUsage` with multiple valid fxs
- Fixed CPU + bandwidth performance regression during vertex processing
- Added example usage of the `/ext/index/X/block` API
- Reduced the default value of `--snow-optimal-processing` from `50` to `10`
- Updated the year in the license header

## [v1.9.14](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.14)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

## [v1.9.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.13)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

## [v1.9.12](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.12)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### Networking

- Removed linger setting on P2P connections
- Improved error message when failing to calculate peer uptimes
- Removed `EngineType` from P2P response messages
- Added context cancellation during dynamic IP updates
- Reduced the maximum P2P reconnect delay from 1 hour to 1 minute

### Consensus

- Added support to switch from `Avalanche` consensus to `Snowman` consensus
- Added support for routing consensus messages to either `Avalanche` or `Snowman` consensus on the same chain
- Removed usage of deferred evaluation of the `handler.Consensus` in the `Avalanche` `OnFinished` callback
- Dropped inbound `Avalanche` consensus messages after switching to `Snowman` consensus
- Renamed the `Avalanche` VM metrics prefix from `avalanche_{chainID}_vm_` to `avalanche_{chainID}_vm_avalanche`
- Replaced `consensus` and `decision` dispatchers with `block`, `tx`, and `vertex` dispatchers
- Removed `Avalanche` bootstrapping restarts during the switch to `Snowman` consensus

### AVM

- Added `avm` block execution manager
- Added `avm` block builder
- Refactored `avm` transaction syntactic verification
- Refactored `avm` transaction semantic verification
- Refactored `avm` transaction execution
- Added `avm` mempool gossip
- Removed block timer interface from `avm` `mempool`
- Moved `toEngine` channel into the `avm` `mempool`
- Added `GetUTXOFromID` to the `avm` `state.Chain` interface
- Added unpopulated `MerkleRoot` to `avm` blocks
- Added `avm` transaction based metrics
- Replaced error strings with error interfaces in the `avm` mempool

### PlatformVM

- Added logs when the local nodes stake amount changes
- Moved `platformvm` `message` package into `components`
- Replaced error strings with error interfaces in the `platformvm` mempool

### Warp

- Added `ID` method to `warp.UnsignedMessage`
- Improved `warp.Signature` verification error descriptions

### Miscellaneous

- Improved `merkledb` locking to allow concurrent read access through `trieView`s
- Fixed `Banff` transaction signing with ledger when using the wallet
- Emitted github artifacts after successful builds
- Added non-blocking bounded queue
- Converted the `x.Parser` helper to be a `block.Parser` interface from a `tx.Parser` interface

### Cleanup

- Separated dockerhub image publishing from the kurtosis test workflow
- Exported various errors to use in testing
- Removed the `vms/components/state` package
- Replaced ad-hoc linked hashmaps with the standard data-structure
- Removed `usr/local/lib/avalanche` from deb packages
- Standardized usage of `constants.UnitTestID`

### Examples

- Added P-chain `RemoveSubnetValidatorTx` example using the wallet
- Added X-chain `CreateAssetTx` example using the wallet

### Configs

- Added support to specify `HTTP` server timeouts
  - `--http-read-timeout`
  - `--http-read-header-timeout`
  - `--http-write-timeout`
  - `--http-idle-timeout`

### APIs

- Added `avm` block APIs
  - `avm.getBlock`
  - `avm.getBlockByHeight`
  - `avm.getHeight`
- Converted `avm` APIs to only surface accepted state
- Deprecated all `ipcs` APIs
  - `ipcs.publishBlockchain`
  - `ipcs.unpublishBlockchain`
  - `ipcs.getPublishedBlockchains`
- Deprecated all `keystore` APIs
  - `keystore.createUser`
  - `keystore.deleteUser`
  - `keystore.listUsers`
  - `keystore.importUser`
  - `keystore.exportUser`
- Deprecated the `avm/pubsub` API endpoint
- Deprecated various `avm` APIs
  - `avm.getAddressTxs`
  - `avm.getBalance`
  - `avm.getAllBalances`
  - `avm.createAsset`
  - `avm.createFixedCapAsset`
  - `avm.createVariableCapAsset`
  - `avm.createNFTAsset`
  - `avm.createAddress`
  - `avm.listAddresses`
  - `avm.exportKey`
  - `avm.importKey`
  - `avm.mint`
  - `avm.sendNFT`
  - `avm.mintNFT`
  - `avm.import`
  - `avm.export`
  - `avm.send`
  - `avm.sendMultiple`
- Deprecated the `avm/wallet` API endpoint
  - `wallet.issueTx`
  - `wallet.send`
  - `wallet.sendMultiple`
- Deprecated various `platformvm` APIs
  - `platform.exportKey`
  - `platform.importKey`
  - `platform.getBalance`
  - `platform.createAddress`
  - `platform.listAddresses`
  - `platform.getSubnets`
  - `platform.addValidator`
  - `platform.addDelegator`
  - `platform.addSubnetValidator`
  - `platform.createSubnet`
  - `platform.exportAVAX`
  - `platform.importAVAX`
  - `platform.createBlockchain`
  - `platform.getBlockchains`
  - `platform.getStake`
  - `platform.getMaxStakeAmount`
  - `platform.getRewardUTXOs`
- Deprecated the `stake` field in the `platform.getTotalStake` response in favor of `weight`

## [v1.9.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.11)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### Plugins

- Removed error from `logging.NoLog#Write`
- Added logging to the static VM factory usage
- Fixed incorrect error being returned from `subprocess.Bootstrap`

### Ledger

- Added ledger tx parsing support

### MerkleDB

- Added explicit consistency guarantees when committing multiple `merkledb.trieView`s to disk at once
- Removed reliance on premature root calculations for `merkledb.trieView` validity tracking
- Updated `x/merkledb/README.md`

## [v1.9.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.10)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `24`.

### MerkleDB

- Removed parent tracking from `merkledb.trieView`
- Removed `base` caches from `merkledb.trieView`
- Fixed error handling during `merkledb` intermediate node eviction
- Replaced values larger than `32` bytes with a hash in the `merkledb` hash representation

### AVM

- Refactored `avm` API tx creation into a standalone `Spender` implementation
- Migrated UTXO interfaces from the `platformvm` into the `components` for use in the `avm`
- Refactored `avm` `tx.SyntacticVerify` to expect the config rather than the fee fields

### Miscellaneous

- Updated the minimum golang version to `v1.19.6`
- Fixed `rpcchainvm` signal handling to only shutdown upon receipt of `SIGTERM`
- Added `warp.Signature#NumSigners` for better cost tracking support
- Added `snow.Context#PublicKey` to provide access to the local node's BLS public key inside the VM execution environment
- Renamed Avalanche consensus metric prefix to `avalanche_{chainID}_avalanche`
- Specified an explicit TCP `Linger` timeout of `15` seconds
- Updated the `secp256k1` library to `v4.1.0`

### Cleanup

- Removed support for the `--whitelisted-subnets` flag
- Removed unnecessary abstractions from the `app` package
- Removed `Factory` embedding from `platformvm.VM` and `avm.VM`
- Removed `validator` package from the `platformvm`
- Removed `timer.TimeoutManager`
- Replaced `snow.Context` in `Factory.New` with `logging.Logger`
- Renamed `set.Bits#Len` to `BitLen` and `set.Bits#HammingWeight` to `Len` to align with `set.Bits64`

## [v1.9.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.9)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `23`.

**Note: The `--whitelisted-subnets` flag was deprecated in `v1.9.6`. This is the last release in which it will be supported. Use `--track-subnets` instead.**

### Monitoring

- Added warning when the P2P server IP is private
- Added warning when the HTTP server IP is potentially publicly reachable
- Removed `merkledb.trieView#calculateIDs` tracing when no recalculation is needed

### Databases

- Capped the number of goroutines that `merkledb.trieView#calculateIDsConcurrent` will create
- Removed `nodb` package
- Refactored `Batch` implementations to share common code
- Added `Batch.Replay` invariant tests
- Converted to use `require` in all `database` interface tests

### Cryptography

- Moved the `secp256k1` implementations to a new `secp256k1` package out of the `crypto` package
- Added `rfc6979` compliance tests to the `secp256k1` signing implementation
- Removed unused cryptography implementations `ed25519`, `rsa`, and `rsapss`
- Removed unnecessary cryptography interfaces `crypto.Factory`, `crypto.RecoverableFactory`, `crypto.PublicKey`, and `crypto.PrivateKey`
- Added verification when parsing `secp256k1` public keys to ensure usage of the compressed format

### API

- Removed delegators from `platform.getCurrentValidators` unless a single `nodeID` is requested
- Added `delegatorCount` and `delegatorWeight` to the validators returned by `platform.getCurrentValidators`

### Documentation

- Improved documentation on the `block.WithVerifyContext` interface
- Fixed `--public-ip` and `--public-ip-resolution-service` CLI flag descriptions
- Updated `README.md` to explicitly reference `SECURITY.md`

### Coreth

- Enabled state sync by default when syncing from an empty database
- Increased block gas limit to 15M for `Cortina` Network Upgrade
- Added back file tracer endpoint
- Added back JS tracer

### Miscellaneous

- Added `allowedNodes` to the subnet config for `validatorOnly` subnets
- Removed the `hashicorp/go-plugin` dependency to improve plugin flexibility
- Replaced specialized `bag` implementations with generic `bag` implementations
- Added `mempool` package to the `avm`
- Added `chain.State#IsProcessing` to simplify integration with `block.WithVerifyContext`
- Added `StateSyncMinVersion` to `sync.ClientConfig`
- Added validity checks for `InitialStakeDuration` in a custom network genesis
- Removed unnecessary reflect call when marshalling an empty slice

### Cleanup

- Renamed `teleporter` package to `warp`
- Replaced `bool` flags in P-chain state diffs with an `enum`
- Refactored subnet configs to more closely align between the primary network and subnets
- Simplified the `utxo.Spender` interface
- Removed unused field `common.Config#Validators`

## [v1.9.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.8)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Networking

- Added TCP proxy support for p2p network traffic
- Added p2p network client utility for directly messaging the p2p network

### Consensus

- Guaranteed delivery of App messages to the VM, regardless of sync status
- Added `EngineType` to consensus context

### MerkleDB - Alpha

- Added initial implementation of a path-based merkle-radix tree
- Added initial implementation of state sync powered by the merkledb

### APIs

- Updated `platform.getCurrentValidators` to return `uptime` as a percentage
- Updated `platform.get*Validators` to avoid iterating over the staker set when requesting specific nodeIDs
- Cached staker data in `platform.get*Validators` to significantly reduce DB IO
- Added `stakeAmount` and `weight` to all staker responses in P-chain APIs
- Deprecated `stakeAmount` in staker responses from P-chain APIs
- Removed `creationTxFee` from `info.GetTxFeeResponse`
- Removed `address` from `platformvm.GetBalanceRequest`

### Fixes

- Fixed `RemoveSubnetValidatorTx` weight diff corruption
- Released network lock before attempting to close a peer connection
- Fixed X-Chain last accepted block initialization to use the genesis block, not the stop vertex after linearization
- Removed plugin directory handling from AMI generation
- Removed copy of plugins directory from tar script

### Cleanup

- Removed unused rpm packaging scripts
- Removed engine dependency from chain registrants
- Removed unused field from chain handler log
- Linted custom test `chains.Manager`
- Used generic btree implementation
- Deleted `utils.CopyBytes`
- Updated rjeczalik/notify from v0.9.2 to v0.9.3

### Miscellaneous

- Added AVM `state.Chain` interface
- Added generic atomic value utility
- Added test for the AMI builder during RCs
- Converted cache implementations to use generics
- Added optional cache eviction callback

## [v1.9.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.7)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Fixes

- Fixed subnet validator lookup regression

## [v1.9.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.6)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `22`.

### Consensus

- Added `StateSyncMode` to the return of `StateSummary#Accept` to support syncing chain state while tracking the chain as a light client
- Added `AcceptedFrontier` to `Chits` messages
- Reduced unnecessary confidence resets during consensus by applying `AcceptedFrontier`s during `QueryFailed` handling
- Added EngineType for consensus messages in the p2p message definitions
- Updated `vertex.DAGVM` interface to support linearization

### Configs

- Added `--plugin-dir` flag. The default value is `[DATADIR]/plugins`
- Removed `--build-dir` flag. The location of the avalanchego binary is no longer considered when looking for the `plugins` directory. Subnet maintainers should ensure that their node is able to properly discover plugins, as the default location is likely changed. See `--plugin-dir`
- Changed the default value of `--api-keystore-enabled` to `false`
- Added `--track-subnets` flag as a replacement of `--whitelisted-subnets`

### Fixes

- Fixed NAT-PMP router discovery and port mapping
- Fixed `--staking-enabled=false` setting to correctly start subnet chains and report healthy
- Fixed message logging in the consensus handler

### VMs

- Populated non-trivial logger in the `rpcchainvm` `Server`'s `snow.Context`
- Updated `rpcchainvm` proto definitions to use enums
- Added `Block` format and definition to the `AVM`
- Removed `proposervm` height index reset

### Metrics

- Added `avalanche_network_peer_connected_duration_average` metric
- Added `avalanche_api_calls_processing` metric
- Added `avalanche_api_calls` metric
- Added `avalanche_api_calls_duration` metric

### Documentation

- Added wallet example to create `stakeable.LockOut` outputs
- Improved ubuntu deb install instructions

### Miscellaneous

- Updated ledger-avalanche to v0.6.5
- Added linter to ban the usage of `fmt.Errorf` without format directives
- Added `List` to the `buffer#Deque` interface
- Added `Index` to the `buffer#Deque` interface
- Added `SetLevel` to the `Logger` interface
- Updated `auth` API to use the new `jwt` standard

## [v1.9.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.5)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `21`.

### Subnet Messaging

- Added subnet message serialization format
- Added subnet message signing
- Replaced `bls.SecretKey` with a `teleporter.Signer` in the `snow.Context`
- Moved `SNLookup` into the `validators.State` interface to support non-whitelisted chainID to subnetID lookups
- Added support for non-whitelisted subnetIDs for fetching the validator set at a given height
- Added subnet message verification
- Added `teleporter.AnycastID` to denote a subnet message not intended for a specific chain

### Fixes

- Added re-gossip of updated validator IPs
- Fixed `rpcchainvm.BatchedParseBlock` to correctly wrap returned blocks
- Removed incorrect `uintptr` handling in the generic codec
- Removed message latency tracking on messages being sent to itself

### Coreth

- Added support for eth_call over VM2VM messaging
- Added config flags for tx pool behavior

### Miscellaneous

- Added networking package README.md
- Removed pagination of large db messages over gRPC
- Added `Size` to the generic codec to reduce allocations
- Added `UnpackLimitedBytes` and `UnpackLimitedStr` to the manual packer
- Added SECURITY.md
- Exposed proposer list from the `proposervm`'s `Windower` interface
- Added health and bootstrapping client helpers that block until the node is healthy
- Moved bit sets from the `ids` package to the `set` package
- Added more wallet examples

## [v1.9.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.4)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `20`.

**This version modifies the db format. The db format is compatible with v1.9.3, but not v1.9.2 or earlier. After running a node with v1.9.4 attempting to run a node with a version earlier than v1.9.3 may report a fatal error on startup.**

### PeerList Gossip Optimization

- Added gossip tracking to the `peer` instance to only gossip new `IP`s to a connection
- Added `PeerListAck` message to report which `TxID`s provided by the `PeerList` message were tracked
- Added `TxID`s to the `PeerList` message to unique-ify nodeIDs across validation periods
- Added `TxID` mappings to the gossip tracker

### Validator Set Tracking

- Renamed `GetValidators` to `Get` on the `validators.Manager` interface
- Removed `Set`, `AddWeight`, `RemoveWeight`, and `Contains` from the `validators.Manager` interface
- Added `Add` to the `validators.Manager` interface
- Removed `Set` from the `validators.Set` interface
- Added `Add` and `Get` to the `validators.Set` interface
- Modified `validators.Set#Sample` to return `ids.NodeID` rather than `valdiators.Validator`
- Replaced the `validators.Validator` interface with a struct
- Added a `BLS` public key field to `validators.Validator`
- Added a `TxID` field to `validators.Validator`
- Improved and documented error handling within the `validators.Set` interface
- Added `BLS` public keys to the result of `GetValidatorSet`
- Added `BuildBlockWithContext` as an optional VM method to build blocks at a specific P-chain height
- Added `VerifyWithContext` as an optional block method to verify blocks at a specific P-chain height

### Uptime Tracking

- Added ConnectedSubnet message handling to the chain handler
- Added SubnetConnector interface and implemented it in the platformvm
- Added subnet uptimes to p2p `pong` messages
- Added subnet uptimes to `platform.getCurrentValidators`
- Added `subnetID` as an argument to `info.Uptime`

### Fixes

- Fixed incorrect context cancellation of escaped contexts from grpc servers
- Fixed race condition between API initialization and shutdown
- Fixed race condition between NAT traversal initialization and shutdown
- Fixed race condition during beacon connection tracking
- Added race detection to the E2E tests
- Added additional message and sender tests

### Coreth

- Improved header and logs caching using maximum accepted depth cache
- Added config option to perform database inspection on startup
- Added configurable transaction indexing to reduce disk usage
- Added special case to allow transactions using Nick's Method to bypass API level replay protection
- Added counter metrics for number of accepted/processed logs

### APIs

- Added indices to the return values of `GetLastAccepted` and `GetContainerByID` on the `indexer` API client
- Removed unnecessary locking from the `info` API

### Chain Data

- Added `ChainDataDir` to the `snow.Context` to allow blockchains to canonically access disk outside avalanchego's database
- Added `--chain-data-dir` as a CLI flag to specify the base directory for all `ChainDataDir`s

### Miscellaneous

- Removed `Version` from the `peer.Network` interface
- Removed `Pong` from the `peer.Network` interface
- Reduced memory allocations inside the system throttler
- Added `CChainID` to the `snow.Context`
- Converted all sorting to utilize generics
- Converted all set management to utilize generics

## [v1.9.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.3)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `19`.

### Tracing

- Added `context.Context` to all `VM` interface functions
- Added `context.Context` to the `validators.State` interface
- Added additional message fields to `tracedRouter#HandleInbound`
- Added `tracedVM` implementations for `block.ChainVM` and `vertex.DAGVM`
- Added `tracedState` implementation for `validators.State`
- Added `tracedHandler` implementation for `http.Handler`
- Added `tracedConsensus` implementations for `snowman.Consensus` and `avalanche.Consensus`

### Fixes

- Fixed incorrect `NodeID` used in registered `AppRequest` timeouts
- Fixed panic when calling `encdb#NewBatch` after `encdb#Close`
- Fixed panic when calling `prefixdb#NewBatch` after `prefixdb#Close`

### Configs

- Added `proposerMinBlockDelay` support to subnet configs
- Added `providedFlags` field to the `initializing node` for easily observing custom node configs
- Added `--chain-aliases-file` and `--chain-aliases-file-content` CLI flags
- Added `--proposervm-use-current-height` CLI flag

### Coreth

- Added metric for number of processed and accepted transactions
- Added wait for state sync goroutines to complete on shutdown
- Increased go-ethereum dependency to v1.10.26
- Increased soft cap on transaction size limits
- Added back isForkIncompatible checks for all existing forks
- Cleaned up Apricot Phase 6 code

### Linting

- Added `unused-receiver` linter
- Added `unused-parameter` linter
- Added `useless-break` linter
- Added `unhandled-error` linter
- Added `unexported-naming` linter
- Added `struct-tag` linter
- Added `bool-literal-in-expr` linter
- Added `early-return` linter
- Added `empty-lines` linter
- Added `error-lint` linter

### Testing

- Added `scripts/build_fuzz.sh` and initial fuzz tests
- Added additional `Fx` tests
- Added additional `messageQueue` tests
- Fixed `vmRegisterer` tests

### Documentation

- Documented `Database.Put` invariant for `nil` and empty slices
- Documented avalanchego's versioning scheme
- Improved `vm.proto` docs

### Miscellaneous

- Added peer gossip tracker
- Added `avalanche_P_vm_time_until_unstake` and `avalanche_P_vm_time_until_unstake_subnet` metrics
- Added `keychain.NewLedgerKeychainFromIndices`
- Removed usage of `Temporary` error handling after `listener#Accept`
- Removed `Parameters` from all `Consensus` interfaces
- Updated `avalanche-network-runner` to `v1.3.0`
- Added `ids.BigBitSet` to extend `ids.BitSet64` for arbitrarily large sets
- Added support for parsing future subnet uptime tracking data to the P-chain's state implementation
- Increased validator set cache size
- Added `avax.UTXOIDFromString` helper for managing `UTXOID`s more easily

## [v1.9.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.2)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `19`.

### Coreth

- Added trie clean cache journaling to disk to improve processing time after restart
- Fixed regression where a snapshot could be marked as stale by the async acceptor during block processing
- Added fine-grained block processing metrics

### RPCChainVM

- Added `validators.State` to the rpcchainvm server's `snow.Context`
- Added `rpcProtocolVersion` to the output of `info.getNodeVersion`
- Added `rpcchainvm` protocol version to the output of the `--version` flag
- Added `version.RPCChainVMProtocolCompatibility` map to easily compare plugin compatibility against avalanchego versions

### Builds

- Downgraded `ubuntu` release binaries from `jammy` to `focal`
- Updated macos github runners to `macos-12`
- Added workflow dispatch to build release binaries

### BLS

- Added bls proof of possession to `platform.getCurrentValidators` and `platform.getPendingValidators`
- Added bls public key to in-memory staker objects
- Improved memory clearing of bls secret keys

### Cleanup

- Fixed issue where the chain manager would attempt to start chain creation multiple times
- Fixed race that caused the P-chain to finish bootstrapping before the primary network finished bootstrapping
- Converted inbound message handling to expect usage of types rather than maps of fields
- Simplified the `validators.Set` implementation
- Added a warning if synchronous consensus messages take too long

## [v1.9.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.1)

This version is backwards compatible to [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0). It is optional, but encouraged. The supported plugin version is `18`.

### Features

- Added cross-chain messaging support to the VM interface
- Added Ledger support to the Primary Network wallet
- Converted Bionic builds to Jammy builds
- Added `mock.gen.sh` to programmatically generate mock implementations
- Added BLS signer to the `snow.Context`
- Moved `base` from `rpc.NewEndpointRequester` to be included in the `method` in `SendRequest`
- Converted `UnboundedQueue` to `UnboundedDeque`

### Observability

- Added support for OpenTelemetry tracing
- Converted periodic bootstrapping status update to be time-based
- Removed duplicated fields from the json format of the node config
- Configured min connected stake health check based on the consensus parameters
- Added new consensus metrics
- Documented how chain time is advanced in the PlatformVM with `chain_time_update.md`

### Cleanup

- Converted chain creation to be handled asynchronously from the P-chain's execution environment
- Removed `SetLinger` usage of P2P TCP connections
- Removed `Banff` upgrade flow
- Fixed ProposerVM inner block caching after verification
- Fixed PlatformVM mempool verification to use an updated chain time
- Removed deprecated CLI flags: `--dynamic-update-duration`, `--dynamic-public-ip`
- Added unexpected Put bytes tests to the Avalanche and Snowman consensus engines
- Removed mockery generated mock implementations
- Converted safe math functions to use generics where possible
- Added linting to prevent usage of `assert` in unit tests
- Converted empty struct usage to `nil` for interface compliance checks
- Added CODEOWNERs to own first rounds of PR review

## [v1.9.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.9.0)

This upgrade adds support for creating Proof-of-Stake Subnets.

This version is not backwards compatible. The changes in the upgrade go into effect at 12 PM EDT, October 18th 2022 on Mainnet.

**All Mainnet nodes should upgrade before 12 PM EDT, October 18th 2022.**

The supported plugin version is `17`.

### Upgrades

- Activated P2P serialization format change to Protobuf
- Activated non-AVAX `ImportTx`/`ExportTx`s to/from the P-chain
- Activated `Banff*` blocks on the P-chain
- Deactivated `Apricot*` blocks on the P-chain
- Activated `RemoveSubnetValidatorTx`s on the P-chain
- Activated `TransformSubnetTx`s on the P-chain
- Activated `AddPermissionlessValidatorTx`s on the P-chain
- Activated `AddPermissionlessDelegatorTx`s on the P-chain
- Deactivated ANT `ImportTx`/`ExportTx`s on the C-chain
- Deactivated ANT precompiles on the C-chain

### Deprecations

- Ubuntu 18.04 releases are deprecated and will not be provided for `>=v1.9.1`

### Miscellaneous

- Fixed locked input signing in the P-chain wallet
- Removed assertions from the logger interface
- Removed `--assertions-enabled` flag
- Fixed typo in `--bootstrap-max-time-get-ancestors` flag
- Standardized exported P-Chain codec usage
- Improved isolation and execution of the E2E tests
- Updated the linked hashmap implementation to use generics

## [v1.8.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.6)

This version is backwards compatible to [v1.8.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0). It is optional, but encouraged. The supported plugin version is `16`.

### BLS

- Added BLS key file at `--staking-signer-key-file`
- Exposed BLS proof of possession in the `info.getNodeID` API
- Added BLS proof of possession to `AddPermissionlessValidatorTx`s for the Primary Network

The default value of `--staking-signer-key-file` is `~/.avalanchego/staking/signer.key`. If the key file doesn't exist, it will be populated with a new key.

### Networking

- Added P2P proto support to be activated in a future release
- Fixed inbound bandwidth spike after leaving the validation set
- Removed support for `ChitsV2` messages
- Removed `ContainerID`s from `Put` and `PushQuery` messages
- Added `pending_timeouts` metric to track the number of active timeouts a node is tracking
- Fixed overflow in gzip decompression
- Optimized memory usage in `peer.MessageQueue`

### Miscellaneous

- Fixed bootstrapping ETA metric
- Removed unused `unknown_txs_count` metric
- Replaced duplicated code with generic implementations

### Coreth

- Added failure reason to bad block API

## [v1.8.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.5)

Please upgrade your node as soon as possible.

The supported plugin version is `16`.

### Fixes

- Fixed stale block reference by evicting blocks upon successful verification

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Removed check for Apricot Phase6 incompatible fork to unblock nodes that did not upgrade ahead of the activation time

## [v1.8.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.4)

Please upgrade your node as soon as possible.

The supported plugin version is `16`.

### Caching

- Added temporarily invalid block caching to reduce repeated network requests
- Added caching to the proposervm's inner block parsing

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Reduced the log level of `BAD BLOCK`s from `ERROR` to `DEBUG`
- Deprecated Native Asset Call

## [v1.8.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.2)

Please upgrade your node as soon as possible.

The changes in `v1.8.x` go into effect at 4 PM EDT on September 6th, 2022 on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

The supported plugin version is `16`.

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Fixed live-lock in bootstrapping, after performing state-sync, by properly reporting `database.ErrNotFound` in `GetBlockIDAtHeight` rather than a formatted error
- Increased the log level of `BAD BLOCK`s from `DEBUG` to `ERROR`
- Fixed typo in Chain Config `String` function

## [v1.8.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.1)

Please upgrade your node as soon as possible.

The changes in `v1.8.x` go into effect at 4 PM EDT on September 6th, 2022 on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

The supported plugin version is `16`.

### Miscellaneous

- Reduced the severity of not quickly connecting to bootstrap nodes from `FATAL` to `WARN`

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Reduced the log level of `BAD BLOCK`s from `ERROR` to `DEBUG`
- Added Apricot Phase6 to Chain Config `String` function

## [v1.8.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.8.0)

This is a mandatory security upgrade. Please upgrade your node **as soon as possible.**

The changes in the upgrade go into effect at **4 PM EDT on September 6th, 2022** on both Fuji and Mainnet. You should upgrade your node before the changes go into effect, otherwise they may experience loss of uptime.

You may see some extraneous ERROR logs ("BAD BLOCK") on your node after upgrading. These may continue until the Apricot Phase 6 activation (at 4 PM EDT on September 6th).

The supported plugin version is `16`.

### PlatformVM APIs

- Fixed `GetBlock` API when requesting the encoding as `json`
- Changed the json key in `AddSubnetValidatorTx`s from `subnet` to `subnetID`
- Added multiple asset support to `getBalance`
- Updated `PermissionlessValidator`s returned from `getCurrentValidators` and `getPendingValidators` to include `validationRewardOwner` and `delegationRewardOwner`
- Deprecated `rewardOwner` in `PermissionlessValidator`s returned from `getCurrentValidators` and `getPendingValidators`
- Added `subnetID` argument to `getCurrentSupply`
- Added multiple asset support to `getStake`
- Added `subnetID` argument to `getMinStake`

### PlatformVM Structures

- Renamed existing blocks
  - `ProposalBlock` -> `ApricotProposalBlock`
  - `AbortBlock` -> `ApricotAbortBlock`
  - `CommitBlock` -> `ApricotCommitBlock`
  - `StandardBlock` -> `ApricotStandardBlock`
  - `AtomicBlock` -> `ApricotAtomicBlock`
- Added new block types **to be enabled in a future release**
  - `BlueberryProposalBlock`
    - Introduces a `Time` field and an unused `Txs` field before the remaining `ApricotProposalBlock` fields
  - `BlueberryAbortBlock`
    - Introduces a `Time` field before the remaining `ApricotAbortBlock` fields
  - `BlueberryCommitBlock`
    - Introduces a `Time` field before the remaining `ApricotCommitBlock` fields
  - `BlueberryStandardBlock`
    - Introduces a `Time` field before the remaining `ApricotStandardBlock` fields
- Added new transaction types **to be enabled in a future release**
  - `RemoveSubnetValidatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Allows a subnet owner to remove a validator from their subnet
  - `TransformSubnetTx`
    - Can be included into `BlueberryStandardBlock`s
    - Allows a subnet owner to convert their subnet into a permissionless subnet
  - `AddPermissionlessValidatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Adds a new validator to the requested permissionless subnet
  - `AddPermissionlessDelegatorTx`
    - Can be included into `BlueberryStandardBlock`s
    - Adds a new delegator to the requested permissionless validator on the requested subnet

### PlatformVM Block Building

- Fixed race in `AdvanceTimeTx` creation to avoid unnecessary block construction
- Added `block_formation_logic.md` to describe how blocks are created
- Refactored `BlockBuilder` into `ApricotBlockBuilder`
- Added `BlueberryBlockBuilder`
- Added `OptionBlock` builder visitor
- Refactored `Mempool` issuance and removal logic to use transaction visitors

### PlatformVM Block Execution

- Added support for executing `AddValidatorTx`, `AddDelegatorTx`, and `AddSubnetValidatorTx` inside of a `BlueberryStandardBlock`
- Refactored time advancement into a standard state modification structure
- Refactored `ProposalTxExecutor` to abstract state diff creation
- Standardized upgrade checking rules
- Refactored subnet authorization checking

### Wallet

- Added support for new transaction types in the P-chain wallet
- Fixed fee amounts used in the Primary Network wallet to reduce unnecessary fee burning

### Networking

- Defined `p2p.proto` to be used for future network messages
- Added `--network-tls-key-log-file-unsafe` to support inspecting p2p messages
- Added `avalanche_network_accept_failed` metrics to track networking `Accept` errors

### Miscellaneous

- Removed reserved fields from proto files and renumbered the existing fields
- Added generic dynamically resized ring buffer
- Updated gRPC version to `v1.49.0` to fix non-deterministic errors reported in the `rpcchainvm`
- Removed `--signature-verification-enabled` flag
- Removed dead code
  - `ids.QueueSet`
  - `timer.Repeater`
  - `timer.NewStagedTimer`
  - `timer.TimedMeter`

### [Coreth](https://medium.com/avalancheavax/apricot-phase-6-native-asset-call-deprecation-a7b7a77b850a)

- Incorrectly deprecated Native Asset Call
- Migrated to go-ethereum v1.10.23
- Added API to fetch Chain Config

## [v1.7.18](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.18)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### Fixes

- Fixed bug in `codeToFetch` database accessors that caused an error when starting/stopping state sync
- Fixed rare BAD BLOCK errors during C-chain bootstrapping
- Fixed platformvm `couldn't get preferred block state` log due to attempted block building during bootstrapping
- Fixed platformvm `failed to fetch next staker to reward` error log due to an incorrect `lastAcceptedID` reference
- Fixed AWS AMI creation

### PlatformVM

- Refactored platformvm metrics handling
- Refactored platformvm block creation
- Introduced support to prevent empty nodeID use on the P-chain to be activated in a future upgrade

### Coreth

- Updated gas price estimation to limit lookback window based on block timestamps
- Added metrics for processed/accepted gas
- Simplified syntactic block verification
- Ensured statedb errors during block processing are logged
- Removed deprecated gossiper/block building logic from pre-Apricot Phase 4
- Added marshal function for duration to improve config output

### Miscellaneous

- Updated local network genesis to use a newer start time
- Updated minimum golang version to go1.18.1
- Removed support for RocksDB
- Bumped go-ethereum version to v1.10.21
- Added various additional tests
- Introduced additional database invariants for all database implementations
- Added retries to windows CI installations
- Removed useless ID aliasing during chain creation

## [v1.7.17](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.17)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### VMs

- Refactored P-chain block state management
  - Supporting easier parsing and usage of blocks
  - Improving separation of block execution with block definition
  - Unifying state definitions
- Introduced support to send custom X-chain assets to the P-chain to be activated in a future upgrade
- Introduced support to use custom assets on the P-chain to be activated in a future upgrade
- Added VMs README to begin fully documenting plugin invariants
- Added various comments around expected usages of VM tools

### Coreth

- Added optional JSON logging
- Added interface for supporting stateful precompiles
- Removed legacy code format from the database

### Fixes

- Fixed ungraceful gRPC connection closure during very long running requests
- Fixed LevelDB panic during shutdown
- Fixed verification of `--stake-max-consumption-rate` to include the upper-bound
- Fixed various CI failures
- Fixed flaky unit tests

### Miscellaneous

- Added bootstrapping ETA metrics
- Converted all logs to support structured fields
- Improved Snowman++ oracle block verification error messages
- Removed deprecated or unused scripts

## [v1.7.16](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.16)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### LevelDB

- Fix rapid disk growth by manually specifying the maximum manifest file size

## [v1.7.15](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.15)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged. The supported plugin version is `15`.

### PlatformVM

- Replaced copy-on-write validator set data-structure to use tree diffs to optimize validator set additions
- Replaced validation transactions with a standardized representation to remove transaction type handling
- Migrated transaction execution to its own package
- Removed child pointers from processing blocks
- Added P-chain wallet helper for providing initial transactions

### Coreth

- Bumped go-ethereum dependency to v1.10.20
- Updated API names used to enable services in `eth-api` config flag. Prior names are supported but deprecated, please update configurations [accordingly](https://docs.avax.network/nodes/maintain/chain-config-flags#c-chain-configs)
- Optimized state sync by parallelizing trie syncing
- Added `eth_syncing` API for compatibility. Note: This API is only accessible after bootstrapping and always returns `"false"`, since the node will no longer be syncing at that point
- Added metrics to the atomic transaction mempool
- Added metrics for incoming/outgoing mempool gossip

### Fixes

- Updated Snowman and Avalanche consensus engines to report original container preferences before processing the provided container
- Fixed inbound message byte throttler context cancellation cleanup
- Removed case sensitivity of IP resolver services
- Added failing health check when a whitelisted subnet fails to initialize a chain

### Miscellaneous

- Added gRPC client metrics for dynamically created connections
- Added uninitialized continuous time averager for when initial predictions are unreliable
- Updated linter version
- Documented various platform invariants
- Cleaned up various dead parameters
- Improved various tests

## [v1.7.14](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.14)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### APIs

**These API format changes are breaking changes. https://api.avax.network and https://api.avax-test.network have been updated with this format. If you are using AvalancheGo APIs in your code, please ensure you have updated to the latest versions. See  https://docs.avax.network/apis/avalanchego/cb58-deprecation for details about the CB58 removal.**

- Removed `CB58` as an encoding option from all APIs
- Added `HexC` and `HexNC` as encoding options for all APIs that accept an encoding format
- Removed the `Success` response from all APIs
- Replaced `containerID` with `id` in the indexer API

### PlatformVM

- Fixed incorrect `P-chain` height in `Snowman++` when staking is disabled
- Moved `platformvm` transactions to be defined in a sub-package
- Moved `platformvm` genesis management to be defined in a sub-package
- Moved `platformvm` state to be defined in a sub-package
- Standardized `platformvm` transactions to always be referenced via pointer
- Moved the `platformvm` transaction builder to be defined in a sub-package
- Fixed uptime rounding during node shutdown

### Coreth

- Bumped go-ethereum dependency to v1.10.18
- Parallelized state sync code fetching

### Networking

- Updated `Connected` and `Disconnected` messages to only be sent to chains if the peer is tracking the subnet
- Updated the minimum TLS version on the p2p network to `v1.3`
- Supported context cancellation in the networking rate limiters
- Added `ChitsV2` message format for the p2p network to be used in a future upgrade

### Miscellaneous

- Fixed `--public-ip-resolution-frequency` invalid overwrite of the resolution service
- Added additional metrics to distinguish between virtuous and rogue currently processing transactions
- Suppressed the super cool `avalanchego` banner when `stdout` is not directed to a terminal
- Updated linter version
- Improved various comments and documentation
- Standardized primary network handling across subnet maps

## [v1.7.13](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.13)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### State Sync

- Added peer bandwidth tracking to optimize `coreth` state sync message routing
- Fixed `coreth` leaf request handler bug to ensure the handler delivers a valid range proof
- Removed redundant proof keys from `coreth` leafs response message format
- Improved `coreth` state sync request retry logic
- Improved `coreth` state sync handler metrics
- Improved `coreth` state sync ETA
- Added `avalanche_{chainID}_handler_async_expired` metric

### Miscellaneous

- Fixed `platform.getCurrentValidators` API to correctly mark a node as connected to itself on subnets.
- Fixed `platform.getBlockchainStatus` to correctly report `Unknown` for blockchains that are not managed by the `P-Chain`
- Added process metrics by default in the `rpcchainvm#Server`
- Added `Database` health checks
- Removed the deprecated `Database.Stat` call from the `rpcdb#Server`
- Added fail fast logic to duplicated Snowman additions to avoid undefined behavior
- Added additional testing around Snowman diverged voting tests
- Deprecated `--dynamic-update-duration` and `--dynamic-public-ip` CLI flags
- Added `--public-ip-resolution-frequency` and `--public-ip-resolution-service` to replace `--dynamic-update-duration` and `--dynamic-public-ip`, respectively

## [v1.7.12](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.12)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### State Sync

- Fixed proposervm state summary acceptance to only accept state summaries with heights higher than the locally last accepted block
- Fixed proposervm state summary serving to only respond to requests after height indexing has finished
- Improved C-chain state sync leaf request serving by optimistically reading leaves from snapshot
- Refactored C-chain state sync block fetching

### Networking

- Reduced default peerlist and accepted frontier gossipping
- Increased the default at-large outbound buffer size to 32 MiB

### Metrics

- Added leveldb metrics
- Added process and golang metrics for the avalanchego binary
- Added available disk space health check
  - Ensured that the disk space will not be fully utilized by shutting down the node if there is a critically low amount of free space remaining
- Improved C-chain state sync metrics

### Performance

- Added C-chain acceptor queue within `core/blockchain.go`
- Removed rpcdb locking when committing batches and using iterators
- Capped C-chain TrieDB dirties cache size during block acceptance to reduce commit size at 4096 block interval

### Cleanup

- Refactored the avm to utilize the external txs package
- Unified platformvm dropped tx handling
- Clarified snowman child block acceptance calls
- Fixed small consensus typos
- Reduced minor duplicated code in consensus
- Moved the platformvm key factory out of the VM into the test file
- Removed unused return values from the timeout manager
- Removed weird json rpc private interface
- Standardized json imports
- Added vm factory interface checks

## [v1.7.11](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.11)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

**The first startup of the C-Chain will cause an increase in CPU and IO usage due to an index update. This index update runs in the background and does not impact restart time.**

### State Sync

- Added state syncer engine to facilitate VM state syncing, rather than full historical syncing
- Added `GetStateSummaryFrontier`, `StateSummaryFrontier`, `GetAcceptedStateSummary`, `AcceptedStateSummary` as P2P messages
- Updated `Ancestors` message specification to expect an empty response if the container is unknown
- Added `--state-sync-ips` and `--state-sync-ids` flags to allow manual overrides of which nodes to query for accepted state summaries
- Updated networking library to permanently track all manually tracked peers, rather than just beacons
- Added state sync support to the `metervm`
- Added state sync support to the `proposervm`
- Added state sync support to the `rpcchainvm`
- Added beta state sync support to `coreth`

### ProposerVM

- Prevented rejected blocks from overwriting the `proposervm` height index
- Optimized `proposervm` block rewind to utilize the height index if available
- Ensured `proposervm` height index is marked as repaired in `Initialize` if it is fully repaired on startup
- Removed `--reset-proposervm-height-index`. The height index will be reset upon first restart
- Optimized `proposervm` height index resetting to periodically flush deletions

### Bug Fixes

- Fixed IPC message issuance and restructured consensus event callbacks to be checked at compile time
- Fixed `coreth` metrics initialization
- Fixed bootstrapping startup logic to correctly startup if initially connected to enough stake
- Fixed `coreth` panic during metrics collection
- Fixed panic on concurrent map read/write in P-chain wallet SDK
- Fixed `rpcchainvm` panic by sanitizing http response codes
- Fixed incorrect JSON tag on `platformvm.BaseTx`
- Fixed `AppRequest`, `AppResponse`, and `AppGossip` stringers used in logging

### API/Client

- Supported client implementations pointing to non-standard URIs
- Introduced `ids.NodeID` type to standardize logging and simplify API service and client implementations
- Changed client implementations to use standard types rather than `string`s wherever possible
- Added `subnetID` as an argument to `platform.getTotalStake`
- Added `connected` to the subnet validators in responses to `platform.getCurrentValidators` and `platform.getPendingValidators`
- Add missing `admin` API client methods
- Improved `indexer` API client implementation to avoid encoding edge cases

### Networking

- Added `--snow-mixed-query-num-push-vdr` and `--snow-mixed-query-num-push-non-vdr` to allow parameterization of sending push queries
  - By default, non-validators now send only pull queries, not push queries.
  - By default, validators now send both pull queries and push queries upon inserting a container into consensus. Previously, nodes sent only push queries.
- Added metrics to track the amount of over gossipping of `peerlist` messages
- Added custom message queueing support to outbound `Peer` messages
- Reused `Ping` messages to avoid needless memory allocations

### Logging

- Replaced AvalancheGo's internal logger with [uber-go/zap](https://github.com/uber-go/zap).
- Replaced AvalancheGo's log rotation with [lumberjack](https://github.com/natefinch/lumberjack).
- Renamed `log-display-highlight` to `log-format` and added `json` option.
- Added `log-rotater-max-size`, `log-rotater-max-files`, `log-rotater-max-age`, `log-rotater-compress-enabled` options for log rotation.

### Miscellaneous

- Added `--data-dir` flag to easily move all default file locations to a custom location
- Standardized RPC specification of timestamp fields
- Logged health checks whenever a failing health check is queried
- Added callback support for the validator set manager
- Increased `coreth` trie tip buffer size to 32
- Added CPU usage metrics for AvalancheGo and all sub-processes
- Added Disk IO usage metrics for AvalancheGo and all sub-processes

### Cleanup

- Refactored easily separable `platformvm` files into separate smaller packages
- Simplified default version parsing
- Fixed various typos
- Converted some structs to interfaces to better support mocked testing
- Refactored IP utils

### Documentation

- Increased recommended disk size to 1 TB
- Updated issue template
- Documented additional `snowman.Block` invariants

## [v1.7.10](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.10)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Improved vertex and block gossiping for validators with low stake weight.
- Added peers metric by subnet.
- Added percentage of stake connected metric by subnet.

### APIs

- Added support for specifying additional headers and query params in the RPC client implementations.
- Added static API clients for the `platformvm` and the `avm`.

### PlatformVM

- Introduced time based windowing of accepted P-chain block heights to ensure that local networks update the proposer list timely in the `proposervm`.
- Improved selection of decision transactions from the mempool.

### RPCChainVM

- Increased `buf` version to `v1.3.1`.
- Migrated all proto definitions to a dedicated `/proto` folder.
- Removed the dependency on the non-standard grpc broker to better support other language implementations.
- Added grpc metrics.
- Added grpc server health checks.

### Coreth

- Fixed a bug where a deadlock on shutdown caused historical re-generation on restart.
- Added an API endpoint to fetch the current VM Config.
- Added AvalancheGo custom log formatting to the logs.
- Removed support for the JS Tracer.

### Logging

- Added piping of subnet logs to stdout.
- Lazily initialized logs to avoid opening files that are never written to.
- Added support for arbitrarily deleted log files while avalanchego is running.
- Removed redundant logging configs.

### Miscellaneous

- Updated minimum go version to `v1.17.9`.
- Added subnet bootstrapping health checks.
- Supported multiple tags per codec instantiation.
- Added minor fail-fast optimization to string packing.
- Removed dead code.
- Fixed typos.
- Simplified consensus engine `Shutdown` notification dispatching.
- Removed `Sleep` call in the inbound connection throttler.

## [v1.7.9](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.9)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Updates

- Improved subnet gossip to only send messages to nodes participating in that subnet.
- Fixed inlined VM initialization to correctly register static APIs.
- Added logging for file descriptor limit errors.
- Removed dead code from network packer.
- Improved logging of invalid hash length errors.

## [v1.7.8](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.8)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Fixed duplicate reference decrease when closing a peer.
- Freed allocated message buffers immediately after sending.
- Added `--network-peer-read-buffer-size` and `--network-peer-write-buffer-size` config options.
- Moved peer IP signature verification to enable concurrent verifications.
- Reduced the number of connection flushes when sending messages.
- Canceled outbound connection requests on shutdown.
- Reused dialer across multiple outbound connections.
- Exported `NewTestNetwork` for easier external testing.

### Coreth

- Reduced log level of snapshot regeneration logs.
- Enabled atomic tx replacement with higher gas fees.
- Parallelized trie index re-generation.

### Miscellaneous

- Fixed incorrect `BlockchainID` usage in the X-chain `ImportTx` builder.
- Fixed incorrect `OutputOwners` in the P-chain `ImportTx` builder.
- Improved FD limit error logging and warnings.
- Rounded bootstrapping ETAs to the nearest second.
- Added gossip config support to the subnet configs.
- Optimized various queue removals for improved memory freeing.
- Added a basic X-chain E2E usage test to the new testing framework.

## [v1.7.7](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.7)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Networking

- Refactored the networking library to track potential peers by nodeID rather than IP.
- Separated peer connections from the mesh network implementation to simplify testing.
- Fixed duplicate `Connected` messages bug.
- Supported establishing outbound connections with peers reporting different inbound and outbound IPs.

### Database

- Disabled seek compaction in leveldb by default.

### GRPC

- Increased protocol version, this requires all plugin definitions to update their communication dependencies.
- Merged services to be served using the same server when possible.
- Implemented a fast path for simple HTTP requests.
- Removed duplicated message definitions.
- Improved error reporting around invalid plugins.

### Coreth

- Optimized FeeHistory API.
- Added protection to prevent accidental corruption of archival node trie index.
- Added capability to restore complete trie index on best effort basis.
- Rounded up fastcache sizes to utilize all mmap'd memory in chunks of 64MB.

### Configs

- Removed `--inbound-connection-throttling-max-recent`
- Renamed `--network-peer-list-size` to `--network-peer-list-num-validator-ips`
- Removed `--network-peer-list-gossip-size`
- Removed `--network-peer-list-staker-gossip-fraction`
- Added `--network-peer-list-validator-gossip-size`
- Added `--network-peer-list-non-validator-gossip-size`
- Removed `--network-get-version-timeout`
- Removed `--benchlist-peer-summary-enabled`
- Removed `--peer-alias-timeout`

### Miscellaneous

- Fixed error reporting when making Avalanche chains that did not manually specify a primary alias.
- Added beacon utils for easier programmatic handling of beacon nodes.
- Resolved the default log directory on initialization to avoid additional error handling.
- Added support to the chain state module to specify an arbitrary new accepted block.

## [v1.7.6](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.6)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Introduced a new vertex type to support future `Avalanche` based network upgrades.
- Added pending message metrics to the chain message queues.
- Refactored event dispatchers to simplify dependencies and remove dead code.

### PlatformVM

- Added `json` encoding option to the `platform.getTx` call.
- Added `platform.getBlock` API.
- Cleaned up block building logic to be more modular and testable.

### Coreth

- Increased `FeeHistory` maximum historical limit to improve MetaMask UI on the C-Chain.
- Enabled chain state metrics.
- Migrated go-ethereum v1.10.16 changes.

### Miscellaneous

- Added the ability to load new VM plugins dynamically.
- Implemented X-chain + P-chain wallet that can be used to build and sign transactions. Without providing a full node private keys.
- Integrated e2e testing to the repo to avoid maintaining multiple synced repos.
- Fixed `proposervm` height indexing check to correctly mark the indexer as repaired.
- Introduced message throttling overrides to be used in future improvements to reliably send messages.
- Introduced a cap on the client specified request deadline.
- Increased the default `leveldb` open files limit to `1024`.
- Documented the `leveldb` configurations.
- Extended chain shutdown timeout.
- Performed various cleanup passes.

## [v1.7.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.5)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Added asynchronous processing of `App.*` messages.
- Added height indexing support to the `proposervm` and `rpcchainvm`. If a node is updated to `>=v1.7.5` and then downgraded to `<v1.7.5`, the user must enable the `--reset-proposervm-height-index=true` flag to ensure the `proposervm` height index is correctly updated going forward.
- Fixed bootstrapping job counter initialization that could cause negative ETAs to be reported.
- Fixed incorrect processing check that could log incorrect information.
- Removed incorrect warning logs.

### Miscellaneous

- Added tracked subnets to be reported in calls to the `info.peers` API.
- Updated gRPC implementations to use `buf` tooling and standardized naming and locations.
- Added a consistent hashing implementation to be used in future improvements.
- Fixed database iteration invariants to report `ErrClosed` rather than silently exiting.
- Added additional sanity checks to prevent users from incorrectly configuring their node.
- Updated log timestamps to include milliseconds.

### Coreth

- Added beta support for offline pruning.
- Refactored peer networking layer.
- Enabled cheap metrics by default.
- Marked RPC call metrics as expensive.
- Added Abigen support for native asset call precompile.
- Fixed bug in BLOCKHASH opcode during traceBlock.
- Fixed bug in handling updated chain config on startup.

## [v1.7.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.4)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

**The first startup of the C-Chain will take a few minutes longer due to an index update.**

### Consensus

- Removed deprecated Snowstorm consensus implementation that no longer aligned with the updated specification.
- Updated bootstrapping logs to no longer reset counters after a node restart.
- Added bootstrapping ETAs for fetching Snowman blocks and executing operations.
- Renamed the `MultiPut` message to the `Ancestors` message to match other message naming conventions.
- Introduced Whitelist conflicts into the Snowstorm specification that will be used in future X-chain improvements.
- Refactored the separation between the Bootstrapping engine and the Consensus engine to support Fast-Sync.

### Coreth

- Added an index mapping height to the list of accepted atomic operations at that height in a trie. Generating this index will cause the node to take a few minutes longer to startup the C-Chain for the first restart.
- Updated Geth dependency to `v1.10.15`.
- Updated `networkID` to match `chainID`.

### VMs

- Refactored `platformvm` rewards calculations to enable usage from an external library.
- Fixed `platformvm` and `avm` UTXO fetching to not re-iterate the UTXO set if no UTXOs are fetched.
- Refactored `platformvm` status definitions.
- Added support for multiple address balance lookups in the `platformvm`.
- Refactored `platformvm` and `avm` keystore users to reuse similar code.

### RPCChainVM

- Returned a `500 InternalServerError` if an unexpected gRPC error occurs during the handling of an HTTP request to a plugin.
- Updated gRPC server's max message size to enable responses larger than 4MiB from the plugin's handling of an HTTP request.

### Configs

- Added `--stake-max-consumption-rate` which defaults to `120,000`.
- Added `--stake-min-consumption-rate` which defaults to `100,000`.
- Added `--stake-supply-cap` which defaults to `720,000,000,000,000,000` nAVAX.
- Renamed `--bootstrap-multiput-max-containers-sent` to `--bootstrap-ancestors-max-containers-sent`.
- Renamed `--bootstrap-multiput-max-containers-received` to `--bootstrap-ancestors-max-containers-received`.
- Enforced that `--staking-enabled=false` can not be specified on public networks (`Fuji` and `Mainnet`).

### Metrics

- All `multi_put` metrics were converted to `ancestors` metrics.

### Miscellaneous

- Improved `corruptabledb` error reporting by tracking the first reported error.
- Updated CPU tracking to use the proper EWMA tracker rather than a linear approximation.
- Separated health checks into `readiness`, `healthiness`, and `liveness` checks to support more fine-grained monitoring.
- Refactored API client utilities to use a `Context` rather than an explicit timeout.

## [v1.7.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.3)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Consensus

- Introduced a notion of vertex conflicts that will be used in future X-chain improvements.

### Coreth

- Added an index mapping height to the list of accepted atomic transactions at that height. Generating this index will cause the node to take approximately 2 minutes longer to startup the C-Chain for the first restart.
- Fixed bug in base fee estimation API that impacted custom defined networks.
- Decreased minimum transaction re-gossiping interval from 1s to 500ms.
- Removed websocket handler from the static vm APIs.

### Database

- Reduced lock contention in `prefixDB`s.

### Networking

- Increase the gossip size from `6` to `10` validators.
- Prioritized `Connected` and `Disconnected` messages in the message handler.

### Miscellaneous

- Notified VMs of peer versions on `Connected`.
- Fixed acceptance broadcasting over IPC.
- Fixed 32-bit architecture builds for AvalancheGo (not Coreth).

## [v1.7.2](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.2)

This version is backwards compatible to [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). It is optional, but encouraged.

### Coreth

- Fixed memory leak in the estimate gas API.
- Reduced the default RPC gas limit to 50,000,000 gas.
- Improved RPC logging.
- Removed pre-AP5 legacy code.

### PlatformVM

- Optimized validator set change calculations.
- Removed storage of non-decided blocks.
- Simplified error handling.
- Removed pre-AP5 legacy code.

### Networking

- Explicitly fail requests with responses that failed to be parsed.
- Removed pre-AP5 legacy code.

### Configs

- Introduced the ability for a delayed graceful node shutdown.
- Added the ability to take all configs as environment variables for containerized deployments.

### Utils

- Fixed panic bug in logging library when importing from external projects.

## [v1.7.1](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.1)

This update is backwards compatible with [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0). Please see the expected update times in the v1.7.0 release.

### Coreth

- Reduced fee estimate volatility.

### Consensus

- Fixed vote bubbling for unverified block chits.

## [v1.7.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.7.0)

This upgrade adds support for issuing multiple atomic transactions into a single block and directly transferring assets between the P-chain and the C-chain.

The changes in the upgrade go into effect at 1 PM EST, December 2nd 2021 on Mainnet. One should upgrade their node before the changes go into effect, otherwise they may experience loss of uptime.

**All nodes should upgrade before 1 PM EST, December 2nd 2021.**

### Networking

- Added peer uptime reports as metrics.
- Removed IP rate limiting over local networks.

### PlatformVM

- Enabled `AtomicTx`s to be issued into `StandardBlock`s and deprecated `AtomicBlock`s.
- Added the ability to export/import AVAX to/from the C-chain.

### Coreth

- Enabled multiple `AtomicTx`s to be issued per block.
- Added the ability to export/import AVAX to/from the P-chain.
- Updated dynamic fee calculations.

### ProposerVM

- Removed storage of undecided blocks.

### RPCChainVM

- Added support for metrics to be reported by plugin VMs.

### Configs

- Removed `--snow-epoch-first-transition` and `snow-epoch-duration` as command line arguments.

## [v1.6.5](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.5)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Bootstrapping

- Drop inbound messages to a chain if that chain is in the execution phase of bootstrapping.
- Print beacon nodeIDs upon failure to connect to them.

### Metrics

- Added `avalanche_{ChainID}_bootstrap_finished`, which is 1 if the chain is done bootstrapping, 0 otherwise.

### APIs

- Added `info.uptime` API call that attempts to report the network's view of the local node.
- Added `observedUptime` to each peer's result in `info.peers`.

### Network

- Added reported uptime to pong messages to be able to better track a local node's uptime as viewed by the network.
- Refactored request timeout registry to avoid a potential race condition.

## [v1.6.4](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.4)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Config

- Added flag `throttler-inbound-bandwidth-refill-rate`, which specifies the max average inbound bandwidth usage of a peer.
- Added flag `throttler-inbound-bandwidth-max-burst-size`, which specifies the max inbound bandwidth usage of a peer.

### Networking

- Updated peerlist gossiping to use the same mechanism as other gossip calls.
- Added inbound message throttling based on recent bandwidth usage.

### Metrics

- Updated `avalanche_{ChainID}_handler_gossip_{count,sum}` to `avalanche_{ChainID}_handler_gossip_request_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_{count,sum}` to `avalanche_{ChainID}_lat_accepted_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_accepted_frontier_{count,sum}` to `avalanche_{ChainID}_lat_accepted_frontier_{count,sum}`.
- Updated `avalanche_{ChainID}_lat_get_ancestors_{count,sum}` to `avalanche_{ChainID}_lat_multi_put_{count,sum}`.
- Combined `avalanche_{ChainID}_lat_pull_query_{count,sum}` and `avalanche_{ChainID}_lat_push_query_{count,sum}` to `avalanche_{ChainID}_lat_chits_{count,sum}`.
- Added `avalanche_{ChainID}_app_response_{count,sum}`.
- Added `avalanche_network_bandwidth_throttler_inbound_acquire_latency_{count,sum}`
- Added `avalanche_network_bandwidth_throttler_inbound_awaiting_acquire`
- Added `avalanche_P_vm_votes_won`
- Added `avalanche_P_vm_votes_lost`

### Indexer

- Added method `GetContainerByID` to client implementation.
- Client methods now return `[]byte` rather than `string` representations of a container.

### C-Chain

- Updated Geth dependency to 1.10.11.
- Added a new admin API for updating the log level and measuring performance.
- Added a new `--allow-unprotected-txs` flag to allow issuance of transactions without EIP-155 replay protection.

### Subnet & Custom VMs

- Ensured that all possible chains are run in `--staking-enabled=false` networks.

---

## [v1.6.3](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.3)

This version is backwards compatible to [v1.6.0](https://github.com/ava-labs/avalanchego/releases/tag/v1.6.0). It is optional, but encouraged.

### Config Options

- Updated the default value of `--inbound-connection-throttling-max-conns-per-sec` to `256`.
- Updated the default value of `--meter-vms-enabled` to `true`.
- Updated the default value of `--staking-disabled-weight` to `100`.

### Metrics

- Changed the behavior of `avalanche_network_buffer_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Changed the behavior of `avalanche_network_byte_throttler_inbound_awaiting_acquire` to only increment if the message is actually blocking.
- Added `Block/Tx` metrics on `meterVM`s.
  - Added `avalanche_{ChainID}_vm_metervm_build_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_parse_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_get_block_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_verify_err_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{ChainID}_vm_metervm_reject_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_parse_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_get_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_verify_tx_err_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_accept_{count,sum}`.
  - Added `avalanche_{DAGID}_vm_metervm_reject_{count,sum}`.

### Coreth

- Applied callTracer fault handling fix.
- Initialized multicoin functions in the runtime environment.

### ProposerVM

- Updated block `Delay` in `--staking-enabled=false` networks to be `0`.

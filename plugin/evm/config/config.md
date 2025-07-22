In order to specify a config for the C-Chain, a JSON config file should be placed at `{chain-config-dir}/C/config.json`. This file does not exist by default.

For example if `chain-config-dir` has the default value which is `$HOME/.avalanchego/configs/chains`, then `config.json` should be placed at `$HOME/.avalanchego/configs/chains/C/config.json`.

The C-Chain config is printed out in the log when a node starts. Default values for each config flag are specified below.

Default values are overridden only if specified in the given config file. It is recommended to only provide values which are different from the default, as that makes the config more resilient to future default changes. Otherwise, if defaults change, your node will remain with the old values, which might adversely affect your node operation.

## Gas Configuration

### `gas-target`

_Integer_

The target gas per second that this node will attempt to use when creating blocks. If this config is not specified, the node will default to use the parent block's target gas per second. Defaults to using the parent block's target.

## State Sync

### `state-sync-enabled`

_Boolean_

Set to `true` to start the chain with state sync enabled. The peer will download chain state from peers up to a recent block near tip, then proceed with normal bootstrapping.

Defaults to perform state sync if starting a new node from scratch. However, if running with an existing database it will default to false and not perform state sync on subsequent runs.

Please note that if you need historical data, state sync isn't the right option. However, it is sufficient if you are just running a validator.

### `state-sync-skip-resume`

_Boolean_

If set to `true`, the chain will not resume a previously started state sync operation that did not complete. Normally, the chain should be able to resume state syncing without any issue. Defaults to `false`.

### `state-sync-min-blocks`

_Integer_

Minimum number of blocks the chain should be ahead of the local node to prefer state syncing over bootstrapping. If the node's database is already close to the chain's tip, bootstrapping is more efficient. Defaults to `300000`.

### `state-sync-ids`

_String_

Comma separated list of node IDs (prefixed with `NodeID-`) to fetch state sync data from. An example setting of this field would be `--state-sync-ids="NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg,NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ"`. If not specified (or empty), peers are selected at random. Defaults to empty string (`""`).

### `state-sync-server-trie-cache`

_Integer_

Size of trie cache used for providing state sync data to peers in MBs. Should be a multiple of `64`. Defaults to `64`.

### `state-sync-commit-interval`

_Integer_

Specifies the commit interval at which to persist EVM and atomic tries during state sync. Defaults to `16384`.

### `state-sync-request-size`

_Integer_

The number of key/values to ask peers for per state sync request. Defaults to `1024`.

## Continuous Profiling

### `continuous-profiler-dir`

_String_

Enables the continuous profiler (captures a CPU/Memory/Lock profile at a specified interval). Defaults to `""`. If a non-empty string is provided, it enables the continuous profiler and specifies the directory to place the profiles in.

### `continuous-profiler-frequency`

_Duration_

Specifies the frequency to run the continuous profiler. Defaults `900000000000` nano seconds which is 15 minutes.

### `continuous-profiler-max-files`

_Integer_

Specifies the maximum number of profiles to keep before removing the oldest. Defaults to `5`.

## Enabling Avalanche Specific APIs

### `admin-api-enabled`

_Boolean_

Enables the Admin API. Defaults to `false`.

### `admin-api-dir`

_String_

Specifies the directory for the Admin API to use to store CPU/Mem/Lock Profiles. Defaults to `""`.

### `warp-api-enabled`

_Boolean_

Enables the Warp API. Defaults to `false`.

### Enabling EVM APIs

### `eth-apis` (\[\]string)

Use the `eth-apis` field to specify the exact set of below services to enable on your node. If this field is not set, then the default list will be: `["eth","eth-filter","net","web3","internal-eth","internal-blockchain","internal-transaction"]`.

<Callout title="Note">
The names used in this configuration flag have been updated in Coreth `v0.8.14`. The previous names containing `public-` and `private-` are deprecated. While the current version continues to accept deprecated values, they may not be supported in future updates and updating to the new values is recommended.

The mapping of deprecated values and their updated equivalent follows:

|Deprecated                      |Use instead         |
|--------------------------------|--------------------|
|public-eth                      |eth                 |
|public-eth-filter               |eth-filter          |
|private-admin                   |admin               |
|private-debug                   |debug               |
|public-debug                    |debug               |
|internal-public-eth             |internal-eth        |
|internal-public-blockchain      |internal-blockchain |
|internal-public-transaction-pool|internal-transaction|
|internal-public-tx-pool         |internal-tx-pool    |
|internal-public-debug           |internal-debug      |
|internal-private-debug          |internal-debug      |
|internal-public-account         |internal-account    |
|internal-private-personal       |internal-personal   |
</Callout>

<Callout title="Note">
If you populate this field, it will override the defaults so you must include every service you wish to enable.
</Callout>

### `eth`

The API name `public-eth` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `eth`.

Adds the following RPC calls to the `eth_*` namespace. Defaults to `true`.

`eth_coinbase` `eth_etherbase`

### `eth-filter`

The API name `public-eth-filter` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `eth-filter`.

Enables the public filter API for the `eth_*` namespace. Defaults to `true`.

Adds the following RPC calls (see [here](https://eth.wiki/json-rpc/API) for complete documentation):

- `eth_newPendingTransactionFilter`
- `eth_newPendingTransactions`
- `eth_newAcceptedTransactions`
- `eth_newBlockFilter`
- `eth_newHeads`
- `eth_logs`
- `eth_newFilter`
- `eth_getLogs`
- `eth_uninstallFilter`
- `eth_getFilterLogs`
- `eth_getFilterChanges`

### `admin`

The API name `private-admin` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `admin`.

Adds the following RPC calls to the `admin_*` namespace. Defaults to `false`.

- `admin_importChain`
- `admin_exportChain`

### `debug`

The API names `private-debug` and `public-debug` are deprecated as of v1.7.15, and the APIs previously under these names have been migrated to `debug`.

Adds the following RPC calls to the `debug_*` namespace. Defaults to `false`.

- `debug_dumpBlock`
- `debug_accountRange`
- `debug_preimage`
- `debug_getBadBlocks`
- `debug_storageRangeAt`
- `debug_getModifiedAccountsByNumber`
- `debug_getModifiedAccountsByHash`
- `debug_getAccessibleState`

### `net`

Adds the following RPC calls to the `net_*` namespace. Defaults to `true`.

- `net_listening`
- `net_peerCount`
- `net_version`

Note: Coreth is a virtual machine and does not have direct access to the networking layer, so `net_listening` always returns true and `net_peerCount` always returns 0. For accurate metrics on the network layer, users should use the AvalancheGo APIs.

### `debug-tracer`

Adds the following RPC calls to the `debug_*` namespace. Defaults to `false`.

- `debug_traceChain`
- `debug_traceBlockByNumber`
- `debug_traceBlockByHash`
- `debug_traceBlock`
- `debug_traceBadBlock`
- `debug_intermediateRoots`
- `debug_traceTransaction`
- `debug_traceCall`

### `web3`

Adds the following RPC calls to the `web3_*` namespace. Defaults to `true`.

- `web3_clientVersion`
- `web3_sha3`

### `internal-eth`

The API name `internal-public-eth` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-eth`.

Adds the following RPC calls to the `eth_*` namespace. Defaults to `true`.

- `eth_gasPrice`
- `eth_baseFee`
- `eth_maxPriorityFeePerGas`
- `eth_feeHistory`

### `internal-blockchain`

The API name `internal-public-blockchain` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-blockchain`.

Adds the following RPC calls to the `eth_*` namespace. Defaults to `true`.

- `eth_chainId`
- `eth_blockNumber`
- `eth_getBalance`
- `eth_getProof`
- `eth_getHeaderByNumber`
- `eth_getHeaderByHash`
- `eth_getBlockByNumber`
- `eth_getBlockByHash`
- `eth_getUncleBlockByNumberAndIndex`
- `eth_getUncleBlockByBlockHashAndIndex`
- `eth_getUncleCountByBlockNumber`
- `eth_getUncleCountByBlockHash`
- `eth_getCode`
- `eth_getStorageAt`
- `eth_call`
- `eth_estimateGas`
- `eth_createAccessList`

### `internal-transaction`

The API name `internal-public-transaction-pool` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-transaction`.

Adds the following RPC calls to the `eth_*` namespace. Defaults to `true`.

- `eth_getBlockTransactionCountByNumber`
- `eth_getBlockTransactionCountByHash`
- `eth_getTransactionByBlockNumberAndIndex`
- `eth_getTransactionByBlockHashAndIndex`
- `eth_getRawTransactionByBlockNumberAndIndex`
- `eth_getRawTransactionByBlockHashAndIndex`
- `eth_getTransactionCount`
- `eth_getTransactionByHash`
- `eth_getRawTransactionByHash`
- `eth_getTransactionReceipt`
- `eth_sendTransaction`
- `eth_fillTransaction`
- `eth_sendRawTransaction`
- `eth_sign`
- `eth_signTransaction`
- `eth_pendingTransactions`
- `eth_resend`

### `internal-tx-pool`

The API name `internal-public-tx-pool` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-tx-pool`.

Adds the following RPC calls to the `txpool_*` namespace. Defaults to `false`.

- `txpool_content`
- `txpool_contentFrom`
- `txpool_status`
- `txpool_inspect`

### `internal-debug`

The API names `internal-private-debug` and `internal-public-debug` are deprecated as of v1.7.15, and the APIs previously under these names have been migrated to `internal-debug`.

Adds the following RPC calls to the `debug_*` namespace. Defaults to `false`.

- `debug_getHeaderRlp`
- `debug_getBlockRlp`
- `debug_printBlock`
- `debug_chaindbProperty`
- `debug_chaindbCompact`

### `debug-handler`

Adds the following RPC calls to the `debug_*` namespace. Defaults to `false`.

- `debug_verbosity`
- `debug_vmodule`
- `debug_backtraceAt`
- `debug_memStats`
- `debug_gcStats`
- `debug_blockProfile`
- `debug_setBlockProfileRate`
- `debug_writeBlockProfile`
- `debug_mutexProfile`
- `debug_setMutexProfileFraction`
- `debug_writeMutexProfile`
- `debug_writeMemProfile`
- `debug_stacks`
- `debug_freeOSMemory`
- `debug_setGCPercent`

### `internal-account`

The API name `internal-public-account` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-account`.

Adds the following RPC calls to the `eth_*` namespace. Defaults to `true`.

- `eth_accounts`

### `internal-personal`

The API name `internal-private-personal` is deprecated as of v1.7.15, and the APIs previously under this name have been migrated to `internal-personal`.

Adds the following RPC calls to the `personal_*` namespace. Defaults to `false`.

- `personal_listAccounts`
- `personal_listWallets`
- `personal_openWallet`
- `personal_deriveAccount`
- `personal_newAccount`
- `personal_importRawKey`
- `personal_unlockAccount`
- `personal_lockAccount`
- `personal_sendTransaction`
- `personal_signTransaction`
- `personal_sign`
- `personal_ecRecover`
- `personal_signAndSendTransaction`
- `personal_initializeWallet`
- `personal_unpair`

## API Configuration

### `rpc-gas-cap`

_Integer_

The maximum gas to be consumed by an RPC Call (used in `eth_estimateGas` and `eth_call`). Defaults to `50,000,000`.

### `rpc-tx-fee-cap`

_Integer_

Global transaction fee (price \* `gaslimit`) cap (measured in AVAX) for send-transaction variants. Defaults to `100`.

### `api-max-duration`

_Duration_

Maximum API call duration. If API calls exceed this duration, they will time out. Defaults to `0` (no maximum).

### `api-max-blocks-per-request`

_Integer_

Maximum number of blocks to serve per `getLogs` request. Defaults to `0` (no maximum).

### `ws-cpu-refill-rate`

_Duration_

The refill rate specifies the maximum amount of CPU time to allot a single connection per second. Defaults to no maximum (`0`).

### `ws-cpu-max-stored`

_Duration_

Specifies the maximum amount of CPU time that can be stored for a single WS connection. Defaults to no maximum (`0`).

### `allow-unfinalized-queries`

Allows queries for unfinalized (not yet accepted) blocks/transactions. Defaults to `false`.

### `accepted-cache-size`

_Integer_

Specifies the depth to keep accepted headers and accepted logs in the cache. This is particularly useful to improve the performance of `eth_getLogs` for recent logs. Defaults to `32`.

### `http-body-limit`

_Integer_

Maximum size in bytes for HTTP request bodies. Defaults to `0` (no limit).

## Transaction Pool

### `local-txs-enabled`

_Boolean_

Enables local transaction handling (prioritizes transactions submitted through this node). Defaults to `false`.

### `allow-unprotected-txs`

_Boolean_

If `true`, the APIs will allow transactions that are not replay protected (EIP-155) to be issued through this node. Defaults to `false`.

### `allow-unprotected-tx-hashes`

_\[\]TxHash_

Specifies an array of transaction hashes that should be allowed to bypass replay protection. This flag is intended for node operators that want to explicitly allow specific transactions to be issued through their API. Defaults to an empty list.

### `price-options-slow-fee-percentage`

_Integer_

Percentage to apply for slow fee estimation. Defaults to `95`.

### `price-options-fast-fee-percentage`

_Integer_

Percentage to apply for fast fee estimation. Defaults to `105`.

### `price-options-max-tip`

_Integer_

Maximum tip in wei for fee estimation. Defaults to `20000000000` (20 Gwei).

#### `push-gossip-percent-stake`

_Float_

Percentage of the total stake to send transactions received over the RPC. Defaults to 0.9.

### `push-gossip-num-validators`

_Integer_

Number of validators to initially send transactions received over the RPC. Defaults to 100.

### `push-gossip-num-peers`

_Integer_

Number of peers to initially send transactions received over the RPC. Defaults to 0.

### `push-regossip-num-validators`

_Integer_

Number of validators to periodically send transactions received over the RPC. Defaults to 10.

### `push-regossip-num-peers`

_Integer_

Number of peers to periodically send transactions received over the RPC. Defaults to 0.

### `push-gossip-frequency`

_Duration_

Frequency to send transactions received over the RPC to peers. Defaults to `100000000` nano seconds which is 100 milliseconds.

### `pull-gossip-frequency`

_Duration_

Frequency to request transactions from peers. Defaults to `1000000000` nano seconds which is 1 second.

### `regossip-frequency`

_Duration_

Amount of time that should elapse before we attempt to re-gossip a transaction that was already gossiped once. Defaults to `30000000000` nano seconds which is 30 seconds.

### `tx-pool-price-limit`

_Integer_

Minimum gas price to enforce for acceptance into the pool. Defaults to 1 wei.

### `tx-pool-price-bump`

_Integer_

Minimum price bump percentage to replace an already existing transaction (nonce). Defaults to 10%.

### `tx-pool-account-slots`

_Integer_

Number of executable transaction slots guaranteed per account. Defaults to 16.

### `tx-pool-global-slots`

_Integer_

Maximum number of executable transaction slots for all accounts. Defaults to 5120.

### `tx-pool-account-queue`

_Integer_

Maximum number of non-executable transaction slots permitted per account. Defaults to 64.

### `tx-pool-global-queue`

_Integer_

Maximum number of non-executable transaction slots for all accounts. Defaults to 1024.

### `tx-pool-lifetime`

_Duration_

Maximum duration a non-executable transaction will be allowed in the poll. Defaults to `600000000000` nano seconds which is 10 minutes.

## Metrics

### `metrics-expensive-enabled`

_Boolean_

Enables expensive metrics. This includes Firewood metrics. Defaults to `true`.

## Snapshots

### `snapshot-wait`

_Boolean_

If `true`, waits for snapshot generation to complete before starting. Defaults to `false`.

### `snapshot-verification-enabled`

_Boolean_

If `true`, verifies the complete snapshot after it has been generated. Defaults to `false`.

## Logging

### `log-level`

_String_

Defines the log level for the chain. Must be one of `"trace"`, `"debug"`, `"info"`, `"warn"`, `"error"`, `"crit"`. Defaults to `"info"`.

### `log-json-format`

_Boolean_

If `true`, changes logs to JSON format. Defaults to `false`.

## Keystore Settings

### `keystore-directory`

_String_

The directory that contains private keys. Can be given as a relative path. If empty, uses a temporary directory at `coreth-keystore`. Defaults to the empty string (`""`).

### `keystore-external-signer`

_String_

Specifies an external URI for a clef-type signer. Defaults to the empty string (`""` as not enabled).

### `keystore-insecure-unlock-allowed`

_Boolean_

If `true`, allow users to unlock accounts in unsafe HTTP environment. Defaults to `false`.

## Database

### `state-scheme`

_String_

Can be one of `hash` or `firewood`. Defaults to `hash`.

__WARNING__: `firewood` scheme is untested in production.

### `trie-clean-cache`

_Integer_

Size of cache used for clean trie nodes (in MBs). Should be a multiple of `64`. Defaults to `512`.

### `trie-dirty-cache`

_Integer_

Size of cache used for dirty trie nodes (in MBs). When the dirty nodes exceed this limit, they are written to disk. Defaults to `512`.

### `trie-dirty-commit-target`

_Integer_

Memory limit to target in the dirty cache before performing a commit (in MBs). Defaults to `20`.

### `trie-prefetcher-parallelism`

_Integer_

Max concurrent disk reads trie pre-fetcher should perform at once. Defaults to `16`.

### `snapshot-cache`

_Integer_

Size of the snapshot disk layer clean cache (in MBs). Should be a multiple of `64`. Defaults to `256`.



### `acceptor-queue-limit`

_Integer_

Specifies the maximum number of blocks to queue during block acceptance before blocking on Accept. Defaults to `64`.

### `commit-interval`

_Integer_

Specifies the commit interval at which to persist the merkle trie to disk. Defaults to `4096`.

### `pruning-enabled`

_Boolean_

If `true`, database pruning of obsolete historical data will be enabled. This reduces the amount of data written to disk, but does not delete any state that is written to the disk previously. This flag should be set to `false` for nodes that need access to all data at historical roots. Pruning will be done only for new data. Defaults to `false` in v1.4.9, and `true` in subsequent versions.

<Callout title="Note">
If a node is ever run with `pruning-enabled` as `false` (archival mode), setting `pruning-enabled` to `true` will result in a warning and the node will shut down. This is to protect against unintentional misconfigurations of an archival node.
</Callout>

To override this and switch to pruning mode, in addition to `pruning-enabled: true`, `allow-missing-tries` should be set to `true` as well.

### `populate-missing-tries`

_uint64_

If non-nil, sets the starting point for repopulating missing tries to re-generate archival merkle forest.

To restore an archival merkle forest that has been corrupted (missing trie nodes for a section of the blockchain), specify the starting point of the last block on disk, where the full trie was available at that block to re-process blocks from that height onwards and re-generate the archival merkle forest on startup. This flag should be used once to re-generate the archival merkle forest and should be removed from the config after completion. This flag will cause the node to delay starting up while it re-processes old blocks.

### `populate-missing-tries-parallelism`

_Integer_

Number of concurrent readers to use when re-populating missing tries on startup. Defaults to 1024.

### `allow-missing-tries`

_Boolean_

If `true`, allows a node that was once configured as archival to switch to pruning mode. Defaults to `false`.

### `preimages-enabled`

_Boolean_

If `true`, enables preimages. Defaults to `false`.

### `prune-warp-db-enabled`

_Boolean_

If `true`, clears the warp database on startup. Defaults to `false`.

### `offline-pruning-enabled`

_Boolean_

If `true`, offline pruning will run on startup and block until it completes (approximately one hour on Mainnet). This will reduce the size of the database by deleting old trie nodes. **While performing offline pruning, your node will not be able to process blocks and will be considered offline.** While ongoing, the pruning process consumes a small amount of additional disk space (for deletion markers and the bloom filter). For more information see [here.](https://build.avax.network/docs/nodes/maintain/reduce-disk-usage#disk-space-considerations)

Since offline pruning deletes old state data, this should not be run on nodes that need to support archival API requests.

This is meant to be run manually, so after running with this flag once, it must be toggled back to false before running the node again. Therefore, you should run with this flag set to true and then set it to false on the subsequent run.

### `offline-pruning-bloom-filter-size`

_Integer_

This flag sets the size of the bloom filter to use in offline pruning (denominated in MB and defaulting to 512 MB). The bloom filter is kept in memory for efficient checks during pruning and is also written to disk to allow pruning to resume without re-generating the bloom filter.

The active state is added to the bloom filter before iterating the DB to find trie nodes that can be safely deleted, any trie nodes not in the bloom filter are considered safe for deletion. The size of the bloom filter may impact its false positive rate, which can impact the results of offline pruning. This is an advanced parameter that has been tuned to 512 MB and should not be changed without thoughtful consideration.

### `offline-pruning-data-directory`

_String_

This flag must be set when offline pruning is enabled and sets the directory that offline pruning will use to write its bloom filter to disk. This directory should not be changed in between runs until offline pruning has completed.

### `transaction-history`

_Integer_

Number of recent blocks for which to maintain transaction lookup indices in the database. If set to 0, transaction lookup indices will be maintained for all blocks. Defaults to `0`.

### `state-history`

_Integer_

The maximum number of blocks from head whose state histories are reserved for pruning blockchains. Defaults to `32`.

### `historical-proof-query-window`

_Integer_

When running in archive mode only, the number of blocks before the last accepted block to be accepted for proof state queries. Defaults to `43200`.

### `skip-tx-indexing`

_Boolean_

If set to `true`, the node will not index transactions. TxLookupLimit can be still used to control deleting old transaction indices. Defaults to `false`.

### `inspect-database`

_Boolean_

If set to `true`, inspects the database on startup. Defaults to `false`.

## VM Networking

### `max-outbound-active-requests`

_Integer_

Specifies the maximum number of outbound VM2VM requests in flight at once. Defaults to `16`.

## Warp Configuration

### `warp-off-chain-messages`

_Array of Hex Strings_

Encodes off-chain messages (unrelated to any on-chain event ie. block or AddressedCall) that the node should be willing to sign. Note: only supports AddressedCall payloads. Defaults to empty array.

## Miscellaneous

### `skip-upgrade-check`

_Boolean_

If set to `true`, the chain will skip verifying that all expected network upgrades have taken place before the last accepted block on startup. This allows node operators to recover if their node has accepted blocks after a network upgrade with a version of the code prior to the upgrade. Defaults to `false`.

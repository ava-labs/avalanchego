# Release Notes

## Pending Release

## Updates

- Changed default write option from `Sync` to `NoSync` in PebbleDB

## Fixes

- Fixed database close on shutdown

## [v0.6.11](https://github.com/ava-labs/subnet-evm/releases/tag/v0.6.11)

This release focuses on Standalone DB and database configs.

This version is backwards compatible to [v0.6.0](https://github.com/ava-labs/subnet-evm/releases/tag/v0.6.0). It is optional, but encouraged.

The plugin version is unchanged at 37 and is compatible with AvalancheGo versions v1.11.12.

### Updates

* Added Standalone DB creation in chain data directory (`~/.avalanchego/chainData/{chain-ID}/db/`). Subnet-EVM will create seperate databases for chains by default if there is no accepted blocks previously
* Refactored Warp Backend to support new payload types
* Refactored TrieDB reference root configuration
* Bumped AvalancheGo dependency to v1.11.12
* Bumped minimum Golang version to v1.22.8

### Configs

* Added following new database options:
  * `"use-standalone-database"` (`bool`): If true it enables creation of standalone database. If false it uses the GRPC Database provided by AvalancheGo. Default is nil and creates the standalone database only if there is no accepted block in the AvalancheGo database (node has not accepted any blocks for this chain)
  * `"database-type"` (`string`): Specifies the type of database to use. Must be one of `pebbledb`, `leveldb` or `memdb`. memdb is an in-memory, non-persisted database. Default is `pebbledb`
  * `"database-config-file"` (`string`): Path to the database config file. Config file is changed for every database type. See [docs](https://docs.avax.network/api-reference/avalanche-go-configs-flags#database-config) for available configs per database type. Ignored if --config-file-content is specified
  * `"database-config-file-content"` (`string`): As an alternative to `database-config-file`, it allows specifying base64 encoded database config content
  * `"database-path"` (`string`): Specifies the directory to which the standalone database is persisted. Defaults to "`$HOME/.avalanchego/chainData/{chainID}`"
  * `"database-read-only"` (`bool`) : Specifies if the standalone database should be a read-only type. Defaults to false

### Fixes

* Fixed Eth upgrade mapping with Avalanche upgrades in genesis
* Fixed transaction size tracking in worker environment
* Fixed a rare case of VM's shutting down ends up panicking in RPC server

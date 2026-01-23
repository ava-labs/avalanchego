//! Chain-specific API endpoints.
//!
//! This module provides JSON-RPC endpoints for each Avalanche chain:
//! - X-Chain (AVM): Asset transfers and NFTs
//! - P-Chain (PlatformVM): Staking and subnet management
//! - C-Chain (EVM): Ethereum-compatible operations
//! - Admin/Info: Node information and health

use serde_json::{json, Value};

use crate::jsonrpc::RpcError;
use crate::server::Endpoint;

// ============================================================================
// X-Chain (AVM) API
// ============================================================================

/// Creates the X-Chain (AVM) API endpoint.
pub fn create_avm_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/bc/X");

    // Transaction methods
    endpoint.register(
        "avm.issueTx",
        "Issues a signed transaction to the network",
        |params| async move {
            let tx = params
                .and_then(|p| p.get("tx").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing tx"))?;

            // In production: decode tx, validate, submit to mempool
            Ok(json!({
                "txID": format!("2oGdPdfw2qcNUHeqjjtvNh1sNKdxDBvqU6i6CzT5nKmkWF{}", &tx[..8.min(tx.len())])
            }))
        },
    );

    endpoint.register(
        "avm.getTx",
        "Gets a transaction by ID",
        |params| async move {
            let tx_id = params
                .and_then(|p| p.get("txID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing txID"))?;

            Ok(json!({
                "tx": "0x...", // Base64 encoded tx
                "encoding": "hex",
                "txID": tx_id
            }))
        },
    );

    endpoint.register(
        "avm.getTxStatus",
        "Gets the status of a transaction",
        |params| async move {
            let tx_id = params
                .and_then(|p| p.get("txID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing txID"))?;

            Ok(json!({
                "status": "Accepted",
                "txID": tx_id
            }))
        },
    );

    // UTXO methods
    endpoint.register(
        "avm.getUTXOs",
        "Gets UTXOs for an address",
        |params| async move {
            let addresses = params
                .and_then(|p| p.get("addresses").cloned())
                .ok_or_else(|| RpcError::invalid_params("missing addresses"))?;

            Ok(json!({
                "numFetched": 0,
                "utxos": [],
                "endIndex": {
                    "address": "",
                    "utxo": ""
                },
                "addresses": addresses
            }))
        },
    );

    // Balance methods
    endpoint.register(
        "avm.getBalance",
        "Gets the balance of an address",
        |params| async move {
            let address = params
                .and_then(|p| p.get("address").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!({
                "balance": "0",
                "utxoIDs": [],
                "address": address
            }))
        },
    );

    endpoint.register(
        "avm.getAllBalances",
        "Gets all balances for an address",
        |params| async move {
            let address = params
                .and_then(|p| p.get("address").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!({
                "balances": [],
                "address": address
            }))
        },
    );

    // Asset methods
    endpoint.register(
        "avm.getAssetDescription",
        "Gets description of an asset",
        |params| async move {
            let asset_id = params
                .and_then(|p| p.get("assetID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing assetID"))?;

            if asset_id == "AVAX" {
                Ok(json!({
                    "assetID": "AVAX",
                    "name": "Avalanche",
                    "symbol": "AVAX",
                    "denomination": 9
                }))
            } else {
                Ok(json!({
                    "assetID": asset_id,
                    "name": "Unknown",
                    "symbol": "???",
                    "denomination": 0
                }))
            }
        },
    );

    endpoint.register(
        "avm.createAsset",
        "Creates a new asset",
        |params| async move {
            let name = params
                .as_ref()
                .and_then(|p| p.get("name").and_then(|v| v.as_str()))
                .unwrap_or("New Asset");

            Ok(json!({
                "assetID": format!("2o{}", name.replace(' ', "")),
                "changeAddr": "X-avax1..."
            }))
        },
    );

    // Address methods
    endpoint.register(
        "avm.buildGenesis",
        "Builds genesis state",
        |params| async move {
            Ok(json!({
                "bytes": "0x...",
                "encoding": "hex"
            }))
        },
    );

    endpoint
}

// ============================================================================
// P-Chain (Platform) API
// ============================================================================

/// Creates the P-Chain (Platform) API endpoint.
pub fn create_platform_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/bc/P");

    // Transaction methods
    endpoint.register(
        "platform.issueTx",
        "Issues a signed transaction",
        |params| async move {
            let tx = params
                .and_then(|p| p.get("tx").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing tx"))?;

            Ok(json!({
                "txID": format!("2pGz{}", &tx[..8.min(tx.len())])
            }))
        },
    );

    endpoint.register(
        "platform.getTx",
        "Gets a transaction by ID",
        |params| async move {
            let tx_id = params
                .and_then(|p| p.get("txID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing txID"))?;

            Ok(json!({
                "tx": "0x...",
                "encoding": "hex",
                "txID": tx_id
            }))
        },
    );

    endpoint.register(
        "platform.getTxStatus",
        "Gets transaction status",
        |params| async move {
            let tx_id = params
                .and_then(|p| p.get("txID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing txID"))?;

            Ok(json!({
                "status": "Committed",
                "reason": ""
            }))
        },
    );

    // Staking methods
    endpoint.register(
        "platform.getCurrentValidators",
        "Gets current validator set",
        |params| async move {
            let subnet_id = params
                .and_then(|p| p.get("subnetID").and_then(|v| v.as_str()).map(String::from));

            Ok(json!({
                "validators": [
                    {
                        "txID": "2pDz...",
                        "startTime": "1654000000",
                        "endTime": "1685536000",
                        "stakeAmount": "2000000000000",
                        "nodeID": "NodeID-Aa...",
                        "delegationFee": "20.0000",
                        "connected": true,
                        "uptime": "0.99"
                    }
                ]
            }))
        },
    );

    endpoint.register(
        "platform.getPendingValidators",
        "Gets pending validator set",
        |_params| async move {
            Ok(json!({
                "validators": [],
                "delegators": []
            }))
        },
    );

    endpoint.register(
        "platform.getStake",
        "Gets stake amount for addresses",
        |params| async move {
            let addresses = params
                .and_then(|p| p.get("addresses").cloned())
                .ok_or_else(|| RpcError::invalid_params("missing addresses"))?;

            Ok(json!({
                "staked": "0",
                "stakedOutputs": [],
                "addresses": addresses
            }))
        },
    );

    endpoint.register(
        "platform.getMinStake",
        "Gets minimum stake amounts",
        |_params| async move {
            Ok(json!({
                "minValidatorStake": "2000000000000",
                "minDelegatorStake": "25000000000"
            }))
        },
    );

    endpoint.register(
        "platform.getRewardUTXOs",
        "Gets reward UTXOs for a transaction",
        |params| async move {
            let tx_id = params
                .and_then(|p| p.get("txID").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing txID"))?;

            Ok(json!({
                "numFetched": 0,
                "utxos": [],
                "txID": tx_id
            }))
        },
    );

    // Subnet methods
    endpoint.register(
        "platform.getSubnets",
        "Gets all subnets",
        |_params| async move {
            Ok(json!({
                "subnets": [
                    {
                        "id": "11111111111111111111111111111111LpoYY",
                        "controlKeys": [],
                        "threshold": 0
                    }
                ]
            }))
        },
    );

    endpoint.register(
        "platform.createSubnet",
        "Creates a new subnet",
        |params| async move {
            Ok(json!({
                "txID": "2pSU...",
                "subnetID": "28nrH..."
            }))
        },
    );

    // Blockchain methods
    endpoint.register(
        "platform.getBlockchains",
        "Gets all blockchains",
        |_params| async move {
            Ok(json!({
                "blockchains": [
                    {
                        "id": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM",
                        "name": "X-Chain",
                        "subnetID": "11111111111111111111111111111111LpoYY",
                        "vmID": "jvYyfQTxGMJLuGWa55kdP2p2zSUYsQ5Raupu4TW34ZAUBAbtq"
                    },
                    {
                        "id": "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5",
                        "name": "C-Chain",
                        "subnetID": "11111111111111111111111111111111LpoYY",
                        "vmID": "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6"
                    }
                ]
            }))
        },
    );

    endpoint.register(
        "platform.createBlockchain",
        "Creates a new blockchain",
        |params| async move {
            Ok(json!({
                "txID": "2pBC...",
                "blockchainID": "29kH..."
            }))
        },
    );

    // Balance methods
    endpoint.register(
        "platform.getBalance",
        "Gets balance for an address",
        |params| async move {
            let address = params
                .and_then(|p| p.get("address").and_then(|v| v.as_str()).map(String::from))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!({
                "balance": "0",
                "unlocked": "0",
                "lockedStakeable": "0",
                "lockedNotStakeable": "0",
                "utxoIDs": []
            }))
        },
    );

    endpoint.register(
        "platform.getUTXOs",
        "Gets UTXOs for addresses",
        |params| async move {
            let addresses = params
                .and_then(|p| p.get("addresses").cloned())
                .ok_or_else(|| RpcError::invalid_params("missing addresses"))?;

            Ok(json!({
                "numFetched": 0,
                "utxos": [],
                "endIndex": {
                    "address": "",
                    "utxo": ""
                }
            }))
        },
    );

    // Height/timestamp methods
    endpoint.register(
        "platform.getHeight",
        "Gets current height",
        |_params| async move {
            Ok(json!({
                "height": "1"
            }))
        },
    );

    endpoint.register(
        "platform.getTimestamp",
        "Gets current timestamp",
        |_params| async move {
            let timestamp = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs();

            Ok(json!({
                "timestamp": timestamp.to_string()
            }))
        },
    );

    endpoint
}

// ============================================================================
// C-Chain (EVM) API
// ============================================================================

/// Creates the C-Chain (EVM) API endpoint.
pub fn create_evm_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/bc/C/rpc");

    // Standard Ethereum JSON-RPC methods
    endpoint.register(
        "eth_chainId",
        "Returns the chain ID",
        |_params| async move {
            Ok(json!("0xa86a")) // 43114 in hex
        },
    );

    endpoint.register(
        "eth_blockNumber",
        "Returns the current block number",
        |_params| async move {
            Ok(json!("0x1"))
        },
    );

    endpoint.register(
        "eth_getBalance",
        "Returns the balance of an address",
        |params| async move {
            let _address = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!("0x0"))
        },
    );

    endpoint.register(
        "eth_getTransactionCount",
        "Returns the nonce of an address",
        |params| async move {
            let _address = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!("0x0"))
        },
    );

    endpoint.register(
        "eth_getCode",
        "Returns the code at an address",
        |params| async move {
            let _address = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing address"))?;

            Ok(json!("0x"))
        },
    );

    endpoint.register(
        "eth_getStorageAt",
        "Returns storage at a position",
        |params| async move {
            let arr = params.as_ref().and_then(|p| p.as_array());
            if arr.map_or(true, |a| a.len() < 2) {
                return Err(RpcError::invalid_params("missing address or position"));
            }

            Ok(json!("0x0000000000000000000000000000000000000000000000000000000000000000"))
        },
    );

    endpoint.register(
        "eth_call",
        "Executes a call without creating a transaction",
        |params| async move {
            let _call = params
                .as_ref()
                .and_then(|p| p.get(0))
                .ok_or_else(|| RpcError::invalid_params("missing call object"))?;

            Ok(json!("0x"))
        },
    );

    endpoint.register(
        "eth_estimateGas",
        "Estimates gas for a transaction",
        |params| async move {
            let _tx = params
                .as_ref()
                .and_then(|p| p.get(0))
                .ok_or_else(|| RpcError::invalid_params("missing transaction object"))?;

            Ok(json!("0x5208")) // 21000 gas for simple transfer
        },
    );

    endpoint.register(
        "eth_gasPrice",
        "Returns the current gas price",
        |_params| async move {
            Ok(json!("0x5d21dba00")) // 25 gwei
        },
    );

    endpoint.register(
        "eth_maxPriorityFeePerGas",
        "Returns suggested priority fee",
        |_params| async move {
            Ok(json!("0x77359400")) // 2 gwei
        },
    );

    endpoint.register(
        "eth_feeHistory",
        "Returns fee history",
        |params| async move {
            let block_count = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_u64()))
                .unwrap_or(1);

            let mut base_fees = vec!["0x5d21dba00"; block_count as usize + 1];
            let mut gas_used = vec!["0.5"; block_count as usize];
            let mut rewards: Vec<Vec<&str>> = vec![vec!["0x77359400"]; block_count as usize];

            Ok(json!({
                "oldestBlock": "0x1",
                "baseFeePerGas": base_fees,
                "gasUsedRatio": gas_used,
                "reward": rewards
            }))
        },
    );

    endpoint.register(
        "eth_sendRawTransaction",
        "Sends a signed transaction",
        |params| async move {
            let tx = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing transaction data"))?;

            // Return a dummy tx hash
            Ok(json!(format!("0x{:0>64}", &tx[2..66.min(tx.len())])))
        },
    );

    endpoint.register(
        "eth_getTransactionByHash",
        "Gets a transaction by hash",
        |params| async move {
            let hash = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing hash"))?;

            Ok(json!({
                "hash": hash,
                "nonce": "0x0",
                "blockHash": null,
                "blockNumber": null,
                "transactionIndex": null,
                "from": "0x0000000000000000000000000000000000000000",
                "to": "0x0000000000000000000000000000000000000000",
                "value": "0x0",
                "gasPrice": "0x5d21dba00",
                "gas": "0x5208",
                "input": "0x"
            }))
        },
    );

    endpoint.register(
        "eth_getTransactionReceipt",
        "Gets a transaction receipt",
        |params| async move {
            let hash = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing hash"))?;

            Ok(json!({
                "transactionHash": hash,
                "blockHash": "0x0000000000000000000000000000000000000000000000000000000000000001",
                "blockNumber": "0x1",
                "transactionIndex": "0x0",
                "from": "0x0000000000000000000000000000000000000000",
                "to": "0x0000000000000000000000000000000000000000",
                "cumulativeGasUsed": "0x5208",
                "gasUsed": "0x5208",
                "contractAddress": null,
                "logs": [],
                "status": "0x1",
                "logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
            }))
        },
    );

    endpoint.register(
        "eth_getBlockByNumber",
        "Gets a block by number",
        |params| async move {
            let arr = params.as_ref().and_then(|p| p.as_array());
            let block_number = arr.and_then(|a| a.get(0)).and_then(|v| v.as_str()).unwrap_or("latest");
            let full_txs = arr.and_then(|a| a.get(1)).and_then(|v| v.as_bool()).unwrap_or(false);

            Ok(json!({
                "number": "0x1",
                "hash": "0x0000000000000000000000000000000000000000000000000000000000000001",
                "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
                "nonce": "0x0000000000000000",
                "sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
                "logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
                "transactionsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
                "stateRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
                "receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
                "miner": "0x0000000000000000000000000000000000000000",
                "difficulty": "0x0",
                "totalDifficulty": "0x0",
                "extraData": "0x",
                "size": "0x200",
                "gasLimit": "0x7a1200",
                "gasUsed": "0x0",
                "timestamp": "0x0",
                "transactions": if full_txs { json!([]) } else { json!([]) },
                "uncles": [],
                "baseFeePerGas": "0x5d21dba00"
            }))
        },
    );

    endpoint.register(
        "eth_getBlockByHash",
        "Gets a block by hash",
        |params| async move {
            let hash = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing hash"))?;

            Ok(json!({
                "number": "0x1",
                "hash": hash,
                "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
                "transactions": [],
                "baseFeePerGas": "0x5d21dba00"
            }))
        },
    );

    endpoint.register(
        "eth_getLogs",
        "Gets logs matching filter",
        |params| async move {
            let _filter = params
                .as_ref()
                .and_then(|p| p.get(0))
                .ok_or_else(|| RpcError::invalid_params("missing filter"))?;

            Ok(json!([]))
        },
    );

    endpoint.register(
        "net_version",
        "Returns the network ID",
        |_params| async move {
            Ok(json!("43114"))
        },
    );

    endpoint.register(
        "net_listening",
        "Returns if client is listening",
        |_params| async move {
            Ok(json!(true))
        },
    );

    endpoint.register(
        "net_peerCount",
        "Returns number of peers",
        |_params| async move {
            Ok(json!("0xa"))
        },
    );

    endpoint.register(
        "web3_clientVersion",
        "Returns the client version",
        |_params| async move {
            Ok(json!("avalanche-rs/0.1.0"))
        },
    );

    endpoint.register(
        "web3_sha3",
        "Returns Keccak-256 of data",
        |params| async move {
            let data = params
                .as_ref()
                .and_then(|p| p.get(0).and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing data"))?;

            // Placeholder - would compute actual keccak256
            Ok(json!(format!("0x{:0>64}", "hash")))
        },
    );

    endpoint
}

// ============================================================================
// Admin API
// ============================================================================

/// Creates the Admin API endpoint.
pub fn create_admin_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/admin");

    endpoint.register(
        "admin.alias",
        "Set an alias for an endpoint",
        |params| async move {
            let endpoint = params
                .as_ref()
                .and_then(|p| p.get("endpoint").and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing endpoint"))?;

            let alias = params
                .as_ref()
                .and_then(|p| p.get("alias").and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing alias"))?;

            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint.register(
        "admin.aliasChain",
        "Set an alias for a chain",
        |params| async move {
            let chain = params
                .as_ref()
                .and_then(|p| p.get("chain").and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing chain"))?;

            let alias = params
                .as_ref()
                .and_then(|p| p.get("alias").and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing alias"))?;

            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint.register(
        "admin.getChainAliases",
        "Gets chain aliases",
        |params| async move {
            let chain = params
                .as_ref()
                .and_then(|p| p.get("chain").and_then(|v| v.as_str()))
                .ok_or_else(|| RpcError::invalid_params("missing chain"))?;

            Ok(json!({
                "aliases": [chain]
            }))
        },
    );

    endpoint.register(
        "admin.lockProfile",
        "Lock CPU/memory profiling",
        |_params| async move {
            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint.register(
        "admin.memoryProfile",
        "Run memory profiling",
        |_params| async move {
            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint.register(
        "admin.startCPUProfiler",
        "Start CPU profiling",
        |_params| async move {
            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint.register(
        "admin.stopCPUProfiler",
        "Stop CPU profiling",
        |_params| async move {
            Ok(json!({
                "success": true
            }))
        },
    );

    endpoint
}

/// Creates a server with all standard endpoints registered.
pub fn create_standard_server(address: impl Into<String>) -> crate::Server {
    use crate::server::{create_health_endpoint, create_info_endpoint};

    let server = crate::Server::new(address);

    // Register all endpoints
    server.register_endpoint(create_info_endpoint());
    server.register_endpoint(create_health_endpoint());
    server.register_endpoint(create_avm_endpoint());
    server.register_endpoint(create_platform_endpoint());
    server.register_endpoint(create_evm_endpoint());
    server.register_endpoint(create_admin_endpoint());

    server
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::jsonrpc::Request;

    #[tokio::test]
    async fn test_avm_get_balance() {
        let endpoint = create_avm_endpoint();
        let request = Request::new(
            "avm.getBalance",
            Some(json!({"address": "X-avax1..."})),
            1,
        );

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());
    }

    #[tokio::test]
    async fn test_platform_get_validators() {
        let endpoint = create_platform_endpoint();
        let request = Request::new("platform.getCurrentValidators", None, 1);

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());

        let result = response.result.unwrap();
        assert!(result.get("validators").is_some());
    }

    #[tokio::test]
    async fn test_evm_chain_id() {
        let endpoint = create_evm_endpoint();
        let request = Request::new("eth_chainId", None, 1);

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());
        assert_eq!(response.result.unwrap(), "0xa86a");
    }

    #[tokio::test]
    async fn test_evm_gas_price() {
        let endpoint = create_evm_endpoint();
        let request = Request::new("eth_gasPrice", None, 1);

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());
    }

    #[tokio::test]
    async fn test_evm_get_balance() {
        let endpoint = create_evm_endpoint();
        let request = Request::new(
            "eth_getBalance",
            Some(json!(["0x742d35Cc6634C0532925a3b844Bc9e7595f5c6D2", "latest"])),
            1,
        );

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());
    }

    #[tokio::test]
    async fn test_admin_endpoint() {
        let endpoint = create_admin_endpoint();
        let request = Request::new(
            "admin.alias",
            Some(json!({"endpoint": "ext/bc/X", "alias": "X"})),
            1,
        );

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());
    }

    #[test]
    fn test_standard_server() {
        let server = create_standard_server("127.0.0.1:9650");
        let endpoints = server.list_endpoints();

        assert!(endpoints.contains(&"ext/bc/X".to_string()));
        assert!(endpoints.contains(&"ext/bc/P".to_string()));
        assert!(endpoints.contains(&"ext/bc/C/rpc".to_string()));
        assert!(endpoints.contains(&"ext/info".to_string()));
        assert!(endpoints.contains(&"ext/health".to_string()));
        assert!(endpoints.contains(&"ext/admin".to_string()));
    }
}

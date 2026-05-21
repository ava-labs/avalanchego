---
title: Subnet-EVM API
---

[Subnet-EVM](https://github.com/ava-labs/subnet-evm) APIs are identical to
[Coreth](https://build.avax.network/docs/api-reference/c-chain/api) C-Chain APIs, except Avalanche Specific APIs
starting with `avax`. Subnet-EVM also supports standard Ethereum APIs as well. For more
information about Coreth APIs see [GitHub](https://github.com/ava-labs/coreth).

Subnet-EVM has some additional APIs that are not available in Coreth.

## `eth_feeConfig`

Subnet-EVM comes with an API request for getting fee config at a specific block. You can use this
API to check your activated fee config.

**Signature:**

```bash
eth_feeConfig([blk BlkNrOrHash]) -> {feeConfig: json}
```

- `blk` is the block number or hash at which to retrieve the fee config. Defaults to the latest block if omitted.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "eth_feeConfig",
    "params": [
        "latest"
    ],
    "id": 1
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/2ebCneCbwthjQ1rYT41nhd7M76Hc6YmosMAQrTFhBq8qeqh6tt/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "feeConfig": {
      "gasLimit": 15000000,
      "targetBlockRate": 2,
      "minBaseFee": 33000000000,
      "targetGas": 15000000,
      "baseFeeChangeDenominator": 36,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 1000000,
      "blockGasCostStep": 200000
    },
    "lastChangedAt": 0
  }
}
```

## `eth_getChainConfig`

`eth_getChainConfig` returns the Chain Config of the blockchain. This API is enabled by default with
`internal-blockchain` namespace.

This API exists on the C-Chain as well, but in addition to the normal Chain Config returned by the
C-Chain `eth_getChainConfig` on `subnet-evm` additionally returns the upgrade config, which specifies
network upgrades activated after the genesis. **Signature:**

```bash
eth_getChainConfig({}) -> {chainConfig: json}
```

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id"     :1,
    "method" :"eth_getChainConfig",
    "params" :[]
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/Nvqcm33CX2XABS62iZsAcVUkavfnzp1Sc5k413wn5Nrf7Qjt7/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "chainId": 43214,
    "feeConfig": {
      "gasLimit": 8000000,
      "targetBlockRate": 2,
      "minBaseFee": 33000000000,
      "targetGas": 15000000,
      "baseFeeChangeDenominator": 36,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 1000000,
      "blockGasCostStep": 200000
    },
    "allowFeeRecipients": true,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "subnetEVMTimestamp": 0,
    "contractDeployerAllowListConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    },
    "contractNativeMinterConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    },
    "feeManagerConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    },
    "upgrades": {
      "precompileUpgrades": [
        {
          "feeManagerConfig": {
            "adminAddresses": null,
            "blockTimestamp": 1661541259,
            "disable": true
          }
        },
        {
          "feeManagerConfig": {
            "adminAddresses": null,
            "blockTimestamp": 1661541269
          }
        }
      ]
    }
  }
}
```

## `eth_getActivePrecompilesAt`

**DEPRECATED—instead use** [`eth_getActiveRulesAt`](#eth_getactiveprecompilesat).

`eth_getActivePrecompilesAt` returns activated precompiles at a specific timestamp. If no
timestamp is provided it returns the latest block timestamp. This API is enabled by default with
`internal-blockchain` namespace.

**Signature:**

```bash
eth_getActivePrecompilesAt([timestamp uint]) -> {precompiles: []Precompile}
```

- `timestamp` specifies the timestamp to show the precompiles active at this time. If omitted it shows precompiles activated at the latest block timestamp.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "eth_getActivePrecompilesAt",
    "params": [],
    "id": 1
}'  -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/Nvqcm33CX2XABS62iZsAcVUkavfnzp1Sc5k413wn5Nrf7Qjt7/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "contractDeployerAllowListConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    },
    "contractNativeMinterConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    },
    "feeManagerConfig": {
      "adminAddresses": ["0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc"],
      "blockTimestamp": 0
    }
  }
}
```

## `eth_getActiveRulesAt`

`eth_getActiveRulesAt` returns activated rules (precompiles, upgrades) at a specific timestamp. If no
timestamp is provided it returns the latest block timestamp. This API is enabled by default with
`internal-blockchain` namespace.

**Signature:**

```bash
eth_getActiveRulesAt([timestamp uint]) -> {rules: json}
```

- `timestamp` specifies the timestamp to show the rules active at this time. If omitted it shows rules activated at the latest block timestamp.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "eth_getActiveRulesAt",
    "params": [],
    "id": 1
}'  -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/Nvqcm33CX2XABS62iZsAcVUkavfnzp1Sc5k413wn5Nrf7Qjt7/rpc
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ethRules": {
      "IsHomestead": true,
      "IsEIP150": true,
      "IsEIP155": true,
      "IsEIP158": true,
      "IsByzantium": true,
      "IsConstantinople": true,
      "IsPetersburg": true,
      "IsIstanbul": true,
      "IsCancun": true
    },
    "avalancheRules": {
      "IsSubnetEVM": true,
      "IsDurango": true,
      "IsEtna": true
    },
    "precompiles": {
      "contractNativeMinterConfig": {
        "timestamp": 0
      },
      "rewardManagerConfig": {
        "timestamp": 1712918700
      },
      "warpConfig": {
        "timestamp": 1714158045
      }
    }
  }
}
```

## `validators.getCurrentValidators`

This API retrieves the list of current validators for the Subnet/L1. It provides detailed information about each validator, including their ID, status, weight, connection, and uptime.

URL: `http://<server-uri>/ext/bc/<blockchainID>/validators`

**Signature:**

```bash
validators.getCurrentValidators({nodeIDs: []string}) -> {validators: []Validator}
```

- `nodeIDs` is an optional parameter that specifies the node IDs of the validators to retrieve. If omitted, all validators are returned.

**Example Call:**

```bash
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "validators.getCurrentValidators",
    "params": {
        "nodeIDs": []
    },
    "id": 1
}'  -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/C49rHzk3vLr1w9Z8sY7scrZ69TU4WcD2pRS6ZyzaSn9xA2U9F/validators
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "result": {
    "validators": [
      {
        "validationID": "nESqWkcNXihfdZESS2idWbFETMzatmkoTCktjxG1qryaQXfS6",
        "nodeID": "NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
        "weight": 20,
        "startTimestamp": 1732025492,
        "isActive": true,
        "isL1Validator": false,
        "isConnected": true,
        "uptimeSeconds": 36,
        "uptimePercentage": 100
      }
    ]
  },
  "id": 1
}
```

**Response Fields:**

- `validationID`: (string) Unique identifier for the validation. This returns validation ID for L1s, AddSubnetValidator txID for Subnets.
- `nodeID`: (string) Node identifier for the validator.
- `weight`: (integer) The weight of the validator, often representing stake.
- `startTimestamp`: (integer) UNIX timestamp for when validation started.
- `isActive`: (boolean) Indicates if the validator is active. This returns true if this is L1 validator and has enough continuous subnet staking fees in P-Chain. It always returns true for subnet validators.
- `isL1Validator`: (boolean) Indicates if the validator is a L1 validator or a subnet validator.
- `isConnected`: (boolean) Indicates if the validator node is currently connected to the callee node.
- `uptimeSeconds`: (integer) The number of seconds the validator has been online.
- `uptimePercentage`: (float) The percentage of time the validator has been online.

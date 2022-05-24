# Subnet EVM

[Avalanche](https://docs.avax.network/learn/platform-overview) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.

Subnet EVM is the [Virtual Machine (VM)](https://docs.avax.network/overview/getting-started/avalanche-platform/#virtual-machines) that defines the Subnet Contract Chains. Subnet EVM is a simplified version of [Coreth VM (C-Chain)](https://github.com/ava-labs/coreth).

This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

The Subnet EVM runs in a separate process from the main AvalancheGo process and communicates with it over a local gRPC connection.

### AvalancheGo Compatibility

```
[v0.1.0] AvalancheGo@v1.7.0-v1.7.4
[v0.1.1-v0.1.2] AvalancheGo@v1.7.5-v1.7.6
[v0.2.0] AvalancheGo@v1.7.7-v1.7.9
[v0.2.1] AvalancheGo@v1.7.10
```

## API

The Subnet EVM supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default.
Full documentation for the C-Chain's API can be found [here.](https://docs.avax.network/apis/avalanchego/apis/c-chain)

## Compatibility

The Subnet EVM is compatible with almost all Ethereum tooling, including [Remix,](https://docs.avax.network/dapps/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask/) [Metamask](https://docs.avax.network/dapps/smart-contracts/deploy-a-smart-contract-on-avalanche-using-remix-and-metamask/) and [Truffle.](https://docs.avax.network/dapps/smart-contracts/using-truffle-with-the-avalanche-c-chain/)

## Differences Between Subnet EVM and Coreth

- Added configurable fees and gas limits in genesis
- Merged Avalanche hardforks into the single "Subnet EVM" hardfork
- Removed Atomic Txs and Shared Memory
- Removed Multicoin Contract and State

## Block Format

To support these changes, there have been a number of changes to the SubnetEVM block format compared to what exists on the C-Chain and Ethereum. Here we list the changes to the block format as compared to Ethereum.

### Block Header

* `BaseFee`: Added by EIP-1559 to represent the base fee of the block (present in Ethereum as of EIP-1559)
* `BlockGasCost`: surcharge for producing a block faster than the target rate


## Run Local Network

See [Create a Local EVM Subnet](https://docs.avax.network/subnets/create-a-local-subnet).

## Fuji Subnet Deployment

See [Fuji Testnet Deployment](https://docs.avax.network/subnets/fuji-deployment).


## Setting the Genesis Allocation

When creating an instance of the subnet-evm, you will need to define the genesis
state of the new chain. Part of this includes defining the genesis allocation
(setting the starting balances for whatever addresses you want). If you don't
provide any genesis allocation, you won't be able to interact with your new
chain (all transactions require a fee to be paid from the sender's balance).

To specify a genesis allocation, populate the `alloc` field in the genesis JSON as follows:

```json
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    },
    "Ab5801a7D398351b8bE11C439e05C5B3259aeC9B": {
      "balance": "0xa796504b1cb5a7c0000"
    }
  },
```

The keys in the allocation are [hex](https://en.wikipedia.org/wiki/Hexadecimal) addresses **without the canonical `0x` prefix**.
The balances are denominated in Wei ([10^18 Wei = 1 Whole Unit of Native Token](https://eth-converter.com/)) and expressed as
hex strings **with the canonical `0x` prefix**. You can use [this converter](https://www.rapidtables.com/convert/number/hex-to-decimal.html)
to translate between decimal and hex numbers.

The above example yields the following genesis allocations (denominated in whole units of the native token ie. 1 AVAX/1 WAGMI):

```
0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC: 100000000 (0x52B7D2DCC80CD2E4000000=100000000000000000000000000 Wei)
0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B: 49463 (0xa796504b1cb5a7c0000=49463000000000000000000 Wei)
```

A fully populated genesis JSON with the above allocation would look like (note the `alloc` field):

```json
{
  "config": {
    "chainId": 99999,
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
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 48,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    }
  },
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    },
    "Ab5801a7D398351b8bE11C439e05C5B3259aeC9B": {
      "balance": "0xa796504b1cb5a7c0000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```

## Setting a Custom Fee Recipient

By default, all fees are burned (sent to the blackhole address). However, it is
possible to enable block producers to set a fee recipient (get compensated for
blocks they produce).

To enable this feature, you'll need to add the following to your
genesis file (under the `"config"` key):

```json
{
  "config": {
    "allowFeeRecipients": true
  }
}
```

Next, you'll need to update your [chain config](https://docs.avax.network/nodes/maintain/chain-config-flags#subnet-configs) with the following:

```json
{
  "feeRecipient": "<YOUR 0x-ADDRESS>"
}
```

_Note: If you enable this feature but a validator doesn't specify
a "feeRecipient", the fees will be burned in blocks they produce._

## Priority Regossip

A transaction is "regossiped" when the node does not find the transaction in
a block after `priority-regossip-frequency` (defaults to `1m`). By default, up to 16 transactions
(max 1 per address) are regossiped to validators per minute.

Operators can use "priority regossip" to more aggressively "regossip" transactions for a set of
important addresses (like bridge relayers). To do so, you'll need to update your
[chain config](https://docs.avax.network/nodes/maintain/chain-config-flags#subnet-configs) with the following:

```json
{
  "priority-regossip-addresses": ["<YOUR 0x-ADDRESS>"]
}
```

By default, up to 32 transactions from priority addresses (max 16 per address) are regossipped to validators per second.
You can override these defaults with the following config:

```json
{
  "priority-regossip-frequency": "1s",
  "priority-regossip-max-txs": 32,
  "priority-regossip-addresses": ["<YOUR 0x-ADDRESS>"],
  "priority-regossip-txs-per-address": 16
}
```

## Precompiles

Subnet EVM can provide custom functionalities with precompiled contracts. These precompiled contracts can be activated through `ChainConfig` (in genesis or as an upgrade).

### Restricting Smart Contract Deployers

If you'd like to restrict who has the ability to deploy contracts on your
subnet, you can provide an `AllowList` configuration in your genesis file:

```json
{
  "config": {
    "chainId": 99999,
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
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 48,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    },
    "contractDeployerAllowListConfig": {
      "blockTimestamp": 0,
      "adminAddresses": ["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"]
    }
  },
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```

In this example, `0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC` is named as the
`Admin` of the `ContractDeployerAllowList`. This enables them to add other `Admins` or to add
`Deployers`. Both `Admins` and `Deployers` can deploy contracts. To provide
a great UX with factory contracts, the `tx.Origin` is checked for being a valid
deployer instead of the caller of `CREATE`. This means that factory contracts will still be
able to create new contracts as long as the sender of the original transaction is an allow
listed deployer.

The `Stateful Precompile` powering the `ContractDeployerAllowList` adheres to the following Solidity interface at `0x0200000000000000000000000000000000000000` (you can load this interface and interact directly in Remix):

```solidity
// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

interface AllowListInterface {
    // Set [addr] to have the admin role over the allow list
    function setAdmin(address addr) external;

    // Set [addr] to be enabled on the allow list
    function setEnabled(address addr) external;

    // Set [addr] to have no role over the allow list
    function setNone(address addr) external;

    // Read the status of [addr]
    function readAllowList(address addr) external view returns (uint256);
}
```

If you attempt to add a `Deployer` and you are not an `Admin`, you will see
something like:
![admin fail](./imgs/admin_fail.png)

If you attempt to deploy a contract but you are not an `Admin` not
a `Deployer`, you will see something like:
![deploy fail](./imgs/deploy_fail.png)

The allow list has three roles: `None`, `Deployer`, and `Admin`.

If you call `readAllowList(addr)` then you can read the current role of `addr`, which will return a uint256 with a value of 0, 1, or 2, corresponding to the roles `None`, `Deployer`, and `Admin` respectively.

WARNING: if you remove all of the admins from the allow list, it will no longer be possible to update the allow list without modifying the subnet-evm to schedule a network upgrade.

### Restricting Who Can Submit Transactions
Similar to restricting contract deployers, this precompile restricts which addresses may submit transactions on chain. Like the previous section, you can activate the precompile by including an `AllowList` configuration in your genesis file:
```json
{
  "config": {
    "chainId": 99999,
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
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 48,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    },
    "txAllowListConfig": {
      "blockTimestamp": 0,
      "adminAddresses":["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"]
    }
  },
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```

In this example, `0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC` is named as the
`Admin` of the `ContractDeployerAllowList`. This enables them to add other `Admins` or to add
`Allowed`. Both `Admins` and `Allowed` can submit transactions to the chain.

The `Stateful Precompile` powering the `TxAllowList` adheres to the following Solidity interface at `0x0200000000000000000000000000000000000002` (you can load this interface and interact directly in Remix):
```solidity
// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

interface AllowListInterface {
    // Set [addr] to have the admin role over the allow list
    function setAdmin(address addr) external;

    // Set [addr] to be enabled on the allow list
    function setEnabled(address addr) external;

    // Set [addr] to have no role over the allow list
    function setNone(address addr) external;

    // Read the status of [addr]
    function readAllowList(address addr) external view returns (uint256);
}
```

If you attempt to add an `Allowed` and you are not an `Admin`, you will see
something like:
![admin fail](./imgs/admin_fail.png)

If you attempt to submit a transaction but you are not an `Admin` or not
`Allowed`, you will see something like: `cannot issue transaction from non-allow listed address`

The allow list has three roles: `None`, `Allowed`, and `Admin`.

If you call `readAllowList(addr)` then you can read the current role of `addr`, which will return a `uint256` with a value of 0, 1, or 2, corresponding to the roles `None`, `Allowed`, and `Admin` respectively.

WARNING: if you remove all of the admins from the allow list, it will no longer be possible to update the allow list without modifying the subnet-evm to schedule a network upgrade.

### Minting Native Coins

You can mint native(gas) coins with a precompiled contract. In order to activate this feature, you can provide `nativeMinterConfig` in genesis:

```json
{
  "config": {
    "chainId": 99999,
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
    "feeConfig": {
      "gasLimit": 20000000,
      "minBaseFee": 1000000000,
      "targetGas": 100000000,
      "baseFeeChangeDenominator": 48,
      "minBlockGasCost": 0,
      "maxBlockGasCost": 10000000,
      "targetBlockRate": 2,
      "blockGasCostStep": 500000
    },
    "contractNativeMinterConfig": {
      "blockTimestamp": 0,
      "adminAddresses": ["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"]
    }
  },
  "alloc": {
    "8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
      "balance": "0x52B7D2DCC80CD2E4000000"
    }
  },
  "nonce": "0x0",
  "timestamp": "0x0",
  "extraData": "0x00",
  "gasLimit": "0x1312D00",
  "difficulty": "0x0",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "coinbase": "0x0000000000000000000000000000000000000000",
  "number": "0x0",
  "gasUsed": "0x0",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
```

`adminAddresses` denotes admin accounts who can add other `Admin` or `Minter` accounts. `Minters` and `Admins` are both eligible to mint native coins for other addresses. `ContractNativeMinter` uses same methods as in `ContractDeployerAllowList`.

The `Stateful Precompile` powering the `ContractNativeMinter` adheres to the following Solidity interface at `0x0200000000000000000000000000000000000001` (you can load this interface and interact directly in Remix):

```solidity
// (c) 2022-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: MIT

pragma solidity >=0.8.0;

interface NativeMinterInterface {
    // Set [addr] to have the admin role over the minter list
    function setAdmin(address addr) external;

    // Set [addr] to be enabled on the minter list
    function setEnabled(address addr) external;

    // Set [addr] to have no role over the minter list
    function setNone(address addr) external;

    // Read the status of [addr]
    function readAllowList(address addr) external view returns (uint256);

    // Mint [amount] number of native coins and send to [addr]
    function mintNativeCoin(address addr, uint256 amount) external;
}
```

_Note: Both `ContractDeployerAllowList` and `ContractNativeMinter` can be used together._

### Examples

Subnet-EVM contains example contracts for precompiles under `/contract-examples`. It's a hardhat project with tests, tasks. For more information see [contract examples README](./contract-examples/README.md).

## Join the WAGMI Subnet Demo

<p align="center">
  <img width="40%" alt="WAGMI" src="./imgs/wagmi.png">
</p>

_Thanks to the @0xNeonMonsters for the logo!_

The WAGMI ("We're All Going to Make It") Subnet Demo is a high throughput
testbed for EVM (Ethereum Virtual Machine) optimizations. It is parameterized
to run at a factor more capacity than Fuji/Mainnet C-Chain and will be used
to experiment with release candidates before they make it into an
official [`coreth`](https://github.com/ava-labs/coreth) release.

We created a basic [WAGMI explorer](https://trywagmi.xyz) that surfaces
aggregated usage statistics about the subnet. If you'd like to see any other
stats added to this site, please send a DM to [@\_patrickogrady on Twitter](https://twitter.com/_patrickogrady).

Everyone that has used the the C-Chain more than twice (~970k addresses) has
been airdropped 10 WGM tokens. With the current fee parameterization, this
should be enough for hundreds of txs.

This is one of the first cases of using Avalanche Subnets as a proving ground
for changes in a production VM (coreth). Many underestimate how useful the isolation
of subnets is for performing complex VM testing on a live network (without impacting
the stability of the primary network).

### Network Creation

To create WAGMI, all we had to do was run the following command:

```bash
subnet-cli wizard \
--node-ids=NodeID-9TCq8np31pHjjhGaHtLjs6ptYYPEt3LGb,NodeID-BrYXghQSu6KKGjuzhs3nrkcB46Wc2yYHy,NodeID-89UCR1CsPzzEHuknxhJHKxuFPNCyPz7Bu,NodeID-Hfm8gpD4DpCz4KTzt2osJPfFvu7az3qiD,NodeID-LkdxkfYhg6nSw1EEUxDUSYPXPwmr2cUet \
--vm-genesis-path=networks/11111/genesis.json \
--vm-id=srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy \
--chain-name=wagmi
```

This added these NodeIDs as validators on Fuji, created the WAGMI Subnet, added
all validators to the WAGMI subnet, and created the WAGMI chain.

- SubnetID: [28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY](https://testnet.avascan.info/blockchains?subnet=28nrH5T2BMvNrWecFcV3mfccjs6axM1TVyqe79MCv2Mhs8kxiY)
- ChainID: [2ebCneCbwthjQ1rYT41nhd7M76Hc6YmosMAQrTFhBq8qeqh6tt](https://testnet.avascan.info/blockchain/2ebCneCbwthjQ1rYT41nhd7M76Hc6YmosMAQrTFhBq8qeqh6tt)

### Network Parameters

```
Network ID: 11111
Chain ID: 11111
Block Gas Limit: 20,000,000 (2.5x C-Chain)
10s Gas Target: 100,000,000 (~6.67x C-Chain)
Min Fee: 1 GWei (4% of C-Chain)
Target Block Rate: 2s (Same as C-Chain)
```

### Adding to MetaMask

```
Network Name: WAGMI
RPC URL: https://subnets.avax.network/wagmi/wagmi-chain-testnet/rpc
Chain ID: 11111
Symbol: WGM
Explorer: https://subnets.avax.network/wagmi/wagmi-chain-testnet/explorer
```

![metamask_WAGMI](./imgs/metamask_WAGMI.png)

### Wrapped WAGMI

#### Info

```
Address: 0x3Ee7094DADda15810F191DD6AcF7E4FFa37571e4
IPFS: /ipfs/QmVAuheeidjD2ktdX3sSHMQqSfcjtmca1g9jr7w9GQf7pU
```

#### Metadata

```json
{
  "compiler": { "version": "0.5.17+commit.d19bba13" },
  "language": "Solidity",
  "output": {
    "abi": [
      {
        "anonymous": false,
        "inputs": [
          {
            "indexed": true,
            "internalType": "address",
            "name": "src",
            "type": "address"
          },
          {
            "indexed": true,
            "internalType": "address",
            "name": "guy",
            "type": "address"
          },
          {
            "indexed": false,
            "internalType": "uint256",
            "name": "wad",
            "type": "uint256"
          }
        ],
        "name": "Approval",
        "type": "event"
      },
      {
        "anonymous": false,
        "inputs": [
          {
            "indexed": true,
            "internalType": "address",
            "name": "dst",
            "type": "address"
          },
          {
            "indexed": false,
            "internalType": "uint256",
            "name": "wad",
            "type": "uint256"
          }
        ],
        "name": "Deposit",
        "type": "event"
      },
      {
        "anonymous": false,
        "inputs": [
          {
            "indexed": true,
            "internalType": "address",
            "name": "src",
            "type": "address"
          },
          {
            "indexed": true,
            "internalType": "address",
            "name": "dst",
            "type": "address"
          },
          {
            "indexed": false,
            "internalType": "uint256",
            "name": "wad",
            "type": "uint256"
          }
        ],
        "name": "Transfer",
        "type": "event"
      },
      {
        "anonymous": false,
        "inputs": [
          {
            "indexed": true,
            "internalType": "address",
            "name": "src",
            "type": "address"
          },
          {
            "indexed": false,
            "internalType": "uint256",
            "name": "wad",
            "type": "uint256"
          }
        ],
        "name": "Withdrawal",
        "type": "event"
      },
      { "payable": true, "stateMutability": "payable", "type": "fallback" },
      {
        "constant": true,
        "inputs": [
          { "internalType": "address", "name": "", "type": "address" },
          { "internalType": "address", "name": "", "type": "address" }
        ],
        "name": "allowance",
        "outputs": [
          { "internalType": "uint256", "name": "", "type": "uint256" }
        ],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": false,
        "inputs": [
          { "internalType": "address", "name": "guy", "type": "address" },
          { "internalType": "uint256", "name": "wad", "type": "uint256" }
        ],
        "name": "approve",
        "outputs": [{ "internalType": "bool", "name": "", "type": "bool" }],
        "payable": false,
        "stateMutability": "nonpayable",
        "type": "function"
      },
      {
        "constant": true,
        "inputs": [
          { "internalType": "address", "name": "", "type": "address" }
        ],
        "name": "balanceOf",
        "outputs": [
          { "internalType": "uint256", "name": "", "type": "uint256" }
        ],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": true,
        "inputs": [],
        "name": "decimals",
        "outputs": [{ "internalType": "uint8", "name": "", "type": "uint8" }],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": false,
        "inputs": [],
        "name": "deposit",
        "outputs": [],
        "payable": true,
        "stateMutability": "payable",
        "type": "function"
      },
      {
        "constant": true,
        "inputs": [],
        "name": "name",
        "outputs": [{ "internalType": "string", "name": "", "type": "string" }],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": true,
        "inputs": [],
        "name": "symbol",
        "outputs": [{ "internalType": "string", "name": "", "type": "string" }],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": true,
        "inputs": [],
        "name": "totalSupply",
        "outputs": [
          { "internalType": "uint256", "name": "", "type": "uint256" }
        ],
        "payable": false,
        "stateMutability": "view",
        "type": "function"
      },
      {
        "constant": false,
        "inputs": [
          { "internalType": "address", "name": "dst", "type": "address" },
          { "internalType": "uint256", "name": "wad", "type": "uint256" }
        ],
        "name": "transfer",
        "outputs": [{ "internalType": "bool", "name": "", "type": "bool" }],
        "payable": false,
        "stateMutability": "nonpayable",
        "type": "function"
      },
      {
        "constant": false,
        "inputs": [
          { "internalType": "address", "name": "src", "type": "address" },
          { "internalType": "address", "name": "dst", "type": "address" },
          { "internalType": "uint256", "name": "wad", "type": "uint256" }
        ],
        "name": "transferFrom",
        "outputs": [{ "internalType": "bool", "name": "", "type": "bool" }],
        "payable": false,
        "stateMutability": "nonpayable",
        "type": "function"
      },
      {
        "constant": false,
        "inputs": [
          { "internalType": "uint256", "name": "wad", "type": "uint256" }
        ],
        "name": "withdraw",
        "outputs": [],
        "payable": false,
        "stateMutability": "nonpayable",
        "type": "function"
      }
    ],
    "devdoc": { "methods": {} },
    "userdoc": { "methods": {} }
  },
  "settings": {
    "compilationTarget": { "contracts/wwagmi.sol": "WWAGMI" },
    "evmVersion": "istanbul",
    "libraries": {},
    "optimizer": { "enabled": false, "runs": 200 },
    "remappings": []
  },
  "sources": {
    "contracts/wwagmi.sol": {
      "keccak256": "0x0a6ce5559225d3c99db4a5e24777049df3c84886ba9a08147f23afae4261b509",
      "urls": [
        "bzz-raw://0aef254c65ae30b578256a7e2496ed18bf0cb68e97f5831050e17a2cf0192a7e",
        "dweb:/ipfs/QmSwAbdnaYvrjDHTKnE3qBZ3smT7uipSSfSGBUiKWmNWEY"
      ]
    }
  },
  "version": 1
}
```

#### Code

```solidity
// Copyright (C) 2015, 2016, 2017 Dapphub

// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Contract name, token name, and token symbol modified by Ava Labs 2020

pragma solidity >=0.4.22 <0.6;

contract WWAGMI{
    string public name     = "Wrapped WAGMI";
    string public symbol   = "WWAGMI";
    uint8  public decimals = 18;

    event  Approval(address indexed src, address indexed guy, uint wad);
    event  Transfer(address indexed src, address indexed dst, uint wad);
    event  Deposit(address indexed dst, uint wad);
    event  Withdrawal(address indexed src, uint wad);

    mapping (address => uint)                       public  balanceOf;
    mapping (address => mapping (address => uint))  public  allowance;

    function() external payable {
        deposit();
    }
    function deposit() public payable {
        balanceOf[msg.sender] += msg.value;
        emit Deposit(msg.sender, msg.value);
    }
    function withdraw(uint wad) public {
        require(balanceOf[msg.sender] >= wad);
        balanceOf[msg.sender] -= wad;
        msg.sender.transfer(wad);
        emit Withdrawal(msg.sender, wad);
    }

    function totalSupply() public view returns (uint) {
        return address(this).balance;
    }

    function approve(address guy, uint wad) public returns (bool) {
        allowance[msg.sender][guy] = wad;
        emit Approval(msg.sender, guy, wad);
        return true;
    }

    function transfer(address dst, uint wad) public returns (bool) {
        return transferFrom(msg.sender, dst, wad);
    }

    function transferFrom(address src, address dst, uint wad)
        public
        returns (bool)
    {
        require(balanceOf[src] >= wad);

        if (src != msg.sender && allowance[src][msg.sender] != uint(-1)) {
            require(allowance[src][msg.sender] >= wad);
            allowance[src][msg.sender] -= wad;
        }

        balanceOf[src] -= wad;
        balanceOf[dst] += wad;

        emit Transfer(src, dst, wad);

        return true;
    }
}
```

---
tags: [Configs]
description: Reference for all available configuration options and parameters for the PlatformVM.
pagination_label: P-Chain Configs
sidebar_position: 1
---

# P-Chain

This document provides details about the configuration options available for the PlatformVM.

In order to specify a configuration for the PlatformVM, you need to define a `Config` struct and its parameters. The default values for these parameters are:

```json
{
  "Chains": null,
  "Validators": null,
  "UptimeLockedCalculator": null,
  "SybilProtectionEnabled": false,
  "PartialSyncPrimaryNetwork": false,
  "TrackedSubnets": [],
  "TxFee": 0,
  "CreateAssetTxFee": 0,
  "CreateSubnetTxFee": 0,
  "TransformSubnetTxFee": 0,
  "CreateBlockchainTxFee": 0,
  "AddPrimaryNetworkValidatorFee": 0,
  "AddPrimaryNetworkDelegatorFee": 0,
  "AddSubnetValidatorFee": 0,
  "AddSubnetDelegatorFee": 0,
  "MinValidatorStake": 0,
  "MaxValidatorStake": 0,
  "MinDelegatorStake": 0,
  "MinDelegationFee": 0,
  "UptimePercentage": 0,
  "MinStakeDuration": "0s",
  "MaxStakeDuration": "0s",
  "RewardConfig": {},
  "ApricotPhase3Time": "0001-01-01T00:00:00Z",
  "ApricotPhase5Time": "0001-01-01T00:00:00Z",
  "BanffTime": "0001-01-01T00:00:00Z",
  "CortinaTime": "0001-01-01T00:00:00Z",
  "DurangoTime": "0001-01-01T00:00:00Z",
  "EUpgradeTime": "0001-01-01T00:00:00Z",
  "UseCurrentHeight": false
}
```

Default values are overridden only if explicitly specified in the config.

## Parameters

The parameters are as follows:

### `Chains`

The node's chain manager

### `Validators`

Node's validator set maps SubnetID to validators of the Subnet

- The primary network's validator set should have been added to the manager before calling VM.Initialize.
- The primary network's validator set should be empty before calling VM.Initialize.

### `UptimeLockedCalculator`

Provides access to the uptime manager as a thread-safe data structure

### `SybilProtectionEnabled`

_Boolean_

True if the node is being run with staking enabled

### `PartialSyncPrimaryNetwork`

_Boolean_

If true, only the P-chain will be instantiated on the primary network.

### `TrackedSubnets`

Set of Subnets that this node is validating

### `TxFee`

_Uint64_

Fee that is burned by every non-state creating transaction

### `CreateAssetTxFee`

_Uint64_

Fee that must be burned by every state creating transaction before AP3

### `CreateSubnetTxFee`

_Uint64_

Fee that must be burned by every Subnet creating transaction after AP3

### `TransformSubnetTxFee`

_Uint64_

Fee that must be burned by every transform Subnet transaction

### `CreateBlockchainTxFee`

_Uint64_

Fee that must be burned by every blockchain creating transaction after AP3

### `AddPrimaryNetworkValidatorFee`

_Uint64_

Transaction fee for adding a primary network validator

### `AddPrimaryNetworkDelegatorFee`

_Uint64_

Transaction fee for adding a primary network delegator

### `AddSubnetValidatorFee`

_Uint64_

Transaction fee for adding a Subnet validator

### `AddSubnetDelegatorFee`

_Uint64_

Transaction fee for adding a Subnet delegator

### `MinValidatorStake`

_Uint64_

The minimum amount of tokens one must bond to be a validator

### `MaxValidatorStake`

_Uint64_

The maximum amount of tokens that can be bonded on a validator

### `MinDelegatorStake`

_Uint64_

Minimum stake, in nAVAX, that can be delegated on the primary network

### `MinDelegationFee`

_Uint32_

Minimum fee that can be charged for delegation

### `UptimePercentage`

_Float64_

UptimePercentage is the minimum uptime required to be rewarded for staking

### `MinStakeDuration`

_Duration_

Minimum amount of time to allow a staker to stake

### `MaxStakeDuration`

_Duration_

Maximum amount of time to allow a staker to stake

### `RewardConfig`

Config for the minting function

### `ApricotPhase3Time`

_Time_

Time of the AP3 network upgrade

### `ApricotPhase5Time`

_Time_

Time of the AP5 network upgrade

### `BanffTime`

_Time_

Time of the Banff network upgrade

### `CortinaTime`

_Time_

Time of the Cortina network upgrade

### `DurangoTime`

_Time_

Time of the Durango network upgrade

### `EUpgradeTime`

_Time_

Time of the E network upgrade

### `UseCurrentHeight`

_Boolean_

UseCurrentHeight forces `GetMinimumHeight` to return the current height of the P-Chain instead of the oldest block in the `recentlyAccepted` window. This config is particularly useful for triggering proposervm activation on recently created Subnets (without this, users need to wait for `recentlyAcceptedWindowTTL` to pass for activation to occur).

// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Module to facilitate the registration of precompiles and their configuration.
package registry

// Force imports of each precompile to ensure each precompile's init function runs and registers itself
// with the registry.
import (
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
)

// This list is kept just for reference. The actual addresses defined in respective packages of precompiles.
// Note: it is important that none of these addresses conflict with each other or any other precompiles
// in core/vm/contracts.go.
// The first stateful precompiles were added in coreth to support nativeAssetCall and nativeAssetBalance. New stateful precompiles
// originating in coreth will continue at this prefix, so we reserve this range in subnet-evm so that they can be migrated into
// subnet-evm without issue.
// These start at the address: 0x0100000000000000000000000000000000000000 and will increment by 1.
// Optional precompiles implemented in subnet-evm start at 0x0200000000000000000000000000000000000000 and will increment by 1
// from here to reduce the risk of conflicts.
// For forks of subnet-evm, users should start at 0x0300000000000000000000000000000000000000 to ensure
// that their own modifications do not conflict with stateful precompiles that may be added to subnet-evm
// in the future.
// ContractDeployerAllowListAddress = common.HexToAddress("0x0200000000000000000000000000000000000000")
// ContractNativeMinterAddress      = common.HexToAddress("0x0200000000000000000000000000000000000001")
// TxAllowListAddress               = common.HexToAddress("0x0200000000000000000000000000000000000002")
// FeeManagerAddress                = common.HexToAddress("0x0200000000000000000000000000000000000003")
// RewardManagerAddress             = common.HexToAddress("0x0200000000000000000000000000000000000004")
// WarpAddress                      = common.HexToAddress("0x0200000000000000000000000000000000000005")
// ADD YOUR PRECOMPILE HERE
// {YourPrecompile}Address          = common.HexToAddress("0x03000000000000000000000000000000000000??")

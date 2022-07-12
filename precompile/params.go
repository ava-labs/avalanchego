// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import "github.com/ethereum/go-ethereum/common"

// Gas costs for stateful precompiles
const (
	writeGasCostPerSlot = 20_000
	readGasCostPerSlot  = 5_000

	ModifyAllowListGasCost = writeGasCostPerSlot
	ReadAllowListGasCost   = readGasCostPerSlot

	MintGasCost = 30_000

	SetFeeConfigGasCost     = writeGasCostPerSlot * (numFeeConfigField + 1) // plus one for setting last changed at
	GetFeeConfigGasCost     = readGasCostPerSlot * numFeeConfigField
	GetLastChangedAtGasCost = readGasCostPerSlot
)

// Designated addresses of stateful precompiles
// Note: it is important that none of these addresses conflict with each other or any other precompiles
// in core/vm/contracts.go.
// We start at 0x0200000000000000000000000000000000000000 and will increment by 1 from here to reduce
// the risk of conflicts.
// For forks of subnet-evm, users should start at 0x0300000000000000000000000000000000000000 to ensure
// that their own modifications do not conflict with stateful precompiles that may be added to subnet-evm
// in the future.
var (
	ContractDeployerAllowListAddress = common.HexToAddress("0x0200000000000000000000000000000000000000")
	ContractNativeMinterAddress      = common.HexToAddress("0x0200000000000000000000000000000000000001")
	TxAllowListAddress               = common.HexToAddress("0x0200000000000000000000000000000000000002")
	FeeConfigManagerAddress          = common.HexToAddress("0x0200000000000000000000000000000000000003")

	UsedAddresses = []common.Address{
		ContractDeployerAllowListAddress,
		ContractNativeMinterAddress,
		TxAllowListAddress,
		FeeConfigManagerAddress,
	}
)

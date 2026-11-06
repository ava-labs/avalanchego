// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeasset

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV3 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts

var (
	// GenesisContractAddr is the address of the deprecated native-asset
	// genesis contract. It is the same account as the blackhole address that
	// receives burned fees.
	GenesisContractAddr    = common.HexToAddress("0x0100000000000000000000000000000000000000")
	NativeAssetBalanceAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	NativeAssetCallAddr    = common.HexToAddress("0x0100000000000000000000000000000000000002")
)

const (
	// AssetBalanceCost is the gas cost of a native asset balance lookup.
	// Based on the cost of an SLOAD operation since native asset balances are
	// kept in state storage.
	AssetBalanceCost uint64 = 2100
	// AssetCallCost is the gas cost of a native asset call. This reflects
	// the additional work done for the native asset transfer itself, which is
	// a write to state storage. The cost of creating a new account and normal
	// value transfer is assessed separately from this cost.
	AssetCallCost uint64 = 20000
)

var _ contract.StatefulPrecompiledContract = (*NativeAssetBalance)(nil)

// NativeAssetBalance is a precompiled contract used to retrieve the native asset balance
type NativeAssetBalance struct{}

// PackNativeAssetBalanceInput packs the arguments into the required input data for a transaction to be passed into
// the native asset balance contract.
func PackNativeAssetBalanceInput(address common.Address, assetID common.Hash) []byte {
	input := make([]byte, 52)
	copy(input, address.Bytes())
	copy(input[20:], assetID.Bytes())
	return input
}

// UnpackNativeAssetBalanceInput attempts to unpack [input] into the arguments to the native asset balance precompile
func UnpackNativeAssetBalanceInput(input []byte) (common.Address, common.Hash, error) {
	if len(input) != 52 {
		return common.Address{}, common.Hash{}, fmt.Errorf("native asset balance input had unexpcted length %d", len(input))
	}
	address := common.BytesToAddress(input[:20])
	assetID := common.Hash{}
	assetID.SetBytes(input[20:52])
	return address, assetID, nil
}

// Run returns the balance of an asset held by an address, as a 32-byte word.
// The input is the address followed by the asset ID, as packed by
// [PackNativeAssetBalanceInput]. Malformed input reverts.
func (*NativeAssetBalance) Run(env contract.Environment, input []byte) ([]byte, error) {
	if err := contract.DeductGas(env, AssetBalanceCost); err != nil {
		return nil, err
	}

	// input: encodePacked(address 20 bytes, assetID 32 bytes)
	address, assetID, err := UnpackNativeAssetBalanceInput(input)
	if err != nil {
		return nil, vm.ErrExecutionReverted
	}

	stateDB := env.ReadOnlyState().(contract.StateDB)
	res := stateDB.GetAssetBalance(address, assetID).Bytes32()
	return res[:], nil
}

var _ contract.StatefulPrecompiledContract = (*NativeAssetCall)(nil)

// NativeAssetCall atomically transfers a native asset to a recipient address as well as calling that
// address
type NativeAssetCall struct{}

// PackNativeAssetCallInput packs the arguments into the required input data for a transaction to be passed into
// the native asset contract.
// Assumes that [assetAmount] is non-nil.
func PackNativeAssetCallInput(address common.Address, assetID common.Hash, assetAmount *uint256.Int, callData []byte) []byte {
	input := make([]byte, 84+len(callData))
	copy(input[0:20], address.Bytes())
	copy(input[20:52], assetID.Bytes())
	assetAmount.WriteToSlice(input[52:84])
	copy(input[84:], callData)
	return input
}

// UnpackNativeAssetCallInput attempts to unpack [input] into the arguments to the native asset call precompile
func UnpackNativeAssetCallInput(input []byte) (common.Address, common.Hash, *uint256.Int, []byte, error) {
	if len(input) < 84 {
		return common.Address{}, common.Hash{}, nil, nil, fmt.Errorf("native asset call input had unexpected length %d", len(input))
	}
	to := common.BytesToAddress(input[:20])
	assetID := common.BytesToHash(input[20:52])
	assetAmount := new(uint256.Int).SetBytes32(input[52:84])
	callData := input[84:]
	return to, assetID, assetAmount, callData, nil
}

// Run transfers an amount of an asset from the caller to a recipient, then
// calls the recipient with the given call data and all remaining gas. The input
// is packed by [PackNativeAssetCallInput]. The transfer is undone if the call
// fails. If the call fails other than by reverting, all remaining gas is
// consumed. Read-only calls and malformed input revert.
func (*NativeAssetCall) Run(env contract.Environment, input []byte) ([]byte, error) {
	if err := contract.DeductGas(env, AssetCallCost); err != nil {
		return nil, err
	}

	if env.ReadOnly() {
		return nil, vm.ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := UnpackNativeAssetCallInput(input)
	if err != nil {
		log.Debug("unpacking native asset call input failed", "err", err)
		return nil, vm.ErrExecutionReverted
	}

	stateDB := env.StateDB().(contract.StateDB)
	if !assetAmount.IsZero() && stateDB.GetAssetBalance(env.Caller(), assetID).Cmp(assetAmount) < 0 {
		return nil, vm.ErrInsufficientBalance
	}

	snapshot := stateDB.Snapshot()

	if !stateDB.Exist(to) {
		if !env.UseGas(params.CallNewAccountGas) {
			return nil, vm.ErrOutOfGas
		}
		stateDB.CreateAccount(to)
	}

	// Send `assetAmount` of `assetID` to `to` address
	stateDB.SubAssetBalance(env.Caller(), assetID, assetAmount)
	stateDB.AddAssetBalance(to, assetID, assetAmount)

	legacy := []vm.CallOption{
		vm.WithUNSAFECallerAddressProxying(),
		vm.WithLegacyOutboundCallGas(),
	}
	ret, err := env.Call(to, callData, env.Gas(), new(uint256.Int), legacy...)
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		stateDB.RevertToSnapshot(snapshot)
		if err != vm.ErrExecutionReverted {
			env.UseGas(env.Gas())
		}
	}
	return ret, err
}

var _ contract.StatefulPrecompiledContract = (*DeprecatedContract)(nil)

type DeprecatedContract struct{}

// Run always reverts without charging gas.
func (*DeprecatedContract) Run(contract.Environment, []byte) ([]byte, error) {
	return nil, vm.ErrExecutionReverted
}

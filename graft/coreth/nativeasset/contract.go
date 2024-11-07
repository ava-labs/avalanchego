// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeasset

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/precompile/contract"
	"github.com/ava-labs/coreth/vmerrs"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV3 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts

var (
	GenesisContractAddr    = common.HexToAddress("0x0100000000000000000000000000000000000000")
	NativeAssetBalanceAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	NativeAssetCallAddr    = common.HexToAddress("0x0100000000000000000000000000000000000002")
)

// NativeAssetBalance is a precompiled contract used to retrieve the native asset balance
type NativeAssetBalance struct {
	GasCost uint64
}

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

// Run implements StatefulPrecompiledContract
func (b *NativeAssetBalance) Run(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes)
	if suppliedGas < b.GasCost {
		return nil, 0, vmerrs.ErrOutOfGas
	}
	remainingGas = suppliedGas - b.GasCost

	address, assetID, err := UnpackNativeAssetBalanceInput(input)
	if err != nil {
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}

	res, overflow := uint256.FromBig(accessibleState.GetStateDB().GetBalanceMultiCoin(address, assetID))
	if overflow {
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}
	return common.LeftPadBytes(res.Bytes(), 32), remainingGas, nil
}

// NativeAssetCall atomically transfers a native asset to a recipient address as well as calling that
// address
type NativeAssetCall struct {
	GasCost           uint64
	CallNewAccountGas uint64
}

// PackNativeAssetCallInput packs the arguments into the required input data for a transaction to be passed into
// the native asset contract.
// Assumes that [assetAmount] is non-nil.
func PackNativeAssetCallInput(address common.Address, assetID common.Hash, assetAmount *big.Int, callData []byte) []byte {
	input := make([]byte, 84+len(callData))
	copy(input[0:20], address.Bytes())
	copy(input[20:52], assetID.Bytes())
	assetAmount.FillBytes(input[52:84])
	copy(input[84:], callData)
	return input
}

// UnpackNativeAssetCallInput attempts to unpack [input] into the arguments to the native asset call precompile
func UnpackNativeAssetCallInput(input []byte) (common.Address, common.Hash, *big.Int, []byte, error) {
	if len(input) < 84 {
		return common.Address{}, common.Hash{}, nil, nil, fmt.Errorf("native asset call input had unexpected length %d", len(input))
	}
	to := common.BytesToAddress(input[:20])
	assetID := common.BytesToHash(input[20:52])
	assetAmount := new(big.Int).SetBytes(input[52:84])
	callData := input[84:]
	return to, assetID, assetAmount, callData, nil
}

// Run implements StatefulPrecompiledContract
func (c *NativeAssetCall) Run(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if suppliedGas < c.GasCost {
		return nil, 0, vmerrs.ErrOutOfGas
	}
	remainingGas = suppliedGas - c.GasCost

	if readOnly {
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := UnpackNativeAssetCallInput(input)
	if err != nil {
		log.Debug("unpacking native asset call input failed", "err", err)
		return nil, remainingGas, vmerrs.ErrExecutionReverted
	}

	stateDB := accessibleState.GetStateDB()
	// Note: it is not possible for a negative assetAmount to be passed in here due to the fact that decoding a
	// byte slice into a *big.Int type will always return a positive value, as documented on [big.Int.SetBytes].
	if assetAmount.Sign() != 0 && stateDB.GetBalanceMultiCoin(caller, assetID).Cmp(assetAmount) < 0 {
		return nil, remainingGas, vmerrs.ErrInsufficientBalance
	}

	snapshot := stateDB.Snapshot()

	if !stateDB.Exist(to) {
		if remainingGas < c.CallNewAccountGas {
			return nil, 0, vmerrs.ErrOutOfGas
		}
		remainingGas -= c.CallNewAccountGas
		stateDB.CreateAccount(to)
	}

	// Send [assetAmount] of [assetID] to [to] address
	stateDB.SubBalanceMultiCoin(caller, assetID, assetAmount)
	stateDB.AddBalanceMultiCoin(to, assetID, assetAmount)

	ret, remainingGas, err = accessibleState.Call(to, callData, remainingGas, new(uint256.Int), vm.WithUNSAFECallerAddressProxying())

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		stateDB.RevertToSnapshot(snapshot)
		if err != vmerrs.ErrExecutionReverted {
			remainingGas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

type DeprecatedContract struct{}

func (*DeprecatedContract) Run(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return nil, suppliedGas, vmerrs.ErrExecutionReverted
}

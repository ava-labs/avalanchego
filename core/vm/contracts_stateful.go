// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// PrecompiledContractsApricot contains the default set of pre-compiled Ethereum
// contracts used in the Istanbul release and the stateful precompiled contracts
// added for the Avalanche Apricot release.
// Apricot is incompatible with the YoloV3 Release since it does not include the
// BLS12-381 Curve Operations added to the set of precompiled contracts

var (
	genesisContractAddr    = common.HexToAddress("0x0100000000000000000000000000000000000000")
	nativeAssetBalanceAddr = common.HexToAddress("0x0100000000000000000000000000000000000001")
	nativeAssetCallAddr    = common.HexToAddress("0x0100000000000000000000000000000000000002")
)

// StatefulPrecompiledContract is the interface for executing a precompiled contract
// This wraps the PrecompiledContracts native to Ethereum and allows adding in stateful
// precompiled contracts to support native Avalanche asset transfers.
type StatefulPrecompiledContract interface {
	// Run executes a precompiled contract in the current state
	// assumes that it has already been verified that [caller] can
	// transfer [value].
	Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error)
}

// wrappedPrecompiledContract implements StatefulPrecompiledContract by wrapping stateless native precompiled contracts
// in Ethereum.
type wrappedPrecompiledContract struct {
	p PrecompiledContract
}

func newWrappedPrecompiledContract(p PrecompiledContract) StatefulPrecompiledContract {
	return &wrappedPrecompiledContract{p: p}
}

// Run implements the StatefulPrecompiledContract interface
func (w *wrappedPrecompiledContract) Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return RunPrecompiledContract(w.p, input, suppliedGas)
}

// nativeAssetBalance is a precompiled contract used to retrieve the native asset balance
type nativeAssetBalance struct {
	gasCost uint64
}

func PackNativeAssetBalanceInput(address common.Address, assetID common.Hash) []byte {
	input := make([]byte, 52)
	copy(input, address.Bytes())
	copy(input[20:], assetID.Bytes())
	return input
}

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
func (b *nativeAssetBalance) Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes)
	if suppliedGas < b.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - b.gasCost

	address, assetID, err := UnpackNativeAssetBalanceInput(input)
	if err != nil {
		return nil, remainingGas, ErrExecutionReverted
	}

	res, overflow := uint256.FromBig(evm.StateDB.GetBalanceMultiCoin(address, assetID))
	if overflow {
		return nil, remainingGas, ErrExecutionReverted
	}
	return common.LeftPadBytes(res.Bytes(), 32), remainingGas, nil
}

// nativeAssetCall atomically transfers a native asset to a recipient address as well as calling that
// address
type nativeAssetCall struct {
	gasCost uint64
}

func PackNativeAssetCallInput(address common.Address, assetID common.Hash, assetAmount *big.Int, callData []byte) []byte {
	input := make([]byte, 84+len(callData))
	copy(input[0:20], address.Bytes())
	copy(input[20:52], assetID.Bytes())
	assetAmount.FillBytes(input[52:84])
	copy(input[84:], callData)
	return input
}

func UnpackNativeAssetCallInput(input []byte) (common.Address, *common.Hash, *big.Int, []byte, error) {
	if len(input) < 84 {
		return common.Address{}, nil, nil, nil, fmt.Errorf("native asset call input had unexpcted length %d", len(input))
	}
	to := common.BytesToAddress(input[:20])
	assetID := new(common.Hash)
	assetID.SetBytes(input[20:52])
	assetAmount := new(big.Int).SetBytes(input[52:84])
	callData := input[84:]
	return to, assetID, assetAmount, callData, nil
}

// Run implements StatefulPrecompiledContract
func (c *nativeAssetCall) Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// input: encodePacked(address 20 bytes, assetID 32 bytes, assetAmount 32 bytes, callData variable length bytes)
	if suppliedGas < c.gasCost {
		return nil, 0, ErrOutOfGas
	}
	remainingGas = suppliedGas - c.gasCost

	if readOnly {
		return nil, remainingGas, ErrExecutionReverted
	}

	to, assetID, assetAmount, callData, err := UnpackNativeAssetCallInput(input)
	if err != nil {
		return nil, remainingGas, ErrExecutionReverted
	}

	if assetAmount.Sign() != 0 && !evm.Context.CanTransferMC(evm.StateDB, caller.Address(), to, assetID, assetAmount) {
		return nil, remainingGas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()

	if !evm.StateDB.Exist(to) {
		if remainingGas < params.CallNewAccountGas {
			return nil, 0, ErrOutOfGas
		}
		remainingGas -= params.CallNewAccountGas
		evm.StateDB.CreateAccount(to)
	}

	// Increment the call depth which is restricted to 1024
	evm.depth++
	defer func() { evm.depth-- }()

	// Send [assetAmount] of [assetID] to [to] address
	evm.Context.TransferMultiCoin(evm.StateDB, caller.Address(), to, assetID, assetAmount)
	ret, remainingGas, err = evm.Call(caller, to, callData, remainingGas, big.NewInt(0))

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			remainingGas = 0
		}
		// TODO: consider clearing up unused snapshots:
		//} else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, remainingGas, err
}

type deprecatedContract struct{}

func (_ *deprecatedContract) Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return nil, suppliedGas, ErrExecutionReverted
}

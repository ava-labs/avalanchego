// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	_ "embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
)

const (
	mintInputLen = common.HashLength + common.HashLength

	MintGasCost = 30_000
)

type MintNativeCoinInput struct {
	Addr   common.Address
	Amount *big.Int
}

var (
	// Singleton StatefulPrecompiledContract for minting native assets by permissioned callers.
	ContractNativeMinterPrecompile contract.StatefulPrecompiledContract = createNativeMinterPrecompile()

	ErrCannotMint = errors.New("non-enabled cannot mint")
	ErrInvalidLen = errors.New("invalid input length for minting")

	// NativeMinterRawABI contains the raw ABI of NativeMinter contract.
	//go:embed contract.abi
	NativeMinterRawABI string

	NativeMinterABI = contract.ParseABI(NativeMinterRawABI)
)

// GetContractNativeMinterStatus returns the role of [address] for the minter list.
func GetContractNativeMinterStatus(stateDB contract.StateDB, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetContractNativeMinterStatus sets the permissions of [address] to [role] for the
// minter list. assumes [role] has already been verified as valid.
func SetContractNativeMinterStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}

// PackMintNativeCoin packs [address] and [amount] into the appropriate arguments for mintNativeCoin.
func PackMintNativeCoin(address common.Address, amount *big.Int) ([]byte, error) {
	return NativeMinterABI.Pack("mintNativeCoin", address, amount)
}

// UnpackMintNativeCoinInput attempts to unpack [input] as address and amount.
// assumes that [input] does not include selector (omits first 4 func signature bytes)
// if [skipLenCheck] is false, it will return an error if the length of [input] is not [mintInputLen]
func UnpackMintNativeCoinInput(input []byte, skipLenCheck bool) (common.Address, *big.Int, error) {
	// Initially we had this check to ensure that the input was the correct length.
	// However solidity does not always pack the input to the correct length, and allows
	// for extra padding bytes to be added to the end of the input. Therefore, we have removed
	// this check with the DUpgrade. We still need to keep this check for backwards compatibility.
	if !skipLenCheck && len(input) != mintInputLen {
		return common.Address{}, nil, fmt.Errorf("%w: %d", ErrInvalidLen, len(input))
	}
	inputStruct := MintNativeCoinInput{}
	err := NativeMinterABI.UnpackInputIntoInterface(&inputStruct, "mintNativeCoin", input)

	return inputStruct.Addr, inputStruct.Amount, err
}

// mintNativeCoin checks if the caller is permissioned for minting operation.
// The execution function parses the [input] into native coin amount and receiver address.
func mintNativeCoin(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, MintGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}

	// We skip the fixed length check with DUpgrade
	to, amount, err := UnpackMintNativeCoinInput(input, contract.IsDUpgradeActivated(accessibleState))
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := allowlist.GetAllowListStatus(stateDB, ContractAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotMint, caller)
	}

	// if there is no address in the state, create one.
	if !stateDB.Exist(to) {
		stateDB.CreateAccount(to)
	}

	stateDB.AddBalance(to, amount)
	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createNativeMinterPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
// Access to the getters/setters is controlled by an allow list for ContractAddress.
func createNativeMinterPrecompile() contract.StatefulPrecompiledContract {
	var functions []*contract.StatefulPrecompileFunction
	functions = append(functions, allowlist.CreateAllowListFunctions(ContractAddress)...)

	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"mintNativeCoin": mintNativeCoin,
	}

	for name, function := range abiFunctionMap {
		method, ok := NativeMinterABI.Methods[name]
		if !ok {
			panic(fmt.Errorf("given method (%s) does not exist in the ABI", name))
		}
		functions = append(functions, contract.NewStatefulPrecompileFunction(method.ID, function))
	}
	// Construct the contract with no fallback function.
	statefulContract, err := contract.NewStatefulPrecompileContract(nil, functions)
	if err != nil {
		panic(err)
	}
	return statefulContract
}

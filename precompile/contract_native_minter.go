// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var (
	_ StatefulPrecompileConfig = &ContractNativeMinterConfig{}
	// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
	ContractNativeMinterPrecompile StatefulPrecompiledContract = createNativeMinterPrecompile(ContractNativeMinterAddress)

	mintSignature = CalculateFunctionSelector("mint(address,uint256)") // address, amount

	ErrCannotMint = errors.New("non-enabled cannot mint")

	inputLen = common.HashLength + common.HashLength
)

// ContractNativeMinterConfig wraps [AllowListConfig] and uses it to implement the StatefulPrecompileConfig
// interface while adding in the contract deployer specific precompile address.
type ContractNativeMinterConfig struct {
	AllowListConfig
}

// Address returns the address of the contract deployer allow list.
func (c *ContractNativeMinterConfig) Address() common.Address {
	return ContractNativeMinterAddress
}

// Configure configures [state] with the desired admins based on [c].
func (c *ContractNativeMinterConfig) Configure(state StateDB) {
	c.AllowListConfig.Configure(state, ContractNativeMinterAddress)
}

// Contract returns the singleton stateful precompiled contract to be used for the allow list.
func (c *ContractNativeMinterConfig) Contract() StatefulPrecompiledContract {
	return ContractNativeMinterPrecompile
}

// GetContractNativeMinterStatus returns the role of [address] for the minter list.
func GetContractNativeMinterStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, ContractNativeMinterAddress, address)
}

// SetContractNativeMinterStatus sets the permissions of [address] to [role] for the
// contract deployer allow list.
// assumes [role] has already been verified as valid.
func SetContractNativeMinterStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, ContractNativeMinterAddress, address, role)
}

// PackMintInput packs [address] and [amount] into the appropriate arguments for minting operation.
func PackMintInput(address common.Address, amount *big.Int) ([]byte, error) {
	// function selector (4 bytes) + input(hash for address + hash for amount)
	input := make([]byte, 0, selectorLen+inputLen)
	input = append(input, mintSignature...)
	input = append(input, address.Hash().Bytes()...)
	input = append(input, amount.Bytes()...)
	return input, nil
}

// UnpackMintInput attempts to unpack [input] into the arguments to the mint precompile
func UnpackMintInput(input []byte) (common.Address, *big.Int, error) {
	if len(input) != inputLen {
		return common.Address{}, nil, fmt.Errorf("invalid input length for minting: %d", len(input))
	}
	to := common.BytesToAddress(input[:common.AddressLength])
	assetAmount := new(big.Int).SetBytes(input[common.AddressLength : common.AddressLength+common.HashLength])
	return to, assetAmount, nil
}

// createMint // TODO: add comment
func createMint(precompileAddr common.Address) RunStatefulPrecompileFunc {
	return func(evm PrecompileAccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		// Note: this should never happen since the required gas should be verified before calling Run.
		if suppliedGas < MintGasCost {
			return nil, 0, fmt.Errorf("%w (%d) < (%d)", ErrExceedsGasAllowance, MintGasCost, suppliedGas)
		}
		remainingGas = suppliedGas - MintGasCost

		if readOnly {
			return nil, remainingGas, ErrWriteProtection
		}

		to, amount, err := UnpackMintInput(input)
		if err != nil {
			return nil, remainingGas, err
		}

		// Verify that the caller is in the allow list and therefore has the right to modify it
		callerStatus := getAllowListStatus(evm.GetStateDB(), precompileAddr, callerAddr)
		if !callerStatus.IsEnabled() {
			return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotMint, callerAddr)
		}

		if !evm.GetStateDB().Exist(to) {
			if remainingGas < CallNewAccountGas {
				return nil, 0, fmt.Errorf("%w (%d) < (%d)", ErrExceedsGasAllowance, CallNewAccountGas, suppliedGas)
			}
			remainingGas -= CallNewAccountGas
			evm.GetStateDB().CreateAccount(to)
		}

		evm.GetStateDB().AddBalance(to, amount)
		// Return an empty output and the remaining gas
		return []byte{}, remainingGas, nil
	}
}

// createNativeMinterPrecompile returns a StatefulPrecompiledContract with R/W control of an allow list at [precompileAddr]
func createNativeMinterPrecompile(precompileAddr common.Address) StatefulPrecompiledContract {
	setAdmin := newStatefulPrecompileFunction(setAdminSignature, createAllowListSetter(precompileAddr, AllowListAdmin), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setEnabled := newStatefulPrecompileFunction(setEnabledSignature, createAllowListSetter(precompileAddr, AllowListEnabled), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setNone := newStatefulPrecompileFunction(setNoneSignature, createAllowListSetter(precompileAddr, AllowListNoRole), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	read := newStatefulPrecompileFunction(readAllowListSignature, createReadAllowList(precompileAddr), createConstantRequiredGasFunc(ReadAllowListGasCost))

	mint := newStatefulPrecompileFunction(mintSignature, createMint(precompileAddr), createConstantRequiredGasFunc(MintGasCost))

	// Construct the contract with no fallback function.
	contract := newStatefulPrecompileWithFunctionSelectors(nil, []*statefulPrecompileFunction{setAdmin, setEnabled, setNone, read, mint})
	return contract
}

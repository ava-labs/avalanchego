// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
)

const (
	minFeeConfigFieldKey = iota + 1
	// add new fields below this
	// must preserve order of these fields
	gasLimitKey = iota
	targetBlockRateKey
	minBaseFeeKey
	targetGasKey
	baseFeeChangeDenominatorKey
	minBlockGasCostKey
	maxBlockGasCostKey
	blockGasCostStepKey
	// add new fields above this
	numFeeConfigField = iota - 1

	// [numFeeConfigField] fields in FeeConfig struct
	feeConfigInputLen = common.HashLength * numFeeConfigField

	SetFeeConfigGasCost     = contract.WriteGasCostPerSlot * (numFeeConfigField + 1) // plus one for setting last changed at
	GetFeeConfigGasCost     = contract.ReadGasCostPerSlot * numFeeConfigField
	GetLastChangedAtGasCost = contract.ReadGasCostPerSlot
)

var (

	// Singleton StatefulPrecompiledContract for setting fee configs by permissioned callers.
	FeeManagerPrecompile contract.StatefulPrecompiledContract = createFeeManagerPrecompile()

	setFeeConfigSignature              = contract.CalculateFunctionSelector("setFeeConfig(uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256)")
	getFeeConfigSignature              = contract.CalculateFunctionSelector("getFeeConfig()")
	getFeeConfigLastChangedAtSignature = contract.CalculateFunctionSelector("getFeeConfigLastChangedAt()")

	feeConfigLastChangedAtKey = common.Hash{'l', 'c', 'a'}

	ErrCannotChangeFee = errors.New("non-enabled cannot change fee config")
)

// GetFeeManagerStatus returns the role of [address] for the fee config manager list.
func GetFeeManagerStatus(stateDB contract.StateDB, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetFeeManagerStatus sets the permissions of [address] to [role] for the
// fee config manager list. assumes [role] has already been verified as valid.
func SetFeeManagerStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}

// PackGetFeeConfigInput packs the getFeeConfig signature
func PackGetFeeConfigInput() []byte {
	return getFeeConfigSignature
}

// PackGetLastChangedAtInput packs the getFeeConfigLastChangedAt signature
func PackGetLastChangedAtInput() []byte {
	return getFeeConfigLastChangedAtSignature
}

// PackFeeConfig packs [feeConfig] without the selector into the appropriate arguments for fee config operations.
func PackFeeConfig(feeConfig commontype.FeeConfig) ([]byte, error) {
	//  input(feeConfig)
	return packFeeConfigHelper(feeConfig, false)
}

// PackSetFeeConfig packs [feeConfig] with the selector into the appropriate arguments for setting fee config operations.
func PackSetFeeConfig(feeConfig commontype.FeeConfig) ([]byte, error) {
	// function selector (4 bytes) + input(feeConfig)
	return packFeeConfigHelper(feeConfig, true)
}

func packFeeConfigHelper(feeConfig commontype.FeeConfig, useSelector bool) ([]byte, error) {
	hashes := []common.Hash{
		common.BigToHash(feeConfig.GasLimit),
		common.BigToHash(new(big.Int).SetUint64(feeConfig.TargetBlockRate)),
		common.BigToHash(feeConfig.MinBaseFee),
		common.BigToHash(feeConfig.TargetGas),
		common.BigToHash(feeConfig.BaseFeeChangeDenominator),
		common.BigToHash(feeConfig.MinBlockGasCost),
		common.BigToHash(feeConfig.MaxBlockGasCost),
		common.BigToHash(feeConfig.BlockGasCostStep),
	}

	if useSelector {
		res := make([]byte, len(setFeeConfigSignature)+feeConfigInputLen)
		err := contract.PackOrderedHashesWithSelector(res, setFeeConfigSignature, hashes)
		return res, err
	}

	res := make([]byte, len(hashes)*common.HashLength)
	err := contract.PackOrderedHashes(res, hashes)
	return res, err
}

// UnpackFeeConfigInput attempts to unpack [input] into the arguments to the fee config precompile
// assumes that [input] does not include selector (omits first 4 bytes in PackSetFeeConfigInput)
func UnpackFeeConfigInput(input []byte) (commontype.FeeConfig, error) {
	if len(input) != feeConfigInputLen {
		return commontype.FeeConfig{}, fmt.Errorf("invalid input length for fee config Input: %d", len(input))
	}
	feeConfig := commontype.FeeConfig{}
	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		listIndex := i - 1
		packedElement := contract.PackedHash(input, listIndex)
		switch i {
		case gasLimitKey:
			feeConfig.GasLimit = new(big.Int).SetBytes(packedElement)
		case targetBlockRateKey:
			feeConfig.TargetBlockRate = new(big.Int).SetBytes(packedElement).Uint64()
		case minBaseFeeKey:
			feeConfig.MinBaseFee = new(big.Int).SetBytes(packedElement)
		case targetGasKey:
			feeConfig.TargetGas = new(big.Int).SetBytes(packedElement)
		case baseFeeChangeDenominatorKey:
			feeConfig.BaseFeeChangeDenominator = new(big.Int).SetBytes(packedElement)
		case minBlockGasCostKey:
			feeConfig.MinBlockGasCost = new(big.Int).SetBytes(packedElement)
		case maxBlockGasCostKey:
			feeConfig.MaxBlockGasCost = new(big.Int).SetBytes(packedElement)
		case blockGasCostStepKey:
			feeConfig.BlockGasCostStep = new(big.Int).SetBytes(packedElement)
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
	}
	return feeConfig, nil
}

// GetStoredFeeConfig returns fee config from contract storage in given state
func GetStoredFeeConfig(stateDB contract.StateDB) commontype.FeeConfig {
	feeConfig := commontype.FeeConfig{}
	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		val := stateDB.GetState(ContractAddress, common.Hash{byte(i)})
		switch i {
		case gasLimitKey:
			feeConfig.GasLimit = new(big.Int).Set(val.Big())
		case targetBlockRateKey:
			feeConfig.TargetBlockRate = val.Big().Uint64()
		case minBaseFeeKey:
			feeConfig.MinBaseFee = new(big.Int).Set(val.Big())
		case targetGasKey:
			feeConfig.TargetGas = new(big.Int).Set(val.Big())
		case baseFeeChangeDenominatorKey:
			feeConfig.BaseFeeChangeDenominator = new(big.Int).Set(val.Big())
		case minBlockGasCostKey:
			feeConfig.MinBlockGasCost = new(big.Int).Set(val.Big())
		case maxBlockGasCostKey:
			feeConfig.MaxBlockGasCost = new(big.Int).Set(val.Big())
		case blockGasCostStepKey:
			feeConfig.BlockGasCostStep = new(big.Int).Set(val.Big())
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
	}
	return feeConfig
}

func GetFeeConfigLastChangedAt(stateDB contract.StateDB) *big.Int {
	val := stateDB.GetState(ContractAddress, feeConfigLastChangedAtKey)
	return val.Big()
}

// StoreFeeConfig stores given [feeConfig] and block number in the [blockContext] to the [stateDB].
// A validation on [feeConfig] is done before storing.
func StoreFeeConfig(stateDB contract.StateDB, feeConfig commontype.FeeConfig, blockContext contract.BlockContext) error {
	if err := feeConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify fee config: %w", err)
	}

	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		var input common.Hash
		switch i {
		case gasLimitKey:
			input = common.BigToHash(feeConfig.GasLimit)
		case targetBlockRateKey:
			input = common.BigToHash(new(big.Int).SetUint64(feeConfig.TargetBlockRate))
		case minBaseFeeKey:
			input = common.BigToHash(feeConfig.MinBaseFee)
		case targetGasKey:
			input = common.BigToHash(feeConfig.TargetGas)
		case baseFeeChangeDenominatorKey:
			input = common.BigToHash(feeConfig.BaseFeeChangeDenominator)
		case minBlockGasCostKey:
			input = common.BigToHash(feeConfig.MinBlockGasCost)
		case maxBlockGasCostKey:
			input = common.BigToHash(feeConfig.MaxBlockGasCost)
		case blockGasCostStepKey:
			input = common.BigToHash(feeConfig.BlockGasCostStep)
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
		stateDB.SetState(ContractAddress, common.Hash{byte(i)}, input)
	}

	blockNumber := blockContext.Number()
	if blockNumber == nil {
		return fmt.Errorf("blockNumber cannot be nil")
	}
	stateDB.SetState(ContractAddress, feeConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}

// setFeeConfig checks if the caller has permissions to set the fee config.
// The execution function parses [input] into FeeConfig structure and sets contract storage accordingly.
func setFeeConfig(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}

	feeConfig, err := UnpackFeeConfigInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := GetFeeManagerStatus(stateDB, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotChangeFee, caller)
	}

	if err := StoreFeeConfig(stateDB, feeConfig, accessibleState.GetBlockContext()); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// getFeeConfig returns the stored fee config as an output.
// The execution function reads the contract state for the stored fee config and returns the output.
func getFeeConfig(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	feeConfig := GetStoredFeeConfig(accessibleState.GetStateDB())

	output, err := PackFeeConfig(feeConfig)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the fee config as output and the remaining gas
	return output, remainingGas, err
}

// getFeeConfigLastChangedAt returns the block number that fee config was last changed in.
// The execution function reads the contract state for the stored block number and returns the output.
func getFeeConfigLastChangedAt(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetFeeConfigLastChangedAt(accessibleState.GetStateDB())

	// Return an empty output and the remaining gas
	return common.BigToHash(lastChangedAt).Bytes(), remainingGas, err
}

// createFeeManagerPrecompile returns a StatefulPrecompiledContract
// with getters and setters for the chain's fee config. Access to the getters/setters
// is controlled by an allow list for ContractAddress.
func createFeeManagerPrecompile() contract.StatefulPrecompiledContract {
	feeManagerFunctions := allowlist.CreateAllowListFunctions(ContractAddress)

	setFeeConfigFunc := contract.NewStatefulPrecompileFunction(setFeeConfigSignature, setFeeConfig)
	getFeeConfigFunc := contract.NewStatefulPrecompileFunction(getFeeConfigSignature, getFeeConfig)
	getFeeConfigLastChangedAtFunc := contract.NewStatefulPrecompileFunction(getFeeConfigLastChangedAtSignature, getFeeConfigLastChangedAt)

	feeManagerFunctions = append(feeManagerFunctions, setFeeConfigFunc, getFeeConfigFunc, getFeeConfigLastChangedAtFunc)
	// Construct the contract with no fallback function.
	contract, err := contract.NewStatefulPrecompileContract(nil, feeManagerFunctions)
	// TODO Change this to be returned as an error after refactoring this precompile
	// to use the new precompile template.
	if err != nil {
		panic(err)
	}
	return contract
}

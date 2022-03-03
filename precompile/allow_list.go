// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Enum constants for valid AllowListRole
type AllowListRole common.Hash

var (
	AllowListNoRole  AllowListRole = AllowListRole(common.BigToHash(big.NewInt(0))) // No role assigned - this is equivalent to common.Hash{} and deletes the key from the DB when set
	AllowListEnabled AllowListRole = AllowListRole(common.BigToHash(big.NewInt(1))) // Deployers are allowed to create new contracts
	AllowListAdmin   AllowListRole = AllowListRole(common.BigToHash(big.NewInt(2))) // Admin - allowed to modify both the admin and deployer list as well as deploy contracts

	// AllowList function signatures
	setAdminSignature      = CalculateFunctionSelector("setAdmin(address)")
	setDeployerSignature   = CalculateFunctionSelector("setDeployer(address)")
	setNoneSignature       = CalculateFunctionSelector("setNone(address)")
	readAllowListSignature = CalculateFunctionSelector("readAllowList(address)")

	// Error returned when an invalid write is attempted
	ErrReadOnlyModifyAllowList = errors.New("cannot modify allow list in read only")
	ErrCannotModifyAllowList   = errors.New("non-admin cannot modify allow list")
	ErrExceedsGasAllowance     = errors.New("running allow list exceeds gas allowance")
)

// AllowListConfig specifies the configuration of the allow list.
// Specifies the block timestamp at which it goes into effect as well as the initial set of allow list admins.
type AllowListConfig struct {
	BlockTimestamp *big.Int `json:"blockTimestamp"`

	AllowListAdmins []common.Address `json:"adminAddresses"`
}

// Timestamp returns the timestamp at which the allow list should be enabled
func (c *AllowListConfig) Timestamp() *big.Int { return c.BlockTimestamp }

// Configure initializes the address space of [precompileAddr] by initializing the role of each of
// the addresses in [AllowListAdmins].
func (c *AllowListConfig) Configure(state StateDB, precompileAddr common.Address) {
	for _, adminAddr := range c.AllowListAdmins {
		setAllowListRole(state, precompileAddr, adminAddr, AllowListAdmin)
	}
}

// Valid returns true iff [s] represents a valid role.
func (s AllowListRole) Valid() bool {
	switch s {
	case AllowListNoRole, AllowListEnabled, AllowListAdmin:
		return true
	default:
		return false
	}
}

// IsAdmin returns true if [s] indicates the permission to modify the allow list.
func (s AllowListRole) IsAdmin() bool {
	switch s {
	case AllowListAdmin:
		return true
	default:
		return false
	}
}

// IsEnabled returns true if [s] indicates that it has permission to access the resource.
func (s AllowListRole) IsEnabled() bool {
	switch s {
	case AllowListAdmin, AllowListEnabled:
		return true
	default:
		return false
	}
}

// getAllowListStatus returns the allow list role of [address] for the precompile
// at [precompileAddr]
func getAllowListStatus(state StateDB, precompileAddr common.Address, address common.Address) AllowListRole {
	// Generate the state key for [address]
	addressKey := address.Hash()
	return AllowListRole(state.GetState(precompileAddr, addressKey))
}

// setAllowListRole sets the permissions of [address] to [role] for the precompile
// at [precompileAddr].
// assumes [role] has already been verified as valid.
func setAllowListRole(stateDB StateDB, precompileAddr, address common.Address, role AllowListRole) {
	// Generate the state key for [address]
	addressKey := address.Hash()
	// Assign [role] to the address
	stateDB.SetState(precompileAddr, addressKey, common.Hash(role))
}

// PackModifyAllowList packs [address] and [role] into the appropriate arguments for modifying the allow list.
// Note: [role] is not packed in the input value returned, but is instead used as a selector for the function
// selector that should be encoded in the input.
func PackModifyAllowList(address common.Address, role AllowListRole) ([]byte, error) {
	// function selector (4 bytes) + hash for address
	input := make([]byte, 0, selectorLen+common.HashLength)

	switch role {
	case AllowListAdmin:
		input = append(input, setAdminSignature...)
	case AllowListEnabled:
		input = append(input, setDeployerSignature...)
	case AllowListNoRole:
		input = append(input, setNoneSignature...)
	default:
		return nil, fmt.Errorf("cannot pack modify list input with invalid role: %s", role)
	}

	input = append(input, address.Hash().Bytes()...)
	return input, nil
}

// PackReadAllowList packs [address] into the input data to the read allow list function
func PackReadAllowList(address common.Address) []byte {
	input := make([]byte, 0, selectorLen+common.HashLength)
	input = append(input, readAllowListSignature...)
	input = append(input, address.Hash().Bytes()...)
	return input
}

// writeAllowList verifies that [callerAddr] has the correct permissions to modify the allow list and if so updates [modifyAddress] permissions
// to [role] within the state of [precompileAddr].
func writeAllowList(evm PrecompileAccessibleState, precompileAddr common.Address, callerAddr common.Address, modifyAddress common.Address, role AllowListRole, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Note: this should never happen since the required gas should be verified before calling Run.
	if suppliedGas < ModifyAllowListGasCost {
		return nil, 0, fmt.Errorf("running allow list exceeds gas allowance (%d) < (%d)", ModifyAllowListGasCost, suppliedGas)
	}

	remainingGas = suppliedGas - ModifyAllowListGasCost
	if readOnly {
		return nil, remainingGas, ErrReadOnlyModifyAllowList
	}

	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := getAllowListStatus(evm.GetStateDB(), precompileAddr, callerAddr)
	if !callerStatus.IsAdmin() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotModifyAllowList, callerAddr)
	}

	setAllowListRole(evm.GetStateDB(), precompileAddr, modifyAddress, role)

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createAllowListSetter returns an execution function for setting the allow list status of the input address argument to [role].
// This execution function is speciifc to [precompileAddr].
func createAllowListSetter(precompileAddr common.Address, role AllowListRole) RunStatefulPrecompileFunc {
	return func(evm PrecompileAccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if len(input) != common.HashLength {
			return nil, remainingGas, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
		}

		modifyAddress := common.BytesToAddress(input)
		return writeAllowList(evm, precompileAddr, callerAddr, modifyAddress, role, suppliedGas, readOnly)
	}
}

// createReadAllowList returns an execution function that reads the allow list for the given [precompileAddr].
// THe execution function parses the input into a single address and returns the 32 byte hash that specifies the
// designated role of that address
func createReadAllowList(precompileAddr common.Address) RunStatefulPrecompileFunc {
	return func(evm PrecompileAccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		// Note: this should never happen since the required gas should be verified before calling Run.
		if suppliedGas < ReadAllowListGasCost {
			return nil, 0, fmt.Errorf("%w (%d) < (%d)", ErrExceedsGasAllowance, ReadAllowListGasCost, suppliedGas)
		}

		remainingGas = suppliedGas - ReadAllowListGasCost

		if len(input) != common.HashLength {
			return nil, remainingGas, fmt.Errorf("invalid input length for read allow list: %d", len(input))
		}

		readAddress := common.BytesToAddress(input)
		role := getAllowListStatus(evm.GetStateDB(), precompileAddr, readAddress)
		roleBytes := common.Hash(role).Bytes()
		return roleBytes, remainingGas, nil
	}
}

// createAllowListPrecompile returns a StatefulPrecompiledContract with R/W control of an allow list at [precompileAddr]
func createAllowListPrecompile(precompileAddr common.Address) StatefulPrecompiledContract {
	setAdmin := newStatefulPrecompileFunction(setAdminSignature, createAllowListSetter(precompileAddr, AllowListAdmin), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setDeployer := newStatefulPrecompileFunction(setDeployerSignature, createAllowListSetter(precompileAddr, AllowListEnabled), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setNone := newStatefulPrecompileFunction(setNoneSignature, createAllowListSetter(precompileAddr, AllowListNoRole), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	read := newStatefulPrecompileFunction(readAllowListSignature, createReadAllowList(precompileAddr), createConstantRequiredGasFunc(ReadAllowListGasCost))

	// Construct the contract with no fallback function.
	contract := newStatefulPrecompileWithFunctionSelectors(nil, []*statefulPrecompileFunction{setAdmin, setDeployer, setNone, read})
	return contract
}

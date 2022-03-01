// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var (
	// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
	AllowListPrecompile StatefulPrecompiledContract = createAllowListPrecompile()
	_                   StatefulPrecompileConfig    = &AllowListConfig{}
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
)

// AllowListConfig specifies the configuration of the allow list.
// Specifies the block timestamp at which it goes into effect as well as the initial set of allow list admins.
type AllowListConfig struct {
	BlockTimestamp *big.Int `json:"blockTimestamp"`

	AllowListAdmins []common.Address `json:"adminAddresses"`
}

// Address returns the address where the stateful precompile is accessible.
func (c *AllowListConfig) Address() common.Address { return AllowListAddress }

// Timestamp returns the timestamp at which the allow list should be enabled
func (c *AllowListConfig) Timestamp() *big.Int { return c.BlockTimestamp }

// Configure initializes the address space of [ModifyAllowListAddress] by initializing the role of each of
// the addresses in [AllowListAdmins].
func (c *AllowListConfig) Configure(state StateDB) {
	for _, adminAddr := range c.AllowListAdmins {
		SetAllowListRole(state, adminAddr, AllowListAdmin)
	}
}

// Contract returns the singleton stateful precompiled contract to be used for the allow list.
func (c *AllowListConfig) Contract() StatefulPrecompiledContract { return AllowListPrecompile }

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

// GetAllowListStatus returns the allow list role of [address].
func GetAllowListStatus(state StateDB, address common.Address) AllowListRole {
	// Generate the state key for [address]
	addressKey := address.Hash()
	return AllowListRole(state.GetState(AllowListAddress, addressKey))
}

// SetAllowListRole sets the permissions of [address] to [role]
// assumes [role] has already been verified as valid.
func SetAllowListRole(stateDB StateDB, address common.Address, role AllowListRole) {
	// Generate the state key for [address]
	addressKey := address.Hash()
	// Assign [role] to the address
	stateDB.SetState(AllowListAddress, addressKey, common.Hash(role))
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
	case AllowListAdmin:
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
// to [role].
func writeAllowList(evm PrecompileAccessibleState, callerAddr common.Address, modifyAddress common.Address, role AllowListRole, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Note: this should never happen since the required gas should be verified before calling Run.
	if suppliedGas < ModifyAllowListGasCost {
		return nil, 0, fmt.Errorf("running allow list exceeds gas allowance (%d) < (%d)", ModifyAllowListGasCost, suppliedGas)
	}

	remainingGas = suppliedGas - ModifyAllowListGasCost
	if readOnly {
		return nil, remainingGas, fmt.Errorf("cannot modify allow list in read only")
	}

	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerStatus := GetAllowListStatus(evm.GetStateDB(), callerAddr)
	if !callerStatus.IsAdmin() {
		return nil, remainingGas, fmt.Errorf("caller %s cannot modify allow list", callerAddr)
	}

	SetAllowListRole(evm.GetStateDB(), modifyAddress, role)

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createAllowListSetter returns an execution function for setting the allow list status of the input address argument to [role].
func createAllowListSetter(role AllowListRole) func(evm PrecompileAccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return func(evm PrecompileAccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if len(input) != common.HashLength {
			return nil, remainingGas, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
		}

		modifyAddress := common.BytesToAddress(input)
		return writeAllowList(evm, callerAddr, modifyAddress, role, suppliedGas, readOnly)
	}
}

// readAllowList is the execution function for read access to the allow list.
// parses [input] into a single address and returns the 32 byte hash that specifies the designated role of that address.
func readAllowList(evm PrecompileAccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Note: this should never happen since the required gas should be verified before calling Run.
	if suppliedGas < ReadAllowListGasCost {
		return nil, 0, fmt.Errorf("running allow list exceeds gas allowance (%d) < (%d)", ReadAllowListGasCost, suppliedGas)
	}

	remainingGas = suppliedGas - ReadAllowListGasCost

	if len(input) != common.HashLength {
		return nil, remainingGas, fmt.Errorf("invalid input length for read allow list: %d", len(input))
	}

	readAddress := common.BytesToAddress(input)
	role := GetAllowListStatus(evm.GetStateDB(), readAddress)
	roleBytes := common.Hash(role).Bytes()
	return roleBytes, remainingGas, nil
}

// createAllowListPrecompile returns a StatefulPrecompiledContract with R/W control of the allow list
func createAllowListPrecompile() StatefulPrecompiledContract {
	setAdmin := newStatefulPrecompileFunction(setAdminSignature, createAllowListSetter(AllowListAdmin), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setDeployer := newStatefulPrecompileFunction(setDeployerSignature, createAllowListSetter(AllowListEnabled), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setNone := newStatefulPrecompileFunction(setNoneSignature, createAllowListSetter(AllowListNoRole), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	read := newStatefulPrecompileFunction(readAllowListSignature, readAllowList, createConstantRequiredGasFunc(ReadAllowListGasCost))

	// Construct the contract with no fallback function.
	contract := newStatefulPrecompileWithFunctionSelectors(nil, []*statefulPrecompileFunction{setAdmin, setDeployer, setNone, read})
	return contract
}

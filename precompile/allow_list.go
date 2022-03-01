// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
var (
	AllowListPrecompile StatefulPrecompiledContract = createAllowListPrecompile()
)

// Enum constants for valid AllowListRole
var (
	None     common.Hash = common.BigToHash(big.NewInt(0)) // No role assigned - this is equivalent to common.Hash{} and deletes the key from the DB when set
	Deployer common.Hash = common.BigToHash(big.NewInt(1)) // Deployers are allowed to create new contracts
	Admin    common.Hash = common.BigToHash(big.NewInt(2)) // Admin - allowed to modify both the admin and deployer list as well as deploy contracts

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
		SetAllowListRole(state, adminAddr, Admin)
	}
}

// Valid returns true iff [s] represents a valid role.
func ValidAllowListRole(s common.Hash) bool {
	switch s {
	case None, Deployer, Admin:
		return true
	default:
		return false
	}
}

// IsAdmin returns true if [s] indicates the permission to modify the allow list.
func IsAllowListAdmin(s common.Hash) bool {
	switch s {
	case Admin:
		return true
	default:
		return false
	}
}

// HasDeployerPrivileges returns true iff [s] indicates the permission to deploy contracts.
func IsAllowListDeployer(s common.Hash) bool {
	switch s {
	case Deployer, Admin:
		return true
	default:
		return false
	}
}

// GetAllowListStatus returns the allow list role of [address].
func GetAllowListStatus(state StateDB, address common.Address) common.Hash {
	// Generate the state key for [address]
	addressKey := address.Hash()
	return state.GetState(AllowListAddress, addressKey)
}

// SetAllowListRole sets the permissions of [address] to [status]
// assumes [status] has already been verified as valid.
func SetAllowListRole(stateDB StateDB, address common.Address, role common.Hash) {
	// Generate the state key for [address]
	addressKey := address.Hash()
	// Assign [role] to the address
	stateDB.SetState(AllowListAddress, addressKey, role)
}

// PackModifyAllowList packs [address] and [role] into the appropriate arguments for modifying the allow list.
// Note: [role] is not packed in the input value returned, but is instead used as a selector for the function
// selector that should be encoded in the input.
func PackModifyAllowList(address common.Address, role common.Hash) ([]byte, error) {
	// function selector (4 bytes) + hash for address
	input := make([]byte, 0, selectorLen+common.HashLength)

	switch role {
	case Admin:
		input = append(input, setAdminSignature...)
	case Deployer:
		input = append(input, setDeployerSignature...)
	case None:
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
func writeAllowList(evm PrecompileAccessibleState, callerAddr common.Address, modifyAddress common.Address, role common.Hash, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
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
	if !IsAllowListAdmin(callerStatus) {
		log.Info("allow list EVM received attempt to modify the allow list from a non-allowed address", "callerAddr", callerAddr, "callerStatus", callerStatus)
		return nil, remainingGas, fmt.Errorf("caller %s cannot modify allow list", callerAddr)
	}

	SetAllowListRole(evm.GetStateDB(), modifyAddress, role)

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createAllowListSetter returns an execution function for setting the allow list status of the input address argument to [role].
func createAllowListSetter(role common.Hash) func(evm PrecompileAccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return func(evm PrecompileAccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if len(input) != common.HashLength {
			return nil, remainingGas, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
		}

		modifyAddress := common.BytesToAddress(input)
		return writeAllowList(evm, callerAddr, modifyAddress, role, suppliedGas, readOnly)
	}
}

// readAllowList parses [input] into a single address and returns the 32 byte hash that specifies the designated role of that address.
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
	roleBytes := role.Bytes()
	return roleBytes, remainingGas, nil
}

// createAllowListPrecompile returns a StatefulPrecompiledContract with R/W control of the allow list
func createAllowListPrecompile() StatefulPrecompiledContract {
	setAdmin := newStatefulPrecompileFunction(setAdminSignature, createAllowListSetter(Admin), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setDeployer := newStatefulPrecompileFunction(setDeployerSignature, createAllowListSetter(Deployer), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	setNone := newStatefulPrecompileFunction(setNoneSignature, createAllowListSetter(None), createConstantRequiredGasFunc(ModifyAllowListGasCost))
	read := newStatefulPrecompileFunction(readAllowListSignature, readAllowList, createConstantRequiredGasFunc(ReadAllowListGasCost))

	contract := newStatefulPrecompileWithFunctionSelectors(setAdmin, setDeployer, setNone, read)
	return contract
}

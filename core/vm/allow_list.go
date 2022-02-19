// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

const (
	allowListGasCost                  = 5000
	modifyStatusPrecompileInputLength = common.AddressLength + common.HashLength
)

type allowListPrecompile struct{}

type ModifyStatus common.Hash

// enum consts for statuses
var (
	None     ModifyStatus = ModifyStatus(common.Hash{})  // No role assigned - this is equivalent to common.Hash{}
	Deployer ModifyStatus = ModifyStatus(common.Hash{1}) // Deployers are allowed to create new contracts
	Admin    ModifyStatus = ModifyStatus(common.Hash{1}) // Admin - allowed to modify both the admin and deployer lists
)

// valid returns nil if the status is a valid status.
func (s ModifyStatus) Valid() bool {
	switch s {
	case None, Deployer, Admin:
		return true
	default:
		return false
	}
}

// IsAdmin
func (s ModifyStatus) HasAdminPrivileges() bool {
	switch s {
	case Admin:
		return true
	default:
		return false
	}
}

// IsDeployer
func (s ModifyStatus) HasDeployPrivileges() bool {
	switch s {
	case Deployer, Admin:
		return true
	default:
		return false
	}
}

// createAddressKey converts [address] into a [common.Hash] value that can be used for a storage slot key
func createAddressKey(address common.Address) common.Hash {
	hashBytes := make([]byte, common.HashLength)
	copy(hashBytes, address[:])
	return common.BytesToHash(hashBytes)
}

// getAllowListStatus checks the state storage of [AllowListPrecompileAddress] and returns
// the ModifyStatus of [address]
func getAllowListStatus(state StateDB, address common.Address) ModifyStatus {
	stateSlot := createAddressKey(address)
	res := state.GetState(AllowListPrecompileAddress, stateSlot)
	return ModifyStatus(res)
}

// PackModifyAllowList packs the arguments [address] and [status] into a single byte slice as input to the modify allow list
// precompile
func PackModifyAllowList(address common.Address, status ModifyStatus) ([]byte, error) {
	// If [allow], set the last byte to 1 to indicate that [address] should be added to the whitelist
	if !status.Valid() {
		return nil, fmt.Errorf("unexpected status: %d", status)
	}

	input := make([]byte, modifyStatusPrecompileInputLength)
	copy(input, address[:])
	copy(input[20:], status[:])
	return input, nil
}

// UnpackModifyAllowList attempts to unpack [input] into the arguments to the allow list precompile
func UnpackModifyAllowList(input []byte) (common.Address, ModifyStatus, error) {
	if len(input) != modifyStatusPrecompileInputLength {
		return common.Address{}, None, fmt.Errorf("unexpected address length: %d not %d", len(input), modifyStatusPrecompileInputLength)
	}

	address := common.BytesToAddress(input[:common.AddressLength])
	status := ModifyStatus(common.BytesToHash(input[common.AddressLength:]))

	if !status.Valid() {
		return common.Address{}, None, fmt.Errorf("unexpected value for modify status: %v", status)
	}

	return address, status, nil
}

// SetAllowListStatus sets the permissions of [address] to [status]
func SetAllowListStatus(stateDB StateDB, address common.Address, status ModifyStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid status used as input for allow list precompile: %v", status)
	}
	// Generate the state key for [address]
	addressKey := createAddressKey(address)
	// Update the type of [status] for the call to SetState
	role := common.Hash(status)
	log.Info("modify allow list", "address", address, "role", role)
	// Assign [role] to the address
	stateDB.SetState(AllowListPrecompileAddress, addressKey, role)
	return nil
}

func (al *allowListPrecompile) Run(evm *EVM, caller ContractRef, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if suppliedGas < allowListGasCost {
		return nil, 0, ErrOutOfGas
	}

	remainingGas = suppliedGas - allowListGasCost

	// Verify that the caller is in the allow list and therefore has the right to modify it
	callerAddr := caller.Address()
	callerStatus := getAllowListStatus(evm.StateDB, callerAddr)
	if !callerStatus.HasAdminPrivileges() {
		log.Info("EVM received attempt to modify the allow list from a non-allowed address", "callerAddr", callerAddr)
		return nil, remainingGas, ErrExecutionReverted
	}

	// Unpack the precompile's input into the necessary arguments
	address, status, err := UnpackModifyAllowList(input)
	if err != nil {
		log.Info("modify allow list reverted", "err", err)
		return nil, remainingGas, ErrExecutionReverted
	}

	if err := SetAllowListStatus(evm.StateDB, address, status); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}
